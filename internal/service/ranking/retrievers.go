package ranking

import (
	"context"
	"errors"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

// IndexFilter scopes retrieval before backend limits, including optional Feed publication exclusion.
type IndexFilter struct {
	// FeedCandidates excludes already published Items before backend limits; Search and Digest leave it
	// false.
	FeedCandidates  bool
	WorkspaceID     uuid.UUID
	SourceIDs       []uuid.UUID
	Kinds           []domain.ItemKind
	PublishedAfter  *time.Time
	PublishedBefore *time.Time
}

// IndexHit carries indexed identity, score, vectors, and current feedback and correction evidence.
type IndexHit struct {
	ItemID           uuid.UUID
	SourceID         uuid.UUID
	PublishedAt      *time.Time
	Embedding        []float32
	Score            float64
	Feedback         *domain.Feedback
	TopicCorrections []domain.TopicCorrectionProjection
}

// Index retrieves dense-text and lexical candidates under common filters.
type Index interface {
	// QueryDenseText embeds the text and retrieves bounded dense matches satisfying the filter.
	QueryDenseText(ctx context.Context, text string, filter IndexFilter, limit int) ([]IndexHit, error)
	// QuerySparse retrieves bounded lexical matches satisfying the filter.
	QuerySparse(ctx context.Context, text string, filter IndexFilter, limit int) ([]IndexHit, error)
}

// RecentIndex retrieves chronological candidates without a text query.
type RecentIndex interface {
	// QueryRecent returns recent Items satisfying the filter, bounded by limit.
	QueryRecent(ctx context.Context, filter IndexFilter, limit int) ([]IndexHit, error)
}

// DenseRetriever retrieves semantic matches for the profile's Prefer text.
type DenseRetriever struct {
	index  Index
	filter IndexFilter
}

// NewDenseRetriever binds semantic retrieval to a fixed index filter.
func NewDenseRetriever(index Index, filter IndexFilter) DenseRetriever {
	return DenseRetriever{index: index, filter: filter}
}

// Name returns the stable retriever name used by fusion weights and score signals.
func (DenseRetriever) Name() string { return "dense" }

// Retrieve returns a bounded candidate list for the preference guidance.
func (r DenseRetriever) Retrieve(ctx context.Context, profile Profile, limit int) ([]Candidate, error) {
	if r.index == nil {
		return nil, errors.New("dense retriever is not configured")
	}
	hits, err := r.index.QueryDenseText(ctx, profile.Prefer, r.filter, limit)
	if err != nil {
		return nil, err
	}
	return candidatesFromHits(hits, r.Name()), nil
}

// SparseRetriever retrieves lexical matches for the profile's Prefer text.
type SparseRetriever struct {
	index  Index
	filter IndexFilter
}

// NewSparseRetriever binds lexical retrieval to a fixed index filter.
func NewSparseRetriever(index Index, filter IndexFilter) SparseRetriever {
	return SparseRetriever{index: index, filter: filter}
}

// Name returns the stable retriever name used by fusion weights and score signals.
func (SparseRetriever) Name() string { return "sparse" }

// Retrieve returns a bounded candidate list for the preference guidance.
func (r SparseRetriever) Retrieve(ctx context.Context, profile Profile, limit int) ([]Candidate, error) {
	if r.index == nil {
		return nil, errors.New("sparse retriever is not configured")
	}
	hits, err := r.index.QuerySparse(ctx, profile.Prefer, r.filter, limit)
	if err != nil {
		return nil, err
	}
	return candidatesFromHits(hits, r.Name()), nil
}

// RecentRetriever provides chronological candidates without requiring query text.
type RecentRetriever struct {
	index  RecentIndex
	filter IndexFilter
}

// NewRecentRetriever binds chronological retrieval to a fixed index filter.
func NewRecentRetriever(index RecentIndex, filter IndexFilter) RecentRetriever {
	return RecentRetriever{index: index, filter: filter}
}

// Name returns the stable retriever name used by fusion weights and score signals.
func (RecentRetriever) Name() string { return "recent" }

// Retrieve returns bounded chronological candidates under the captured filter.
func (r RecentRetriever) Retrieve(ctx context.Context, _ Profile, limit int) ([]Candidate, error) {
	if r.index == nil {
		return nil, errors.New("recent retriever is not configured")
	}
	hits, err := r.index.QueryRecent(ctx, r.filter, limit)
	if err != nil {
		return nil, err
	}
	return candidatesFromHits(hits, r.Name()), nil
}

func candidatesFromHits(hits []IndexHit, signal string) []Candidate {
	result := make([]Candidate, len(hits))
	for index, hit := range hits {
		var publishedAt time.Time
		if hit.PublishedAt != nil {
			publishedAt = *hit.PublishedAt
		}
		embedding := make([]float64, len(hit.Embedding))
		for vectorIndex, value := range hit.Embedding {
			embedding[vectorIndex] = float64(value)
		}
		result[index] = Candidate{
			ItemID: hit.ItemID.String(), SourceID: hit.SourceID.String(), PublishedAt: publishedAt,
			Embedding: embedding, Score: hit.Score, Signals: map[string]float64{signal: hit.Score},
			Feedback: hit.Feedback,
		}
	}
	return result
}
