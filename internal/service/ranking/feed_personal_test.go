package ranking_test

import (
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type personalFactory struct{ index *topicIndexStub }

// BuildPersonalIndex returns the fixture's personal index without opening resources.
func (f personalFactory) BuildPersonalIndex(domain.RetrievalIndex) (ranking.PersonalIndex, error) {
	return f.index, nil
}
func TestFeedPersonalizationUsesOneBatchAndKeepsSourceOverflowWithoutRefill(t *testing.T) {
	ids := []uuid.UUID{uuid.MustParse("00000000-0000-4000-8000-000000000001"), uuid.MustParse("00000000-0000-4000-8000-000000000002"), uuid.MustParse("00000000-0000-4000-8000-000000000003"), uuid.MustParse("00000000-0000-4000-8000-000000000004")}
	now := time.Now().UTC()
	workspaceID, firstSource, secondSource := uuid.New(), uuid.New(), uuid.New()
	index := &topicIndexStub{personalResult: ranking.PersonalRetrieval{Direct: map[uuid.UUID]domain.Feedback{ids[2]: {Sentiment: "dislike"}}}}
	ranker := ranking.NewFeedRanker(personalFactory{index: index})
	settings := validRetrievalSettings(4)
	settings.FeedSourceCap = 1
	settings.RecencyMaxBoost = 0
	candidates := make([]domain.FeedCandidate, 0, 4)
	for _, id := range ids {
		candidates = append(candidates, domain.FeedCandidate{ItemID: id, SourceID: firstSource, Score: 1, ContentAt: now})
	}
	candidates[0].Feedback = &domain.Feedback{Sentiment: "like"}
	candidates[3].SourceID = secondSource
	result, err := ranker.RankFeed(t.Context(), domain.FeedRankingRequest{WorkspaceID: workspaceID, Settings: domain.WorkspaceSettings{Retrieval: settings}, Candidates: candidates, At: now})
	require.NoError(t, err)
	require.Len(t, index.personalRequests, 1)
	require.Equal(t, ids, index.personalRequests[0].CandidateIDs)
	require.False(t, index.personalRequests[0].Filter.FeedCandidates)
	require.Len(t, result, 3)
	require.Equal(t, []uuid.UUID{ids[0], ids[3], ids[1]}, []uuid.UUID{result[0].ItemID, result[1].ItemID, result[2].ItemID})
	require.Equal(t, float64(1), result[0].Signals["personal.directLike"])
}
