package embeddingmodels

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

// CollectionIndex initializes and observes the durable identity of one derived model collection.
type CollectionIndex interface {
	// Ensure validates or creates the model's vector and payload schema.
	Ensure(ctx context.Context) error
	// CollectionIncarnation returns zero for an existing collection without an ownership marker.
	CollectionIncarnation(ctx context.Context) (uuid.UUID, error)
	// SetCollectionIncarnation stamps an unmarked collection under the registry's coordination lock.
	SetCollectionIncarnation(ctx context.Context, incarnation uuid.UUID) error
}

// CollectionRegistry coordinates first markers and invalidates replaced synchronization state.
type CollectionRegistry interface {
	// WithModelCollectionLock serializes marker initialization without holding Workspace locks over I/O.
	WithModelCollectionLock(ctx context.Context, modelID uuid.UUID, action func(context.Context) error) error
	// ObserveModelCollection records the observed incarnation and refreshes the caller's model state.
	ObserveModelCollection(ctx context.Context, observation domain.ModelCollectionObservation) error
}

// EnsureCollection validates schema and records changes from the caller's last known incarnation.
func EnsureCollection(ctx context.Context, registry CollectionRegistry, known domain.ModelCollectionObservation, index CollectionIndex) (uuid.UUID, error) {
	if err := index.Ensure(ctx); err != nil {
		return uuid.Nil, err
	}
	actual, err := index.CollectionIncarnation(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if actual == known.Incarnation && actual != uuid.Nil {
		return actual, nil
	}
	err = registry.WithModelCollectionLock(ctx, known.ModelID, func(lockCtx context.Context) error {
		actual, err = initializeCollectionMarker(lockCtx, index)
		if err != nil {
			return err
		}
		known.Incarnation = actual
		return registry.ObserveModelCollection(lockCtx, known)
	})
	if err != nil {
		return uuid.Nil, err
	}
	return actual, nil
}

func initializeCollectionMarker(ctx context.Context, index CollectionIndex) (uuid.UUID, error) {
	actual, err := index.CollectionIncarnation(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if actual == uuid.Nil {
		if err := index.SetCollectionIncarnation(ctx, uuid.New()); err != nil {
			return uuid.Nil, err
		}
		actual, err = index.CollectionIncarnation(ctx)
		if err != nil {
			return uuid.Nil, err
		}
	}
	if actual == uuid.Nil {
		return uuid.Nil, fmt.Errorf("collection identity missing after initialization: %w", domain.ErrConflict)
	}
	return actual, nil
}
