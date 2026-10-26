//go:build integration

package deps_test

import (
	"context"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewReindexBuildsWithoutRunningTheOperation(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	t.Chdir(t.TempDir())
	t.Setenv("TOPIC_STREAM_DB_PRIMARY_KIND", "postgres")
	t.Setenv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN", dsn)
	t.Setenv("TOPIC_STREAM_WORKSPACE_ID", uuid.NewString())
	runtime, err := deps.NewReindex(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(context.Background())) })
	_, err = runtime.Rebuild(t.Context())
	require.ErrorContains(t, err, "active retrieval generation does not exist")
}
