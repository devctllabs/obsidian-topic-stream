//go:build integration && live

package postgres_test

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/generation"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/service/summaries"
	"github.com/ethernity/obsidian-topic-stream/internal/service/summaries/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type liveSummaryGenerator struct {
	client *generation.SummaryClient
	calls  atomic.Int64
}

// Summarize records stage latency while preserving the production provider protocol.
func (g *liveSummaryGenerator) Summarize(ctx context.Context, prompt summaries.Prompt) (string, error) {
	g.calls.Add(1)
	return g.client.Summarize(ctx, prompt)
}

func TestSummaryLiveProvider(t *testing.T) {
	endpoint, model := os.Getenv("TOPIC_STREAM_LIVE_SUMMARY_ENDPOINT"), os.Getenv("TOPIC_STREAM_LIVE_SUMMARY_MODEL")
	require.NotEmpty(t, endpoint, "explicit live Summary endpoint is required")
	require.NotEmpty(t, model, "explicit installed Summary model is required")
	cases := []struct {
		name, body string
		budget     int
	}{
		{"english", "The archive stores immutable daily snapshots. New annotations are saved separately. Readers can inspect previous versions without rebuilding them. No external fetch is needed to summarize a saved article.", 48000},
		{"russian", "Архив хранит неизменяемые ежедневные выпуски. Новые аннотации сохраняются отдельно от исходного текста. Читатель может открыть старый выпуск без повторной сборки. Для краткого описания используется уже сохранённый текст статьи.", 48000},
		{"hierarchical", strings.Repeat("## Storage design\n\nThe archive stores immutable snapshots. Annotation jobs use captured source versions. A database transaction checks the worker lease before publishing. Ready historical annotations remain readable when generation is disabled.\n\n", 12), 1000},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			f := newFeedFixture(t)
			input := domain.SummaryInputForItem(domain.Item{ID: uuid.New(), WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, Title: scenario.name, ContentMarkdown: scenario.body, ContentOrigin: domain.ContentOriginArticleBody})
			accepted, err := f.store.Summaries().AdmitSummary(t.Context(), summaryAdmissionRequest(t, input))
			require.NoError(t, err)
			configuration := settings.DefaultWorkspaceSettings(time.Now())
			configuration.Generation = domain.GenerationSettings{Enabled: true, Endpoint: &endpoint, SummaryModel: &model}
			configuration.Summary.MaxInputChars = scenario.budget
			reader := mocks.NewMockWorkspaceSettings(gomock.NewController(t))
			reader.EXPECT().GetWorkspace(gomock.Any()).Return(configuration, nil).Times(1)
			worker := jobs.New(f.store.Jobs(), time.Now, jobs.Config{PollInterval: 10 * time.Millisecond, LeaseDuration: time.Minute, HeartbeatInterval: time.Second})
			generator := &liveSummaryGenerator{client: generation.NewSummaryClient(nil)}
			_, err = summaries.New(summaries.Config{WorkspaceID: f.workspaceID, Repository: f.store.Summaries(), Settings: reader, Jobs: worker.ForWorkspace(f.workspaceID), Events: events.NewForWorkspace(f.workspaceID, nil), Generator: generator, Now: time.Now})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- worker.Run(ctx) }()
			t.Cleanup(func() { cancel(); require.NoError(t, testutil.Receive(t, done)) })
			started := time.Now()
			var result domain.AISummary
			require.Eventually(t, func() bool {
				var found bool
				result, found, err = f.store.Summaries().GetSummaryResultForJob(t.Context(), f.workspaceID, accepted.Job.ID)
				return err == nil && found
			}, 170*time.Second, 100*time.Millisecond)
			require.NotEmpty(t, result.Text)
			require.LessOrEqual(t, utf8.RuneCountInString(result.Text), 800)
			require.Equal(t, model, result.Model)
			require.Equal(t, input.SourceVersion, result.SourceVersion)
			if scenario.name == "hierarchical" {
				require.Greater(t, generator.calls.Load(), int64(1))
			} else {
				require.EqualValues(t, 1, generator.calls.Load())
			}
			t.Logf("model=%s calls=%d elapsed=%s input_chars=%d output_chars=%d text=%s", model, generator.calls.Load(), time.Since(started), utf8.RuneCountInString(input.Markdown), utf8.RuneCountInString(result.Text), result.Text)
		})
	}
}
