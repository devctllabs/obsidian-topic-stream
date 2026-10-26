package ranking

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

const (
	DigestLLMRerankerVersion  = "digest-llm-reranker-v1"
	DigestRerankPromptVersion = "digest-rerank-prompt-v1"
	llmFeedbackLimit          = 3
)

// LLMRerankItem is the bounded Item metadata exposed to the reranking prompt.
type LLMRerankItem struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Excerpt     string          `json:"excerpt"`
	Kind        domain.ItemKind `json:"kind"`
	PublishedAt *time.Time      `json:"publishedAt"`
}

// LLMFeedbackExample pairs prompt-safe Item metadata with current personal feedback.
type LLMFeedbackExample struct {
	Item     LLMRerankItem   `json:"item"`
	Feedback domain.Feedback `json:"feedback"`
}

// LLMRerankRequest carries candidate metadata, preference guidance, and bounded feedback evidence.
type LLMRerankRequest struct {
	Instruction   string                   `json:"instruction"`
	TopicID       uuid.UUID                `json:"topicId"`
	TopicRevision int                      `json:"topicRevision"`
	Profile       domain.PreferenceProfile `json:"profile"`
	Candidates    []LLMRerankItem          `json:"candidates"`
	Likes         []LLMFeedbackExample     `json:"likes"`
	Dislikes      []LLMFeedbackExample     `json:"dislikes"`
}

// LLMRerankClient orders a bounded set of candidate IDs using generation.
type LLMRerankClient interface {
	// Rerank returns ordered candidate IDs; consumers must validate the returned ID set.
	Rerank(ctx context.Context, request LLMRerankRequest) ([]string, error)
}

// RankingItemStore loads canonical Item content for reranking prompts.
type RankingItemStore interface {
	// GetItem returns the Workspace Item and whether it exists.
	GetItem(ctx context.Context, workspaceID uuid.UUID, itemID uuid.UUID) (domain.Item, bool, error)
}

// FeedbackEvidence exposes the feedback neighborhoods used by personal scoring.
type FeedbackEvidence interface {
	// FeedbackNeighborhoods returns the positive and negative feedback evidence used for scoring.
	FeedbackNeighborhoods() ([]FeedbackNeighborhood, []FeedbackNeighborhood)
}

// DigestLLMReranker is fail-open: invalid configuration, unavailable Item context, model errors, or
// an invalid permutation retain the deterministic shortlist and add an llm.fallback signal.
type DigestLLMReranker struct {
	Client      LLMRerankClient
	Items       RankingItemStore
	WorkspaceID uuid.UUID
	Topic       domain.Topic
	Profile     domain.PreferenceProfile
	Positive    []FeedbackNeighborhood
	Negative    []FeedbackNeighborhood
	Evidence    FeedbackEvidence
	Instruction string
	Limit       int
	Timeout     time.Duration
}

