package sources_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/httpfetch"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ingest"
	ingestmocks "github.com/ethernity/obsidian-topic-stream/internal/service/ingest/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/sources"
	"github.com/ethernity/obsidian-topic-stream/internal/service/sources/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestCreateValidatesEverySourceConfiguration(t *testing.T) {
	t.Parallel()
	valid := []domain.SourceConfig{
		{Kind: domain.SourceKindRSS, URL: "https://example.com/feed.xml"},
		{Kind: domain.SourceKindAtom, URL: "http://feeds.example.com/atom"},
		{Kind: domain.SourceKindArxiv, Query: "cat:cs.AI"},
		{Kind: domain.SourceKindGithub, Period: "weekly", Language: "go"},
		{Kind: domain.SourceKindYoutube, ChannelID: "UCabcdefghijklmnopqrstuv"},
	}
	for _, config := range valid {
		repository := mocks.NewMockRepository(gomock.NewController(t))
		repository.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		service := sources.New(sources.Config{WorkspaceID: uuid.New(), Repository: repository, Now: time.Now})
		created, err := service.Create(t.Context(), sources.CreateInput{Name: "Source", Config: config, Enabled: true, PollIntervalMinutes: 60})
		require.NoError(t, err, config.Kind)
		require.Equal(t, config, created.Config)
	}

	invalid := []domain.SourceConfig{
		{Kind: domain.SourceKindRSS, URL: "file:///etc/passwd"},
		{Kind: domain.SourceKindRSS, URL: "https://user:secret@example.com/feed"},
		{Kind: domain.SourceKindArxiv, Query: " "},
		{Kind: domain.SourceKindGithub, Period: "yearly", Language: "go"},
		{Kind: domain.SourceKindGithub, Period: "daily", Language: "ruby"},
		{Kind: domain.SourceKindYoutube, ChannelID: "channel-name"},
		{Kind: "unknown"},
	}
	for _, config := range invalid {
		service := sources.New(sources.Config{WorkspaceID: uuid.New(), Repository: mocks.NewMockRepository(gomock.NewController(t)), Now: time.Now})
		_, err := service.Create(t.Context(), sources.CreateInput{Name: "Source", Config: config, Enabled: true, PollIntervalMinutes: 60})
		require.Error(t, err, config)
	}
}

func TestCreateValidatesNameAndPollingBounds(t *testing.T) {
	t.Parallel()
	service := sources.New(sources.Config{WorkspaceID: uuid.New(), Repository: mocks.NewMockRepository(gomock.NewController(t)), Now: time.Now})
	config := domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://example.com/feed"}
	_, err := service.Create(t.Context(), sources.CreateInput{Name: " ", Config: config, PollIntervalMinutes: 60})
	var validation *domain.ValidationError
	require.ErrorAs(t, err, &validation)
	require.Equal(t, "name", validation.Violations[0].Field)
	_, err = service.Create(t.Context(), sources.CreateInput{Name: "Feed", Config: config, PollIntervalMinutes: 14})
	require.ErrorAs(t, err, &validation)
	require.Equal(t, domain.Violation{Field: "pollIntervalMinutes", Rule: domain.ValidationRange, Minimum: 15, Maximum: 10080}, validation.Violations[0])
	_, err = service.Create(t.Context(), sources.CreateInput{Name: "Feed", Config: config, PollIntervalMinutes: 10081})
	require.ErrorAs(t, err, &validation)
	require.Equal(t, "pollIntervalMinutes", validation.Violations[0].Field)
}

func TestCreateCollectsIndependentSourceViolationsBeforeWriting(t *testing.T) {
	t.Parallel()
	service := sources.New(sources.Config{WorkspaceID: uuid.New(), Repository: mocks.NewMockRepository(gomock.NewController(t)), Now: time.Now})
	_, err := service.Create(t.Context(), sources.CreateInput{
		Name: " ", Config: domain.SourceConfig{Kind: domain.SourceKindGithub, Period: "yearly", Language: "unknown"}, PollIntervalMinutes: 1,
	})
	var validation *domain.ValidationError
	require.ErrorAs(t, err, &validation)
	require.ErrorIs(t, err, domain.ErrValidation)
	require.Len(t, validation.Violations, 4)
	fields := make([]string, 0, 4)
	for _, issue := range validation.Violations {
		fields = append(fields, issue.Field)
	}
	require.ElementsMatch(t, []string{"name", "config.period", "config.language", "pollIntervalMinutes"}, fields)
}

