package summaries_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	jobmocks "github.com/ethernity/obsidian-topic-stream/internal/service/jobs/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/service/summaries"
	"github.com/ethernity/obsidian-topic-stream/internal/service/summaries/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// summaryFixture keeps all provider calls observable at the consumer-owned outbound port.
type summaryFixture struct {
	service     *summaries.Service
	workspaceID uuid.UUID
	repository  *mocks.MockRepository
	settings    *mocks.MockWorkspaceSettings
	generator   *mocks.MockGenerator
	events      *jobmocks.MockStateNotifier
	queue       *jobmocks.MockQueue
	definition  jobs.JobDefinition
	handler     jobs.Handler
}

// newSummaryFixture constructs the real owner with strict persistence/provider boundaries.
func newSummaryFixture(t *testing.T) *summaryFixture {
	t.Helper()
	controller := gomock.NewController(t)
	f := &summaryFixture{workspaceID: uuid.New(), repository: mocks.NewMockRepository(controller), settings: mocks.NewMockWorkspaceSettings(controller), generator: mocks.NewMockGenerator(controller), events: jobmocks.NewMockStateNotifier(controller), queue: jobmocks.NewMockQueue(controller)}
	f.queue.EXPECT().RegisterDefinition(gomock.Any(), gomock.Any()).Do(func(def jobs.JobDefinition, handler jobs.Handler) { f.definition = def; f.handler = handler })
	service, err := summaries.New(summaries.Config{WorkspaceID: f.workspaceID, Repository: f.repository, Settings: f.settings, Generator: f.generator, Jobs: f.queue, Events: f.events, Now: time.Now})
	require.NoError(t, err)
	f.service = service
	return f
}

// summarySettings supplies valid enabled generation settings independent of an Embedding Model.
func summarySettings() domain.WorkspaceSettings {
	endpoint, model := "https://summary.example/v1", "summary-model"
	return domain.WorkspaceSettings{Generation: domain.GenerationSettings{Enabled: true, Endpoint: &endpoint, SummaryModel: &model}, Summary: domain.SummarySettings{Language: "same-as-source", MaxInputChars: 48000, MaxSummaryCharacters: 800}, Instructions: domain.TaskInstructions{SummaryChunk: "Extract facts", SummaryReduce: "Reduce facts", SummaryFinal: "Summarize facts"}}
}

func TestSummaryReadReturnsNotRequestedWithoutCallingProvider(t *testing.T) {
	t.Parallel()
	f := newSummaryFixture(t)
	itemID := uuid.New()
	item := domain.Item{ID: itemID, WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, Title: "Article", ContentMarkdown: "Extracted body", ContentOrigin: domain.ContentOriginArticleBody}
	f.repository.EXPECT().GetSummarySource(gomock.Any(), f.workspaceID, domain.SummarySource{ItemID: itemID}).Return(item, true, nil)
	f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(summarySettings(), nil)
	f.repository.EXPECT().ReadSummaryBinding(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, query domain.SummaryBindingQuery) (domain.SummaryBinding, error) {
		require.Equal(t, f.workspaceID, query.WorkspaceID)
		require.Equal(t, itemID, query.ItemID)
		require.Len(t, query.SourceVersion, 64)
		return domain.SummaryBinding{}, nil
	})
	state, err := f.service.Read(t.Context(), domain.SummarySource{ItemID: itemID})
	require.NoError(t, err)
	require.True(t, state.Supported)
	require.True(t, state.CanGenerate)
	require.Equal(t, domain.ItemSummaryNotRequested, state.State)
	require.Nil(t, state.Result)
	require.Nil(t, state.JobID)
}

func TestSummaryReadRejectsDescriptiveContentWithoutCallingProvider(t *testing.T) {
	cases := []struct {
		kind   domain.ItemKind
		origin domain.ContentOrigin
	}{
		{domain.ItemKindArticle, domain.ContentOriginArticleDescription},
		{domain.ItemKindPaper, domain.ContentOriginPaperAbstract},
		{domain.ItemKindVideo, domain.ContentOriginVideoDescription},
		{domain.ItemKindRepository, domain.ContentOriginRepositoryDescription},
		{domain.ItemKindArticle, domain.ContentOriginUnknown},
	}
	for _, scenario := range cases {
		t.Run(string(scenario.origin), func(t *testing.T) {
			f := newSummaryFixture(t)
			itemID := uuid.New()
			f.repository.EXPECT().GetSummarySource(gomock.Any(), f.workspaceID, domain.SummarySource{ItemID: itemID}).Return(domain.Item{ID: itemID, WorkspaceID: f.workspaceID, Kind: scenario.kind, Title: "Metadata", ContentMarkdown: "Nonempty description or abstract", ContentOrigin: scenario.origin}, true, nil)
			f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(summarySettings(), nil)
			f.repository.EXPECT().ReadSummaryBinding(gomock.Any(), gomock.Any()).Return(domain.SummaryBinding{}, nil)
			state, err := f.service.Read(t.Context(), domain.SummarySource{ItemID: itemID})
			require.NoError(t, err)
			require.False(t, state.Supported)
			require.False(t, state.CanGenerate)
			require.Equal(t, "unsupported_input", *state.UnavailableReason)
			require.Equal(t, domain.ItemSummaryNotRequested, state.State)
		})
	}
}

