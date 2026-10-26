package bench_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/bench"
	"github.com/ethernity/obsidian-topic-stream/internal/service/bench/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type benchIndexStub struct {
	dense, sparse []ranking.IndexHit
	personal      ranking.PersonalRetrieval
}

// QueryDenseText returns the fixture's dense retrieval results.
func (s benchIndexStub) QueryDenseText(context.Context, string, ranking.IndexFilter, int) ([]ranking.IndexHit, error) {
	return s.dense, nil
}

// QuerySparse returns the fixture's lexical retrieval results.
func (s benchIndexStub) QuerySparse(context.Context, string, ranking.IndexFilter, int) ([]ranking.IndexHit, error) {
	return s.sparse, nil
}

// RetrievePersonal returns configured personal-feedback evidence.
func (s benchIndexStub) RetrievePersonal(context.Context, ranking.PersonalIndexRequest) (ranking.PersonalRetrieval, error) {
	return s.personal, nil
}

func TestEvaluatorComparesNamedPipelinesWithReproducibleTemporalSplit(t *testing.T) {
	t.Parallel()

	workspaceID, sourceID := uuid.New(), uuid.New()
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	items := make([]domain.Item, 10)
	for index := range items {
		sentiment := "like"
		if index%2 == 1 {
			sentiment = "dislike"
		}
		updatedAt := start.Add(time.Duration(index) * time.Hour)
		items[index] = domain.Item{
			ID: uuid.New(), WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{sourceID},
			Title: "Training item", Feedback: &domain.Feedback{Sentiment: sentiment, UpdatedAt: updatedAt},
		}
	}
	testLike, testDislike := items[8], items[9]
	index := benchIndexStub{
		dense:  []ranking.IndexHit{{ItemID: testDislike.ID, Score: 1}, {ItemID: testLike.ID, Score: 0.9}},
		sparse: []ranking.IndexHit{{ItemID: testLike.ID, Score: 1}, {ItemID: testDislike.ID, Score: 0.9}},
	}
	repository := mocks.NewMockRepository(gomock.NewController(t))
	feedback := make([]domain.ItemFeedback, len(items))
	for i, item := range items {
		feedback[i] = domain.ItemFeedback{ItemID: item.ID, Feedback: *item.Feedback}
		repository.EXPECT().GetItem(gomock.Any(), workspaceID, item.ID).Return(item, true, nil).AnyTimes()
	}
	repository.EXPECT().ListCurrentFeedback(gomock.Any(), workspaceID).Return(feedback, nil)
	evaluator, err := bench.New(workspaceID, repository, index, 60)
	require.NoError(t, err)

	report, err := evaluator.Evaluate(t.Context(), bench.Options{})

	require.NoError(t, err)
	require.Equal(t, bench.ReportVersion, report.Version)
	require.Equal(t, 80, report.TrainPercent)
	require.Equal(t, 8, report.TrainingCount)
	require.Equal(t, 2, report.TestCount)
	require.Equal(t, []string{"bm25", "dense", "hybrid-rrf", "hybrid-personalized"}, pipelineNames(report))
	require.Equal(t, 1.0, report.Pipelines[0].Metrics.MRR)
	require.Equal(t, 0.5, report.Pipelines[1].Metrics.MRR)
}

func pipelineNames(report bench.Report) []string {
	result := make([]string, len(report.Pipelines))
	for index, pipeline := range report.Pipelines {
		result[index] = pipeline.Name
	}
	return result
}
