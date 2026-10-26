//go:build integration && performance

package vectorindex_test

import (
	"fmt"
	"testing"

	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
)

func TestSyntheticRecallUsesUniqueNeighborsAndCountsIncompleteResponses(t *testing.T) {
	var exact []string
	for n := range 20 {
		exact = append(exact, fmt.Sprint(n))
	}
	require.Equal(t, 1.0, syntheticRecallIDs(exact, exact))
	require.Zero(t, syntheticRecallIDs(exact, nil))
	require.Equal(t, .05, syntheticRecallIDs(exact, []string{"0", "0", "0", "outside"}))
	require.Equal(t, .95, syntheticRecallIDs(exact, exact[:19]))
	require.Equal(t, .99, syntheticRecallTarget([]float64{.96, .995}))
	require.Equal(t, .95, syntheticRecallTarget([]float64{.94, .97}))
	require.Zero(t, syntheticRecallTarget([]float64{.94, .7}))
}

func TestSyntheticPolicyUsesAutomaticPlanningAndDominantScope(t *testing.T) {
	t.Setenv("TOPIC_STREAM_SYNTHETIC_DISTRIBUTION", "dominant")
	cases := syntheticCapacityCases()
	require.Len(t, cases, 4)
	count := 0
	for n := range 1000 {
		if cases[1].accept(n) {
			count++
		}
		require.Equal(t, syntheticCapacityID("workspace", syntheticWorkspace(n)), syntheticCapacityPayload(n)["workspaceId"])
	}
	require.Equal(t, 800, count)
	params := syntheticQuery("fixture", cases[1], 0, false).Params
	require.Nil(t, params.Exact)
	require.Equal(t, uint64(256), params.GetHnswEf())
	require.True(t, params.GetAcorn().GetEnable())
	require.Equal(t, .4, params.GetAcorn().GetMaxSelectivity())
	require.True(t, syntheticQuery("fixture", cases[1], 0, true).Params.GetExact())
}

func TestSyntheticResultValidationRejectsScopeIdentityAndDuplicateCorruption(t *testing.T) {
	scenario := syntheticCapacityCases()[0]
	hit := &qdrant.ScoredPoint{Id: qdrant.NewIDUUID(syntheticCapacityID("item", 0)), Payload: qdrant.NewValueMap(syntheticCapacityPayload(0))}
	require.Empty(t, syntheticFilterViolation([]*qdrant.ScoredPoint{hit}, scenario))
	require.NotEmpty(t, syntheticFilterViolation([]*qdrant.ScoredPoint{hit, hit}, scenario))
	hit.Payload["itemId"] = qdrant.NewValueString("incorrect")
	require.NotEmpty(t, syntheticFilterViolation([]*qdrant.ScoredPoint{hit}, scenario))
	hit.Payload = nil
	require.NotEmpty(t, syntheticFilterViolation([]*qdrant.ScoredPoint{hit}, scenario))
}

func TestSyntheticOptimizationReadinessRequiresObservedEmptyQueue(t *testing.T) {
	for _, body := range []string{
		`{"result":{"summary":{"queued_optimizations":1},"running":[],"queued":[{}]}}`,
		`{"result":{"summary":{"queued_optimizations":0},"running":[{}],"queued":[]}}`,
	} {
		idle, err := syntheticOptimizationIdle([]byte(body))
		require.NoError(t, err)
		require.False(t, idle)
	}
	idle, err := syntheticOptimizationIdle([]byte(`{"result":{"summary":{"queued_optimizations":0},"running":[],"queued":[]}}`))
	require.NoError(t, err)
	require.True(t, idle)
	_, err = syntheticOptimizationIdle([]byte(`{"result":{}}`))
	require.Error(t, err)
}
