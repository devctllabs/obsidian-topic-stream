//go:build integration

package vectorindex_test

import (
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
)

func TestQdrantConfiguredPolicyReconcilesAndRollsBackWithoutReplacingData(t *testing.T) {
	f := newIndexFixture(t)
	// Exercise configured creation, then reconcile/rollback that same owned collection with data.
	require.NoError(t, f.client.DeleteCollection(t.Context(), f.collection))
	policy := vectorindex.SearchPolicy{FullScanThresholdKB: 100000, HNSWEf: 256, Acorn: true, MaxSelectivity: .4}
	index, err := vectorindex.NewWithSearchPolicy(f.client, f.collection, 3, policy)
	require.NoError(t, err)
	require.NoError(t, index.Ensure(t.Context()))
	incarnation, err := index.CollectionIncarnation(t.Context())
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, incarnation)
	workspace, itemID := uuid.New(), uuid.New()
	require.NoError(t, index.UpsertItem(t.Context(), domain.Item{ID: itemID, WorkspaceID: workspace, Kind: domain.ItemKindArticle, DiscoveredAt: time.Now()}, "policy", []float32{1, 0, 0}))
	before, err := index.DenseVectors(t.Context(), workspace, []uuid.UUID{itemID})
	require.NoError(t, err)
	// Simulate an existing model collection still using the previous operator setting.
	require.NoError(t, f.client.UpdateCollection(t.Context(), &qdrant.UpdateCollection{CollectionName: f.collection, HnswConfig: &qdrant.HnswConfigDiff{FullScanThreshold: qdrant.PtrOf(uint64(10000))}}))
	require.NoError(t, index.Ensure(t.Context()))
	require.NoError(t, index.Ensure(t.Context()))
	info, err := f.client.GetCollectionInfo(t.Context(), f.collection)
	require.NoError(t, err)
	require.Equal(t, uint64(100000), info.Config.HnswConfig.GetFullScanThreshold())
	hits, err := index.QueryDense(t.Context(), []float32{1, 0, 0}, vectorindex.Filter{WorkspaceID: workspace}, 20)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, itemID, hits[0].ItemID)
	rollback, err := vectorindex.NewWithSearchPolicy(f.client, f.collection, 3, vectorindex.SearchPolicy{FullScanThresholdKB: 10000})
	require.NoError(t, err)
	require.NoError(t, rollback.Ensure(t.Context()))
	info, err = f.client.GetCollectionInfo(t.Context(), f.collection)
	require.NoError(t, err)
	require.Equal(t, uint64(10000), info.Config.HnswConfig.GetFullScanThreshold())
	after, err := index.DenseVectors(t.Context(), workspace, []uuid.UUID{itemID})
	require.NoError(t, err)
	require.Equal(t, before, after)
	current, err := index.CollectionIncarnation(t.Context())
	require.NoError(t, err)
	require.Equal(t, incarnation, current)
	count, err := f.client.Count(t.Context(), &qdrant.CountPoints{CollectionName: f.collection, Exact: qdrant.PtrOf(true)})
	require.NoError(t, err)
	require.Equal(t, uint64(1), count)
	incompatible, err := vectorindex.NewWithSearchPolicy(f.client, f.collection, 4, policy)
	require.NoError(t, err)
	require.ErrorContains(t, incompatible.Ensure(t.Context()), "dimensions")
	info, err = f.client.GetCollectionInfo(t.Context(), f.collection)
	require.NoError(t, err)
	require.Equal(t, uint64(10000), info.Config.HnswConfig.GetFullScanThreshold(), "schema mismatch must precede policy updates")
}
