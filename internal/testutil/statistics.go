//go:build integration && (performance || live)

package testutil

import (
	"math"
	"slices"
)

// Percentiles reports nearest-rank nonnegative sample quantiles without changing input order.
func Percentiles(values []float64) map[string]float64 {
	if len(values) == 0 {
		panic("percentiles require measured samples")
	}
	for _, value := range values {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			panic("percentiles require finite nonnegative measurements")
		}
	}
	ordered := slices.Clone(values)
	slices.Sort(ordered)
	rank := func(fraction float64) float64 { return ordered[int(math.Ceil(fraction*float64(len(ordered))))-1] }
	return map[string]float64{"p50": rank(.5), "p95": rank(.95), "p99": rank(.99), "max": ordered[len(ordered)-1]}
}