func TestSummaryReadKeepsReadyReadableAndReportsPendingOrFailedWork(t *testing.T) {
	jobID := uuid.New()
	failure := "Provider unavailable"
	cases := []struct {
		name               string
		binding            domain.SummaryBinding
		state              domain.ItemSummaryState
		enabled, retryable bool
	}{
		{"pending", domain.SummaryBinding{Job: &domain.Job{ID: jobID, Status: domain.JobStatusQueued}}, domain.ItemSummaryPending, true, false},
		{"failed", domain.SummaryBinding{Job: &domain.Job{ID: jobID, Status: domain.JobStatusDead, Reason: &failure}}, domain.ItemSummaryFailed, true, true},
		{"ready after opt out", domain.SummaryBinding{Job: &domain.Job{ID: jobID, Status: domain.JobStatusSucceeded}, Result: &domain.AISummary{ID: uuid.New(), GenerationJobID: jobID, Text: "Stored annotation"}}, domain.ItemSummaryReady, false, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			f := newSummaryFixture(t)
			itemID := uuid.New()
			f.repository.EXPECT().GetSummarySource(gomock.Any(), f.workspaceID, domain.SummarySource{ItemID: itemID}).Return(domain.Item{ID: itemID, WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, ContentOrigin: domain.ContentOriginArticleBody, ContentMarkdown: "Body"}, true, nil)
			settings := summarySettings()
			settings.Generation.Enabled = scenario.enabled
			f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(settings, nil)
			f.repository.EXPECT().ReadSummaryBinding(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, query domain.SummaryBindingQuery) (domain.SummaryBinding, error) {
				if scenario.binding.Result != nil {
					scenario.binding.Result.SourceVersion = query.SourceVersion
				}
				return scenario.binding, nil
			})
			state, err := f.service.Read(t.Context(), domain.SummarySource{ItemID: itemID})
			require.NoError(t, err)
			require.Equal(t, scenario.state, state.State)
			require.Equal(t, scenario.enabled, state.CanGenerate)
			require.Equal(t, scenario.retryable, state.Retryable)
			require.Equal(t, &jobID, state.JobID)
			if scenario.binding.Result != nil {
				require.Equal(t, "Stored annotation", state.Result.Text)
			}
		})
	}
}

func TestSummaryReadOfHistoricalVersionNeverReturnsNewerContentResult(t *testing.T) {
	t.Parallel()
	f := newSummaryFixture(t)
	itemID := uuid.New()
	version := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	source := domain.SummarySource{ItemID: itemID, SourceVersion: version}
	f.repository.EXPECT().GetSummarySource(gomock.Any(), f.workspaceID, source).Return(domain.Item{ID: itemID, WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, ContentOrigin: domain.ContentOriginArticleBody, ContentMarkdown: "New canonical body"}, true, nil)
	key := domain.SummaryInputKey{WorkspaceID: f.workspaceID, ItemID: itemID, SourceVersion: version}
	f.repository.EXPECT().GetSummaryInput(gomock.Any(), key).Return(domain.SummaryInput{SummaryInputKey: key, Markdown: "Old captured body", Origin: domain.ContentOriginArticleBody}, true, nil)
	f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(summarySettings(), nil)
	f.repository.EXPECT().ReadSummaryBinding(gomock.Any(), domain.SummaryBindingQuery{SummaryInputKey: key}).Return(domain.SummaryBinding{Result: &domain.AISummary{ID: uuid.New(), SourceVersion: version, Text: "Old version annotation"}}, nil)
	state, err := f.service.Read(t.Context(), source)
	require.NoError(t, err)
	require.Equal(t, version, state.SourceVersion)
	require.Equal(t, "Old version annotation", state.Result.Text)
	require.False(t, state.CanGenerate)
	require.Equal(t, "historical_input", *state.UnavailableReason)
}

func TestSavedSummaryAfterLostAckIsReadyWithoutRetryableFailure(t *testing.T) {
	t.Parallel()
	f := newSummaryFixture(t)
	itemID, jobID := uuid.New(), uuid.New()
	failure := "acknowledgement lost"
	f.repository.EXPECT().GetSummarySource(gomock.Any(), f.workspaceID, domain.SummarySource{ItemID: itemID}).Return(domain.Item{ID: itemID, WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, ContentOrigin: domain.ContentOriginArticleBody, ContentMarkdown: "Body"}, true, nil)
	f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(summarySettings(), nil)
	f.repository.EXPECT().ReadSummaryBinding(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, query domain.SummaryBindingQuery) (domain.SummaryBinding, error) {
		return domain.SummaryBinding{Job: &domain.Job{ID: jobID, Status: domain.JobStatusDead, Reason: &failure}, Result: &domain.AISummary{ID: uuid.New(), GenerationJobID: jobID, SourceVersion: query.SourceVersion, Text: "Saved"}}, nil
	})
	state, err := f.service.Read(t.Context(), domain.SummarySource{ItemID: itemID})
	require.NoError(t, err)
	require.Equal(t, domain.ItemSummaryReady, state.State)
	require.Nil(t, state.Failure)
	require.False(t, state.Retryable)
}
