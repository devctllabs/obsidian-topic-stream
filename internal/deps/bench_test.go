package deps_test

import (
	"testing"

	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	"github.com/stretchr/testify/require"
)

func TestNewBenchValidatesWorkspaceBeforeAcquiringResources(t *testing.T) {
	t.Parallel()

	_, err := deps.NewBench(t.Context(), generated.Config{
		DBPrimary: generated.DBPrimaryConfig{Kind: "postgres", PostgresDSN: "postgres://unreachable.invalid/topic_stream"},
		Runtime:   generated.RuntimeConfig{EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1, WorkspaceId: "not-a-uuid", QdrantUrl: "http://127.0.0.1:6334"},
	})

	require.ErrorContains(t, err, "workspace ID")
	require.NotContains(t, err.Error(), "connect to Postgres")
}