func TestSourceJobPreservesUpstreamRetryAfter(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7200")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	_, upstreamErr := httpfetch.NewClient(true).Get(server.URL)
	require.Error(t, upstreamErr)
	reader := ingestmocks.NewMockReader(gomock.NewController(t))
	writer := ingestmocks.NewMockWriter(gomock.NewController(t))
	repository := mocks.NewMockRepository(gomock.NewController(t))
	source := domain.Source{ID: uuid.New(), WorkspaceID: uuid.New()}
	reader.EXPECT().Read(gomock.Any(), source).Return(nil, upstreamErr)
	repository.EXPECT().Get(gomock.Any(), source.WorkspaceID, source.ID).Return(source, true, nil).Times(2)
	ingester := ingest.New(reader, writer, nil, time.Now)
	queue := newJobQueue(t)
	service := sources.New(sources.Config{WorkspaceID: source.WorkspaceID, Repository: repository, Ingester: &ingester, Jobs: queue, Now: time.Now})
	_, err := service.Sync(t.Context(), source.ID)
	require.NoError(t, err)
	err = queue.run(t, 0)
	require.ErrorIs(t, err, upstreamErr)
	require.Equal(t, jobs.RetryAfter(errors.Unwrap(err), 2*time.Hour), err)
}
func TestSyncAllEnqueuesIndependentIdempotentJobsForEnabledSources(t *testing.T) {
	t.Parallel()
	workspaceID := uuid.New()
	first, second := domain.Source{ID: uuid.New(), Enabled: true}, domain.Source{ID: uuid.New(), Enabled: true}
	repository := mocks.NewMockRepository(gomock.NewController(t))
	repository.EXPECT().List(gomock.Any(), workspaceID).Return([]domain.Source{first, {ID: uuid.New(), Enabled: false}, second}, nil)
	queue := newJobQueue(t)
	service := sources.New(sources.Config{WorkspaceID: workspaceID, Repository: repository, Jobs: queue, Now: time.Now})
	_, err := service.SyncAll(t.Context())
	require.NoError(t, err)
	require.NoError(t, queue.run(t, 0))
	require.Len(t, queue.requests, 3)
	for i, source := range []domain.Source{first, second} {
		request := queue.requests[i+1]
		require.Equal(t, workspaceID, request.WorkspaceID)
		require.Equal(t, "sync-source", request.Definition.Kind)
		require.Equal(t, domain.JobQueueFetch, request.Definition.Queue)
		require.Equal(t, "sync-source:"+source.ID.String(), request.IdempotencyKey)
		payload, err := json.Marshal(request.Arguments)
		require.NoError(t, err)
		require.JSONEq(t, `{"sourceId":"`+source.ID.String()+`"}`, string(payload))
	}
}
func TestExtractionFailureIsReturnedForWorkerRetry(t *testing.T) {
	t.Parallel()
	source := domain.Source{ID: uuid.New(), WorkspaceID: uuid.New()}
	failure := errors.New("extract article: fetch article returned status 503")
	reader := ingestmocks.NewMockReader(gomock.NewController(t))
	reader.EXPECT().Read(gomock.Any(), source).Return(nil, failure)
	writer := ingestmocks.NewMockWriter(gomock.NewController(t))
	repository := mocks.NewMockRepository(gomock.NewController(t))
	repository.EXPECT().Get(gomock.Any(), source.WorkspaceID, source.ID).Return(source, true, nil).Times(2)
	ingester := ingest.New(reader, writer, nil, time.Now)
	queue := newJobQueue(t)
	service := sources.New(sources.Config{WorkspaceID: source.WorkspaceID, Repository: repository, Ingester: &ingester, Jobs: queue, Now: time.Now})
	_, err := service.Sync(t.Context(), source.ID)
	require.NoError(t, err)
	require.Equal(t, 5, queue.requests[0].Definition.Retry.MaxAttempts)
	require.ErrorIs(t, queue.run(t, 0), failure)
}
