package retrieval

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
)

// Reader queries an existing retrieval generation without reconciling or
// creating derived state. It is intended for read-only tooling such as bench.
type Reader struct {
	runtime generationRuntime
}

// ReaderConfig selects an existing active generation and the factory used to query it.
type ReaderConfig struct {
	Generation domain.RetrievalIndex
	Factory    Factory
}

// NewReader opens the captured active generation without reconciling or creating derived state.
func NewReader(config ReaderConfig) (*Reader, error) {
	if config.Factory == nil || config.Generation.Generation < 1 ||
		config.Generation.Status != domain.RetrievalIndexActive {
		return nil, errors.New("active retrieval generation and factory are required")
	}
	if !config.Generation.Initialized {
		return nil, domain.ErrRetrievalUnavailable
	}
	runtime, err := buildRuntime(config.Factory, config.Generation)
	if err != nil {
		return nil, err
	}
	return &Reader{runtime: runtime}, nil
}

// QueryDenseText embeds the text and retrieves bounded dense matches satisfying the filter.
func (r *Reader) QueryDenseText(
	ctx context.Context,
	text string,
	filter ranking.IndexFilter,
	limit int,
) ([]ranking.IndexHit, error) {
	vector, err := r.runtime.embedder.Embed(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("embed query for retrieval generation %d: %w", r.runtime.generation.Generation, err)
	}
	return readGeneration(ctx, r.runtime, nil, func() ([]ranking.IndexHit, error) { return r.runtime.index.QueryDense(ctx, vector, filter, limit) })
}

// QuerySparse retrieves bounded lexical matches satisfying the filter.
func (r *Reader) QuerySparse(
	ctx context.Context,
	text string,
	filter ranking.IndexFilter,
	limit int,
) ([]ranking.IndexHit, error) {
	return readGeneration(ctx, r.runtime, nil, func() ([]ranking.IndexHit, error) { return r.runtime.index.QuerySparse(ctx, text, filter, limit) })
}

// RetrievePersonal returns direct feedback and positive and negative similarity neighborhoods.
func (r *Reader) RetrievePersonal(
	ctx context.Context,
	request ranking.PersonalIndexRequest,
) (ranking.PersonalRetrieval, error) {
	return readGeneration(ctx, r.runtime, nil, func() (ranking.PersonalRetrieval, error) { return r.runtime.index.RetrievePersonal(ctx, request) })
}

// RetrieveTopic uses the captured generation for both retrieval and reusable gate embeddings.
func (r *Reader) RetrieveTopic(ctx context.Context, request ranking.TopicIndexRequest) (ranking.TopicRetrieval, error) {
	return readGeneration(ctx, r.runtime, nil, func() (ranking.TopicRetrieval, error) { return retrieveTopic(ctx, r.runtime, request) })
}
