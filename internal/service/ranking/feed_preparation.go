package ranking

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

// TopicCandidateIndex retrieves candidates and reusable gate embeddings for a Topic.
type TopicCandidateIndex interface {
	// RetrieveTopic retrieves dense, sparse, and correction evidence using one Topic index request.
	RetrieveTopic(ctx context.Context, request TopicIndexRequest) (TopicRetrieval, error)
}

// FeedIndexFactory opens Topic retrieval against a captured serving generation.
type FeedIndexFactory interface {
	// BuildFeedIndex constructs Topic candidate retrieval for the captured generation.
	BuildFeedIndex(generation domain.RetrievalIndex) (TopicCandidateIndex, error)
}

// FeedPreparer retrieves and gates bounded Topic candidates without publishing them.
type FeedPreparer struct {
	factory    FeedIndexFactory
	thresholds TopicThresholdProvider
	defaults   TopicThresholdDefaults
}

// NewFeedPreparer constructs bounded Topic candidate preparation with model-specific gates.
func NewFeedPreparer(factory FeedIndexFactory, thresholds TopicThresholdProvider, defaults TopicThresholdDefaults) *FeedPreparer {
	return &FeedPreparer{factory: factory, thresholds: thresholds, defaults: defaults}
}

// PrepareFeed retrieves and gates candidates using the captured Topic, settings, and generation.
func (p *FeedPreparer) PrepareFeed(ctx context.Context, request domain.FeedTopicPreparation) (domain.FeedTopicPrepared, error) {
	limit := request.Settings.Retrieval.CandidateLimit
	if limit < 1 || limit > 100 {
		return domain.FeedTopicPrepared{}, errors.New("invalid Feed candidate limit")
	}
	index, err := p.factory.BuildFeedIndex(request.Generation)
	if err != nil {
		return domain.FeedTopicPrepared{}, err
	}
	thresholds, err := p.thresholds.Thresholds(ctx, request.Topic, p.defaults)
	if err != nil {
		return domain.FeedTopicPrepared{}, err
	}
	after := request.At.AddDate(0, 0, -30)
	retrieved, err := index.RetrieveTopic(ctx, TopicIndexRequest{TopicID: request.Topic.ID, CorrectionEpoch: request.Topic.CorrectionEpoch, Include: request.Topic.Include, Exclude: request.Topic.Exclude,
		IncludeEmbedding: preparedVector(request.IncludeEmbedding), ExcludeEmbedding: preparedVector(request.ExcludeEmbedding),
		Filter: IndexFilter{WorkspaceID: request.Topic.WorkspaceID, SourceIDs: request.Topic.SourceIDs, Kinds: request.Topic.ItemKinds, FeedCandidates: true, PublishedAfter: &after, PublishedBefore: &request.At}, Limit: limit})
	if err != nil {
		return domain.FeedTopicPrepared{}, err
	}
	inputs := request.Inputs
	inputs.GateVersion = p.defaults.Version
	inputs.GateInclude = thresholds.Include
	inputs.GateExclude = thresholds.Exclude
	result := domain.FeedTopicPrepared{Inputs: inputs, Candidates: []domain.FeedCandidate{}, IncludeEmbedding: float64Vector(retrieved.IncludeEmbedding), ExcludeEmbedding: float64Vector(retrieved.ExcludeEmbedding)}
	candidates, err := preparedTopicCandidates(ctx, request, retrieved, thresholds)
	if err != nil {
		return domain.FeedTopicPrepared{}, err
	}
	for position, candidate := range candidates {
		id, err := uuid.Parse(candidate.ItemID)
		if err != nil {
			return domain.FeedTopicPrepared{}, fmt.Errorf("invalid candidate Item ID: %w", err)
		}
		result.Candidates = append(result.Candidates, domain.FeedCandidate{WorkspaceID: request.Topic.WorkspaceID, TopicID: request.Topic.ID, ItemID: id, Position: position + 1, Score: candidate.Score, Signals: candidate.Signals, Inputs: inputs})
	}
	return result, nil
}

func preparedTopicCandidates(ctx context.Context, request domain.FeedTopicPreparation, retrieved TopicRetrieval, thresholds TopicThresholds) ([]Candidate, error) {
	corrections := currentTopicCorrections(retrieved, request.Topic)
	retrieved = mergeManualIncludes(retrieved, corrections)
	retrievers := topicRetrievers(retrieved)
	if len(retrievers) == 0 {
		return []Candidate{}, nil
	}
	pipeline := Pipeline{Retrievers: retrievers, Gate: TopicSemanticGate{IncludeEmbedding: float64Vector(retrieved.IncludeEmbedding), ExcludeEmbedding: float64Vector(retrieved.ExcludeEmbedding), Thresholds: thresholds, Corrections: correctionKeys(corrections)},
		Fusion: ReciprocalRankFusion{K: request.Settings.Retrieval.RRFK}, Reranker: IdentityReranker{}, Diversifier: TopN{}}
	return pipeline.Recommend(ctx, Request{CandidateLimit: request.Settings.Retrieval.CandidateLimit, ResultLimit: request.Settings.Retrieval.CandidateLimit})
}
func preparedVector(values []float64) []float32 {
	result := make([]float32, len(values))
	for i, value := range values {
		result[i] = float32(value)
	}
	return result
}
