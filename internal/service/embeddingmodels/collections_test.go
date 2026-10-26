package embeddingmodels_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/embeddingmodels"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type collectionRegistryStub struct {
	locked       bool
	locks        int
	observations []domain.ModelCollectionObservation
}

// WithModelCollectionLock protects only initialization of an absent marker.
func (s *collectionRegistryStub) WithModelCollectionLock(ctx context.Context, _ uuid.UUID, action func(context.Context) error) error {
	s.locks++
	s.locked = true
	defer func() { s.locked = false }()
	return action(ctx)
}

// ObserveModelCollection records the actual identity reported by the index.
func (s *collectionRegistryStub) ObserveModelCollection(_ context.Context, value domain.ModelCollectionObservation) error {
	if !s.locked {
		return errors.New("collection replacement observation lacks coordination")
	}
	s.observations = append(s.observations, value)
	return nil
}

type collectionIndexStub struct {
	incarnation uuid.UUID
	registry    *collectionRegistryStub
	stamps      int
}

// Ensure represents successful collection schema validation.
func (s *collectionIndexStub) Ensure(context.Context) error { return nil }

// CollectionIncarnation reads the current immutable marker.
func (s *collectionIndexStub) CollectionIncarnation(context.Context) (uuid.UUID, error) {
	return s.incarnation, nil
}

// SetCollectionIncarnation refuses an uncoordinated metadata mutation.
func (s *collectionIndexStub) SetCollectionIncarnation(_ context.Context, value uuid.UUID) error {
	if !s.registry.locked {
		return errors.New("marker write lacks coordination")
	}
	s.stamps++
	s.incarnation = value
	return nil
}

func TestCollectionEnsureObservesReplacementAndSkipsMatchingRegistry(t *testing.T) {
	registry := &collectionRegistryStub{}
	index := &collectionIndexStub{incarnation: uuid.New(), registry: registry}
	known := domain.ModelCollectionObservation{WorkspaceID: uuid.New(), ModelID: uuid.New(), Incarnation: uuid.New()}
	actual, err := embeddingmodels.EnsureCollection(t.Context(), registry, known, index)
	require.NoError(t, err)
	require.Equal(t, index.incarnation, actual)
	known.Incarnation = actual
	require.Equal(t, []domain.ModelCollectionObservation{known}, registry.observations)
	_, err = embeddingmodels.EnsureCollection(t.Context(), registry, known, index)
	require.NoError(t, err)
	require.Len(t, registry.observations, 1, "matching snapshots need no additional registry work")
	require.Equal(t, 1, registry.locks, "matching snapshots need no further coordination")
}

func TestCollectionEnsureInitializesMissingMarkerUnderLock(t *testing.T) {
	registry := &collectionRegistryStub{}
	index := &collectionIndexStub{registry: registry}
	known := domain.ModelCollectionObservation{WorkspaceID: uuid.New(), ModelID: uuid.New()}
	actual, err := embeddingmodels.EnsureCollection(t.Context(), registry, known, index)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, actual)
	require.Equal(t, 1, index.stamps)
	require.Equal(t, 1, registry.locks)
	require.Len(t, registry.observations, 1)
}
