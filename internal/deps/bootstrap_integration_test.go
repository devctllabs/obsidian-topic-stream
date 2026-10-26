//go:build integration

package deps_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewServeBootstrapsConfiguredModelWithoutOverwritingRegistry(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1,0,0]}]}`))
	}))
	t.Cleanup(provider.Close)
	dsn := testutil.MigratedPostgresSchema(t)
	t.Chdir(t.TempDir())
	t.Setenv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN", dsn)
	t.Setenv("TOPIC_STREAM_WORKSPACE_ID", uuid.NewString())
	t.Setenv("TOPIC_STREAM_QDRANT_URL", testutil.QdrantURL(t))
	t.Setenv("TOPIC_STREAM_SCHEDULERS_ENABLED", "false")
	t.Setenv("TOPIC_STREAM_WORKERS_ENABLED", "false")
	t.Setenv("TOPIC_STREAM_BOOTSTRAP_EMBEDDING_ENDPOINT", provider.URL+"/v1")
	t.Setenv("TOPIC_STREAM_BOOTSTRAP_EMBEDDING_MODEL", "test-embedding")
	t.Setenv("TOPIC_STREAM_BOOTSTRAP_EMBEDDING_DIMENSIONS", "3")

	client, err := vectorindex.NewClient(testutil.QdrantURL(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	collection := vectorindex.CollectionName(domain.DefaultEmbeddingModelID)
	exists, err := client.CollectionExists(t.Context(), collection)
	require.NoError(t, err)
	require.False(t, exists, "bootstrap test requires an unused default-model collection; use an isolated test Qdrant")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		exists, err := client.CollectionExists(ctx, collection)
		require.NoError(t, err)
		if exists {
			require.NoError(t, client.DeleteCollection(ctx, collection))
		}
	})
	store, err := postgres.Open(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)

	runtime, _, err := deps.NewServe(t.Context(), "test")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(context.Background())) })
	model, found, err := store.EmbeddingModels().Get(t.Context(), domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, provider.URL+"/v1", model.Endpoint)
	require.Equal(t, "test-embedding", model.ProviderModel)
	require.Equal(t, 3, model.Dimensions)
	require.Equal(t, "Default embedding model", model.Name)
	require.Equal(t, domain.EmbeddingModelReady, model.InitializationStatus)
	require.NoError(t, runtime.Shutdown(t.Context()))

	t.Setenv("TOPIC_STREAM_BOOTSTRAP_EMBEDDING_ENDPOINT", "http://changed.invalid/v1")
	t.Setenv("TOPIC_STREAM_BOOTSTRAP_EMBEDDING_MODEL", "another-model")
	t.Setenv("TOPIC_STREAM_BOOTSTRAP_EMBEDDING_DIMENSIONS", "1024")
	restarted, _, err := deps.NewServe(t.Context(), "test")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, restarted.Shutdown(context.Background())) })
	unchanged, found, err := store.EmbeddingModels().Get(t.Context(), model.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, model, unchanged)
}
