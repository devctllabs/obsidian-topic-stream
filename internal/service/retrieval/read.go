package retrieval

import (
	"context"
	"errors"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/embeddingmodels"
	"github.com/google/uuid"
)

// readGeneration keeps query results inside the captured collection instance. A nil registry
// makes validation read-only; Managers additionally request recovery when they observe replacement.
func readGeneration[T any](ctx context.Context, runtime generationRuntime, registry embeddingmodels.CollectionRegistry, read func() (T, error)) (T, error) {
	var zero T
	if err := checkReadGeneration(ctx, runtime, registry); err != nil {
		return zero, err
	}
	result, err := read()
	if err != nil {
		return zero, err
	}
	if err := checkReadGeneration(ctx, runtime, registry); err != nil {
		return zero, err
	}
	return result, nil
}

func checkReadGeneration(ctx context.Context, runtime generationRuntime, registry embeddingmodels.CollectionRegistry) error {
	actual, err := runtime.index.CollectionIncarnation(ctx)
	if err != nil {
		return err
	}
	generation := runtime.generation
	if generation.Initialized && actual != uuid.Nil && actual == generation.CollectionIncarnation {
		return nil
	}
	if registry != nil {
		_, err = embeddingmodels.EnsureCollection(ctx, registry, domain.ModelCollectionObservation{WorkspaceID: generation.WorkspaceID, ModelID: generation.ModelID, Incarnation: generation.CollectionIncarnation}, runtime.index)
	}
	return errors.Join(domain.ErrRetrievalUnavailable, err)
}
