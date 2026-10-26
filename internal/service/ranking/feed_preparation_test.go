package ranking_test

import (
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type preparationFactory struct{ index *topicIndexStub }

// BuildFeedIndex returns the fixture's Topic index without opening resources.
func (f preparationFactory) BuildFeedIndex(domain.RetrievalIndex) (ranking.TopicCandidateIndex, error) {
	return f.index, nil
}
func TestFeedPreparationKeepsGateAndLocalFusionWithoutPersonalWork(t *testing.T) {
	accepted, rejected := uuid.New(), uuid.New()
	index := &topicIndexStub{results: map[int]ranking.TopicRetrieval{2: {IncludeEmbedding: []float32{1, 0}, Dense: []ranking.IndexHit{{ItemID: rejected, Embedding: []float32{0, 1}, Score: 1}, {ItemID: accepted, Embedding: []float32{1, 0}, Score: 0.9}}, Sparse: []ranking.IndexHit{{ItemID: accepted, Embedding: []float32{1, 0}, Score: 1}}}}}
	preparer := ranking.NewFeedPreparer(preparationFactory{index: index}, &thresholdProviderStub{}, ranking.DefaultTopicThresholdDefaults("synthetic"))
	result, err := preparer.PrepareFeed(t.Context(), domain.FeedTopicPreparation{Topic: domain.Topic{ID: uuid.New(), WorkspaceID: uuid.New(), Mode: domain.TopicModeOrdinary, Enabled: true, Include: "systems"}, Settings: domain.WorkspaceSettings{Retrieval: validRetrievalSettings(2)}, Inputs: domain.FeedCandidateInputs{CandidateLimit: 2}, At: time.Now().UTC()})
	require.NoError(t, err)
	require.Len(t, result.Candidates, 1)
	require.Equal(t, accepted, result.Candidates[0].ItemID)
	require.True(t, index.requests[0].Filter.FeedCandidates)
	require.NotNil(t, index.requests[0].Filter.PublishedAfter)
	require.Empty(t, index.personalRequests)
	require.Equal(t, float64(1), result.Candidates[0].Signals["topic.accepted"])
	require.Contains(t, result.Candidates[0].Signals, "rrf.dense")
}
