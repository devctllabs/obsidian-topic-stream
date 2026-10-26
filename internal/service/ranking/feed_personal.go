package ranking

import (
	"context"
	"errors"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

// FeedPersonalFactory opens personal retrieval against a captured serving generation.
type FeedPersonalFactory interface {
	// BuildPersonalIndex constructs personal-feedback retrieval for the captured generation.
	BuildPersonalIndex(generation domain.RetrievalIndex) (PersonalIndex, error)
}

// FeedRanker personalizes the deduplicated common pool using one captured serving generation.
type FeedRanker struct{ factory FeedPersonalFactory }

// NewFeedRanker constructs the common Feed personal-ranking pass.
func NewFeedRanker(factory FeedPersonalFactory) *FeedRanker { return &FeedRanker{factory: factory} }

// RankFeed applies one personal ranking pass to the common Feed candidate pool.
func (r *FeedRanker) RankFeed(ctx context.Context, request domain.FeedRankingRequest) ([]domain.FeedCandidate, error) {
	if len(request.Candidates) == 0 {
		return []domain.FeedCandidate{}, nil
	}
	settings := request.Settings.Retrieval
	if !validEngineSettings(settings) || len(request.Candidates) > settings.CandidateLimit {
		return nil, errors.New("invalid common Feed ranking budget")
	}
	index, err := r.factory.BuildPersonalIndex(request.Generation)
	if err != nil {
		return nil, err
	}
	values := make([]Candidate, 0, len(request.Candidates))
	originals := make(map[string]domain.FeedCandidate, len(request.Candidates))
	for _, candidate := range request.Candidates {
		id := candidate.ItemID.String()
		originals[id] = candidate
		values = append(values, Candidate{ItemID: id, SourceID: candidate.SourceID.String(), PublishedAt: candidate.ContentAt, Score: candidate.Score, Signals: candidate.Signals, Feedback: candidate.Feedback})
	}
	now := func() time.Time { return request.At }
	ranked, err := scoreCandidates(ctx, []Scorer{
		&PersonalKNN{Index: index, Filter: IndexFilter{WorkspaceID: request.WorkspaceID}, NeighborLimit: 12, Now: now},
		RecencyBoost{Now: now, HalfLife: time.Duration(settings.RecencyHalfLifeDays * float64(24*time.Hour)), MaxBoost: settings.RecencyMaxBoost},
		SourceCap{Limit: settings.FeedSourceCap},
	}, Profile{}, values)
	if err != nil {
		return nil, err
	}
	result := make([]domain.FeedCandidate, 0, len(ranked))
	for _, value := range ranked {
		candidate := originals[value.ItemID]
		candidate.Score = value.Score
		candidate.Signals = value.Signals
		result = append(result, candidate)
	}
	return result, nil
}
