//go:build integration && (performance || live)

package testutil_test

import (
	"math"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestPercentilesUseNearestRankAndPreserveSampleOrder(t *testing.T) {
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(100 - i)
	}
	actual := testutil.Percentiles(values)
	require.Equal(t, map[string]float64{"p50": 50, "p95": 95, "p99": 99, "max": 100}, actual)
	require.Equal(t, 100.0, values[0])
	require.Equal(t, 1.0, values[99])
	require.Equal(t, map[string]float64{"p50": 7, "p95": 7, "p99": 7, "max": 7}, testutil.Percentiles([]float64{7}))
	require.Equal(t, 19.0, testutil.Percentiles([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20})["p95"])
}

func TestPercentilesRejectMissingOrInvalidMeasurements(t *testing.T) {
	for _, samples := range [][]float64{nil, {-1}, {math.NaN()}, {math.Inf(1)}} {
		require.Panics(t, func() { testutil.Percentiles(samples) })
	}
}
