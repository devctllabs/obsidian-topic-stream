//go:build integration

package vectorindex_test

import (
	"context"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
)

type ensureCountingBackend struct {
	*qdrant.Client
	writes int
}

// CreateFieldIndex counts schema writes while forwarding them to the real Qdrant server.
func (backend *ensureCountingBackend) CreateFieldIndex(ctx context.Context, request *qdrant.CreateFieldIndexCollection) (*qdrant.UpdateResult, error) {
	backend.writes++
	return backend.Client.CreateFieldIndex(ctx, request)
}

func TestQdrantEnsureRepairsRemovedPayloadIndexAndReplacedCollection(t *testing.T) {
	fixture := newIndexFixture(t)
	backend := &ensureCountingBackend{Client: fixture.client}
	index, err := vectorindex.New(backend, fixture.collection, 3)
	require.NoError(t, err)
	for range 3 {
		require.NoError(t, index.Ensure(t.Context()))
	}
	require.Zero(t, backend.writes)

	_, err = fixture.client.DeleteFieldIndex(t.Context(), &qdrant.DeleteFieldIndexCollection{
		CollectionName: fixture.collection, FieldName: "hidden", Wait: qdrant.PtrOf(true),
	})
	require.NoError(t, err)
	require.NoError(t, index.Ensure(t.Context()))
	require.Equal(t, 1, backend.writes)
	require.NoError(t, index.Ensure(t.Context()))
	require.Equal(t, 1, backend.writes)

	before, err := index.CollectionIncarnation(t.Context())
	require.NoError(t, err)
	require.NoError(t, fixture.client.DeleteCollection(t.Context(), fixture.collection))
	modifier := qdrant.Modifier_Idf
	require.NoError(t, fixture.client.CreateCollection(t.Context(), &qdrant.CreateCollection{
		CollectionName: fixture.collection,
		VectorsConfig: qdrant.NewVectorsConfigMap(map[string]*qdrant.VectorParams{
			vectorindex.DenseVectorName: {Size: 3, Distance: qdrant.Distance_Cosine},
		}),
		SparseVectorsConfig: qdrant.NewSparseVectorsConfig(map[string]*qdrant.SparseVectorParams{
			vectorindex.SparseVectorName: {Modifier: &modifier},
		}),
	}))
	unmarked, err := index.CollectionIncarnation(t.Context())
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, unmarked)
	require.NoError(t, index.Ensure(t.Context()))
	require.Equal(t, 15, backend.writes)
	marker := uuid.New()
	require.NoError(t, index.SetCollectionIncarnation(t.Context(), marker))
	after, err := index.CollectionIncarnation(t.Context())
	require.NoError(t, err)
	require.Equal(t, marker, after)
	require.NotEqual(t, before, after)
	require.NoError(t, index.Ensure(t.Context()))
	require.Equal(t, 15, backend.writes)
	reader, err := vectorindex.New(fixture.client, fixture.collection, 3)
	require.NoError(t, err)
	persisted, err := reader.CollectionIncarnation(t.Context())
	require.NoError(t, err)
	require.Equal(t, marker, persisted, "a new reader observes the persisted legacy collection marker")
}
