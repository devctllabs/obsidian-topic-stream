//go:build integration && performance

package vectorindex_test

import (
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/stretchr/testify/require"
)

func TestQdrantEnsureExistingCollectionCapacity(t *testing.T) {
	fixture := newIndexFixture(t)
	backend := &ensureCountingBackend{Client: fixture.client}
	index, err := vectorindex.New(backend, fixture.collection, 3)
	require.NoError(t, err)
	for round := range 5 {
		backend.writes = 0
		started := time.Now()
		for range 100 {
			require.NoError(t, index.Ensure(t.Context()))
		}
		t.Logf("round=%d checks=100 elapsed_ms=%.3f schema_writes=%d", round+1, float64(time.Since(started))/float64(time.Millisecond), backend.writes)
	}
}
