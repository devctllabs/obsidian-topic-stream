package vectorindex

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
)

func measuredSearchPolicy() SearchPolicy {
	return SearchPolicy{FullScanThresholdKB: 100000, HNSWEf: 256, Acorn: true, MaxSelectivity: .4}
}

func TestConfiguredPolicyRejectsUnsupportedServerAndRetriesHealthFailures(t *testing.T) {
	backend := &backendSpy{healthVersion: "1.15.9"}
	index, err := NewWithSearchPolicy(backend, "policy", 3, measuredSearchPolicy())
	require.NoError(t, err)
	_, err = index.QueryDense(t.Context(), []float32{1, 0, 0}, Filter{WorkspaceID: uuid.New()}, 20)
	require.ErrorContains(t, err, "ACORN requires Qdrant")
	require.Empty(t, backend.queryRequests)
	backend.healthVersion = "1.19.1"
	backend.healthErr = errors.New("offline")
	_, err = index.QueryDense(t.Context(), []float32{1, 0, 0}, Filter{WorkspaceID: uuid.New()}, 20)
	require.ErrorContains(t, err, "offline")
	backend.healthErr = nil
	for range 2 {
		_, err = index.QueryDense(t.Context(), []float32{1, 0, 0}, Filter{WorkspaceID: uuid.New()}, 20)
		require.NoError(t, err)
	}
	require.Equal(t, 3, backend.healthCalls, "cache successful compatibility, not transient failures")
}

func TestConfiguredPolicyDetectsConflictingNamedVectorOverride(t *testing.T) {
	info := validCollectionInfo(3)
	info.Config.HnswConfig = &qdrant.HnswConfigDiff{FullScanThreshold: qdrant.PtrOf(uint64(100000))}
	info.Config.Params.VectorsConfig.GetParamsMap().Map[DenseVectorName].HnswConfig = &qdrant.HnswConfigDiff{FullScanThreshold: qdrant.PtrOf(uint64(10000))}
	backend := &backendSpy{exists: true, collectionInfo: info}
	index, err := NewWithSearchPolicy(backend, "policy", 3, measuredSearchPolicy())
	require.NoError(t, err)
	require.ErrorContains(t, index.Ensure(t.Context()), "dense vector overrides")
	require.Empty(t, backend.updateRequests, "must not report success while an override keeps another threshold")
}

func TestConfiguredPolicyDoesNotChangeOtherDenseVectorSettings(t *testing.T) {
	info := validCollectionInfo(3)
	info.Config.HnswConfig = &qdrant.HnswConfigDiff{FullScanThreshold: qdrant.PtrOf(uint64(10000))}
	info.Config.Params.VectorsConfig.GetParamsMap().Map["custom"] = &qdrant.VectorParams{Size: 3, Distance: qdrant.Distance_Cosine}
	backend := &backendSpy{exists: true, collectionInfo: info}
	index, err := NewWithSearchPolicy(backend, "policy", 3, measuredSearchPolicy())
	require.NoError(t, err)
	require.ErrorContains(t, index.Ensure(t.Context()), "other dense vectors")
	require.Empty(t, backend.updateRequests)
}

func TestConfiguredPolicyCreatesAndReconcilesOnlyOwnedSettings(t *testing.T) {
	created := &backendSpy{}
	index, err := NewWithSearchPolicy(created, "policy", 3, measuredSearchPolicy())
	require.NoError(t, err)
	require.NoError(t, index.Ensure(t.Context()))
	require.NotNil(t, created.createRequest.HnswConfig)
	require.Equal(t, uint64(100000), created.createRequest.HnswConfig.GetFullScanThreshold())
	require.Nil(t, created.createRequest.HnswConfig.M, "preserve structural defaults")
	info := validCollectionInfo(3)
	info.Config.HnswConfig = &qdrant.HnswConfigDiff{M: qdrant.PtrOf(uint64(32)), FullScanThreshold: qdrant.PtrOf(uint64(10000))}
	backend := &backendSpy{exists: true, collectionInfo: info}
	index, err = NewWithSearchPolicy(backend, "policy", 3, measuredSearchPolicy())
	require.NoError(t, err)
	require.NoError(t, index.Ensure(t.Context()))
	require.Len(t, backend.updateRequests, 1)
	update := backend.updateRequests[0]
	require.Equal(t, uint64(100000), update.HnswConfig.GetFullScanThreshold())
	require.Nil(t, update.HnswConfig.M)
	require.Nil(t, update.OptimizersConfig)
	require.Empty(t, update.Metadata)
	require.NoError(t, index.Ensure(t.Context()))
	require.Len(t, backend.updateRequests, 1, "same policy must not repeatedly update collections")
	require.Equal(t, uint64(32), backend.collectionInfo.Config.HnswConfig.GetM())
}

func TestConfiguredPolicyRejectsInvalidBudgetsWithoutIO(t *testing.T) {
	for _, policy := range []SearchPolicy{
		{FullScanThresholdKB: math.MaxUint64}, {HNSWEf: 65537}, {Acorn: true}, {MaxSelectivity: 1.1}, {MaxSelectivity: math.NaN()},
	} {
		backend := &backendSpy{}
		_, err := NewWithSearchPolicy(backend, "policy", 3, policy)
		require.Error(t, err)
		require.Nil(t, backend.createRequest)
	}
	index, err := NewWithSearchPolicy(&backendSpy{}, "legacy", 3, SearchPolicy{})
	require.NoError(t, err)
	require.Nil(t, index.searchPolicy.params())
}

func TestConfiguredDenseAndPersonalQueriesUsePlannerPolicy(t *testing.T) {
	backend := &backendSpy{batchResult: []*qdrant.BatchResult{{}, {}}}
	index, err := NewWithSearchPolicy(backend, "policy", 3, measuredSearchPolicy())
	require.NoError(t, err)
	workspace := uuid.New()
	filter := Filter{WorkspaceID: workspace}
	_, err = index.QueryDense(context.Background(), []float32{1, 0, 0}, filter, 20)
	require.NoError(t, err)
	requirePlannerParams(t, backend.queryRequests[0].Params)
	require.Equal(t, workspace.String(), backend.queryRequests[0].Filter.Must[0].GetField().GetMatch().GetKeyword())
	_, err = index.QuerySparse(context.Background(), "text", filter, 20)
	require.NoError(t, err)
	require.Nil(t, backend.queryRequests[1].Params, "dense policy must not alter sparse queries")
	_, err = index.RetrievePersonal(context.Background(), ranking.PersonalIndexRequest{Filter: filter, CandidateIDs: []uuid.UUID{uuid.New()}, NeighborLimit: 20})
	require.NoError(t, err)
	for _, request := range backend.batchRequest.QueryPoints {
		requirePlannerParams(t, request.Params)
	}
	require.Nil(t, backend.createRequest, "queries must not create or reconcile collections")
}

func requirePlannerParams(t *testing.T, p *qdrant.SearchParams) {
	t.Helper()
	require.NotNil(t, p)
	require.Nil(t, p.Exact, "leave direct scoring versus graph traversal to Qdrant")
	require.Equal(t, uint64(256), p.GetHnswEf())
	require.True(t, p.GetAcorn().GetEnable())
	require.Equal(t, .4, p.GetAcorn().GetMaxSelectivity())
}
