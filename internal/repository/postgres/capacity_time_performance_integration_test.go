//go:build integration && performance

package postgres_test

import "time"

func milliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
