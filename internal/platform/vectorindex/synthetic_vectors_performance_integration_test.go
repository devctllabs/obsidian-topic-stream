//go:build integration && performance

package vectorindex_test

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSyntheticCapacityVectorsAreReproducibleAndQueriesIndependent(t *testing.T) {
	for n := range 32 {
		document := syntheticCapacityVector(uint64(n), false)
		require.Len(t, document, 1024)
		require.Equal(t, document, syntheticCapacityVector(uint64(n), false))
		require.NotEqual(t, document, syntheticCapacityVector(uint64(n), true))
		var norm float64
		for _, value := range document {
			require.False(t, math.IsInf(float64(value), 0) || math.IsNaN(float64(value)))
			norm += float64(value) * float64(value)
		}
		require.InDelta(t, 1, norm, 1e-6)
	}
	require.NotEqual(t, syntheticCapacityVector(0, false), syntheticCapacityVector(1, false))
}

// Separate noise streams keep queries independent of document vectors in the same cluster.
func syntheticCapacityVector(n uint64, query bool) []float32 {
	center := rand.New(rand.NewPCG(20261025, n%64))
	domain := uint64(0)
	if query {
		domain = 1 << 63
	}
	noise := rand.New(rand.NewPCG(8675309, n^domain))
	vector := make([]float32, 1024)
	var norm float64
	for index := range vector {
		value := float32(center.NormFloat64() + 0.35*noise.NormFloat64())
		vector[index] = value
		norm += float64(value) * float64(value)
	}
	scale := float32(1 / math.Sqrt(norm))
	for index := range vector {
		vector[index] *= scale
	}
	return vector
}