// Rerank blends deterministic and generated ranks for a bounded shortlist, falling back to deterministic
// order on failure.
func (r DigestLLMReranker) Rerank(
	ctx context.Context,
	_ Profile,
	candidates []Candidate,
) ([]Candidate, error) {
	limit := min(r.Limit, len(candidates))
	shortlist := cloneCandidates(candidates[:limit])
	if err := r.validate(); err != nil || limit == 0 {
		return llmFallback(shortlist), nil
	}
	if r.Evidence != nil {
		r.Positive, r.Negative = r.Evidence.FeedbackNeighborhoods()
	}
	request, err := r.buildRequest(ctx, shortlist)
	if err != nil {
		return llmFallback(shortlist), nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	order, err := r.Client.Rerank(requestCtx, request)
	if err != nil || !validLLMOrder(order, shortlist) {
		return llmFallback(shortlist), nil
	}
	llmOrder := candidatesByID(shortlist, order)
	return (ReciprocalRankFusion{
		K: 60, Weights: map[string]float64{"deterministic": 1, "llm": 0.5},
	}).Fuse([]Retrieval{
		{Name: "deterministic", Candidates: shortlist},
		{Name: "llm", Candidates: llmOrder},
	})
}

func (r DigestLLMReranker) validate() error {
	if r.Client == nil || r.Items == nil || r.WorkspaceID == uuid.Nil || r.Topic.ID == uuid.Nil {
		return errors.New("digest LLM reranker dependencies are required")
	}
	if r.Limit <= 0 || r.Timeout <= 0 {
		return errors.New("digest LLM reranker limits are invalid")
	}
	return nil
}

func (r DigestLLMReranker) buildRequest(
	ctx context.Context,
	candidates []Candidate,
) (LLMRerankRequest, error) {
	items, err := r.loadCandidates(ctx, candidates)
	if err != nil {
		return LLMRerankRequest{}, err
	}
	likes, err := r.loadFeedback(ctx, relevantFeedback(r.Positive, candidates))
	if err != nil {
		return LLMRerankRequest{}, err
	}
	dislikes, err := r.loadFeedback(ctx, relevantFeedback(r.Negative, candidates))
	if err != nil {
		return LLMRerankRequest{}, err
	}
	return LLMRerankRequest{
		Instruction: r.Instruction, TopicID: r.Topic.ID, TopicRevision: r.Topic.Revision,
		Profile: r.Profile, Candidates: items, Likes: likes, Dislikes: dislikes,
	}, nil
}

func (r DigestLLMReranker) loadCandidates(
	ctx context.Context,
	candidates []Candidate,
) ([]LLMRerankItem, error) {
	result := make([]LLMRerankItem, 0, len(candidates))
	for _, candidate := range candidates {
		itemID, err := uuid.Parse(candidate.ItemID)
		if err != nil {
			return nil, err
		}
		item, found, err := r.Items.GetItem(ctx, r.WorkspaceID, itemID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errors.New("ranking item not found")
		}
		result = append(result, llmItem(item))
	}
	return result, nil
}

func (r DigestLLMReranker) loadFeedback(
	ctx context.Context,
	feedback []domain.ItemFeedback,
) ([]LLMFeedbackExample, error) {
	result := make([]LLMFeedbackExample, 0, len(feedback))
	for _, value := range feedback {
		item, found, err := r.Items.GetItem(ctx, r.WorkspaceID, value.ItemID)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		if item.Feedback == nil || item.Feedback.Sentiment != value.Feedback.Sentiment {
			continue
		}
		result = append(result, LLMFeedbackExample{Item: llmItem(item), Feedback: *item.Feedback})
	}
	return result, nil
}

func llmItem(item domain.Item) LLMRerankItem {
	return LLMRerankItem{
		ID: item.ID.String(), Title: item.Title, Excerpt: item.Excerpt,
		Kind: item.Kind, PublishedAt: item.PublishedAt,
	}
}

type scoredFeedback struct {
	value domain.ItemFeedback
	score float64
}

func relevantFeedback(neighborhoods []FeedbackNeighborhood, candidates []Candidate) []domain.ItemFeedback {
	candidateIDs := make(map[uuid.UUID]struct{}, len(candidates))
	for _, candidate := range candidates {
		if itemID, err := uuid.Parse(candidate.ItemID); err == nil {
			candidateIDs[itemID] = struct{}{}
		}
	}
	scored := make([]scoredFeedback, 0, len(neighborhoods))
	for _, neighborhood := range neighborhoods {
		var relevance float64
		for _, hit := range neighborhood.Hits {
			if _, found := candidateIDs[hit.ItemID]; found {
				relevance = max(relevance, hit.Score)
			}
		}
		if relevance > 0 {
			scored = append(scored, scoredFeedback{value: neighborhood.Feedback, score: relevance})
		}
	}
	sort.SliceStable(scored, func(left, right int) bool { return scored[left].score > scored[right].score })
	if len(scored) > llmFeedbackLimit {
		scored = scored[:llmFeedbackLimit]
	}
	result := make([]domain.ItemFeedback, len(scored))
	for index, value := range scored {
		result[index] = value.value
	}
	return result
}

func validLLMOrder(order []string, candidates []Candidate) bool {
	if len(order) != len(candidates) {
		return false
	}
	wanted := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		wanted[candidate.ItemID] = struct{}{}
	}
	for _, itemID := range order {
		if _, found := wanted[itemID]; !found {
			return false
		}
		delete(wanted, itemID)
	}
	return len(wanted) == 0
}

func candidatesByID(candidates []Candidate, order []string) []Candidate {
	byID := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.ItemID] = candidate
	}
	result := make([]Candidate, len(order))
	for index, itemID := range order {
		result[index] = byID[itemID]
	}
	return result
}

func llmFallback(candidates []Candidate) []Candidate {
	result := cloneCandidates(candidates)
	for index := range result {
		ensureSignals(&result[index])["llm.fallback"] = 1
	}
	return result
}
