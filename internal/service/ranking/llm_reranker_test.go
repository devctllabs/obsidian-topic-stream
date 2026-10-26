package ranking_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type llmClientStub struct {
	order   []string
	err     error
	request ranking.LLMRerankRequest
}

// Rerank records the request and returns the configured candidate order or failure.
func (s *llmClientStub) Rerank(_ context.Context, request ranking.LLMRerankRequest) ([]string, error) {
	s.request = request
	return append([]string(nil), s.order...), s.err
}

type rankingItemStoreStub struct {
	items map[uuid.UUID]domain.Item
}

// GetItem returns the fixture Item and its lookup result.
func (s rankingItemStoreStub) GetItem(_ context.Context, _ uuid.UUID, itemID uuid.UUID) (domain.Item, bool, error) {
	item, found := s.items[itemID]
	return item, found, nil
}

func TestDigestLLMRerankerSendsBoundedContextAndFusesReturnedOrder(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	items := make(map[uuid.UUID]domain.Item, len(ids))
	candidates := make([]ranking.Candidate, len(ids))
	for index, itemID := range ids {
		items[itemID] = domain.Item{ID: itemID, WorkspaceID: workspaceID, Title: "Item " + itemID.String(), Excerpt: "Summary"}
		candidates[index] = ranking.Candidate{ItemID: itemID.String(), Score: float64(3 - index), Signals: map[string]float64{}}
	}
	comment := "Practical detail"
	canonicalFeedback := domain.Feedback{Sentiment: "like", Comment: &comment, UpdatedAt: time.Now()}
	item := items[ids[0]]
	item.Feedback = &canonicalFeedback
	items[ids[0]] = item
	staleComment := "stale projected comment"
	liked := domain.ItemFeedback{
		ItemID: ids[0], Feedback: domain.Feedback{Sentiment: "like", Comment: &staleComment, UpdatedAt: time.Now()},
	}
	client := &llmClientStub{order: []string{ids[2].String(), ids[0].String(), ids[1].String()}}
	reranker := ranking.DigestLLMReranker{
		Client: client, Items: rankingItemStoreStub{items: items}, WorkspaceID: workspaceID,
		Topic: domain.Topic{ID: uuid.New(), Revision: 3}, Profile: domain.PreferenceProfile{Prefer: "systems", Revision: 2},
		Positive: []ranking.FeedbackNeighborhood{{
			Feedback: liked, Hits: []ranking.IndexHit{{ItemID: ids[1], Score: 0.8}},
		}},
		Instruction: "Rank for technical depth.", Limit: 3, Timeout: time.Second,
	}

	result, err := reranker.Rerank(t.Context(), ranking.Profile{}, candidates)

	require.NoError(t, err)
	require.Len(t, result, 3)
	require.Equal(t, []string{ids[0].String(), ids[1].String(), ids[2].String()}, []string{client.request.Candidates[0].ID, client.request.Candidates[1].ID, client.request.Candidates[2].ID})
	require.Equal(t, "systems", client.request.Profile.Prefer)
	require.Equal(t, 3, client.request.TopicRevision)
	require.Len(t, client.request.Likes, 1)
	require.Equal(t, &comment, client.request.Likes[0].Feedback.Comment)
	for _, candidate := range result {
		require.Contains(t, candidate.Signals, "rrf.deterministic")
		require.Contains(t, candidate.Signals, "rrf.llm")
	}
}

func TestDigestLLMRerankerSkipsFeedbackThatIsNoLongerCurrent(t *testing.T) {
	t.Parallel()

	for name, current := range map[string]*domain.Feedback{
		"removed":          nil,
		"changed polarity": {Sentiment: "dislike", UpdatedAt: time.Now()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			workspaceID, candidateID, feedbackID := uuid.New(), uuid.New(), uuid.New()
			client := &llmClientStub{order: []string{candidateID.String()}}
			reranker := ranking.DigestLLMReranker{
				Client: client,
				Items: rankingItemStoreStub{items: map[uuid.UUID]domain.Item{
					candidateID: {ID: candidateID, WorkspaceID: workspaceID},
					feedbackID:  {ID: feedbackID, WorkspaceID: workspaceID, Feedback: current},
				}},
				WorkspaceID: workspaceID, Topic: domain.Topic{ID: uuid.New()}, Limit: 1, Timeout: time.Second,
				Positive: []ranking.FeedbackNeighborhood{{
					Feedback: domain.ItemFeedback{
						ItemID: feedbackID, Feedback: domain.Feedback{Sentiment: "like", UpdatedAt: time.Now()},
					},
					Hits: []ranking.IndexHit{{ItemID: candidateID, Score: 0.8}},
				}},
			}

			_, err := reranker.Rerank(t.Context(), ranking.Profile{}, []ranking.Candidate{{
				ItemID: candidateID.String(), Score: 1,
			}})

			require.NoError(t, err)
			require.Empty(t, client.request.Likes)
		})
	}
}

func TestDigestLLMRerankerFallsBackOnProviderOrInvalidOutput(t *testing.T) {
	t.Parallel()

	workspaceID, itemID := uuid.New(), uuid.New()
	candidate := ranking.Candidate{ItemID: itemID.String(), Score: 1}
	for name, client := range map[string]*llmClientStub{
		"provider": {err: errors.New("unavailable")},
		"invalid":  {order: []string{uuid.NewString()}},
	} {
		t.Run(name, func(t *testing.T) {
			reranker := ranking.DigestLLMReranker{
				Client: client, Items: rankingItemStoreStub{items: map[uuid.UUID]domain.Item{
					itemID: {ID: itemID, WorkspaceID: workspaceID},
				}},
				WorkspaceID: workspaceID, Topic: domain.Topic{ID: uuid.New()}, Limit: 1, Timeout: time.Second,
			}

			result, err := reranker.Rerank(t.Context(), ranking.Profile{}, []ranking.Candidate{candidate})

			require.NoError(t, err)
			require.Equal(t, itemID.String(), result[0].ItemID)
			require.Equal(t, 1.0, result[0].Signals["llm.fallback"])
		})
	}
}
