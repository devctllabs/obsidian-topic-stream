//go:build integration

package deps_test

import (
	"context"
	"testing"
	"time"

	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewAPIBindsProjectionGraphWithoutProvisioningDerivedCollection(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := postgres.Open(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	workspaceID := uuid.New()
	modelID := seedRetrievalModel(t, store, workspaceID)

	client, err := vectorindex.NewClient(testutil.QdrantURL(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	collection := vectorindex.CollectionName(modelID)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exists, err := client.CollectionExists(ctx, collection)
		require.NoError(t, err)
		if exists {
			require.NoError(t, client.DeleteCollection(ctx, collection))
		}
	})
	runtime, err := deps.NewAPI(t.Context(), generated.Config{
		Bootstrap: generated.BootstrapConfig{BootstrapEmbeddingEndpoint: "http://fixture.example/v1", BootstrapEmbeddingModel: "synthetic", BootstrapEmbeddingDimensions: 3},
		DBPrimary: generated.DBPrimaryConfig{Kind: "postgres", PostgresDSN: dsn},
		HTTP:      generated.HTTPConfig{Address: "127.0.0.1:0", Enabled: true},
		Runtime: generated.RuntimeConfig{EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1,
			WorkspaceId: workspaceID.String(), QdrantUrl: testutil.QdrantURL(t), ShutdownTimeout: time.Second,
		},
	}, "test")
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, runtime.Shutdown(ctx))
	})
	exists, err := client.CollectionExists(t.Context(), collection)
	require.NoError(t, err)
	require.False(t, exists, "construction binds the real graph; operations and Jobs provision derived state")
	var taskNames []string
	for _, task := range runtime.Tasks() {
		taskNames = append(taskNames, task.Name)
	}
	require.ElementsMatch(t, []string{"postgres-advisories", "http-api"}, taskNames)
}

func TestNewAPIConstructsWorkerOnlyGraphWithoutWorkspace(t *testing.T) {
	runtime, err := deps.NewAPI(t.Context(), generated.Config{
		Bootstrap: generated.BootstrapConfig{BootstrapEmbeddingEndpoint: "http://fixture.example/v1", BootstrapEmbeddingModel: "synthetic", BootstrapEmbeddingDimensions: 3},
		DBPrimary: generated.DBPrimaryConfig{Kind: "postgres", PostgresDSN: testutil.MigratedPostgresSchema(t)},
		HTTP:      generated.HTTPConfig{Enabled: false},
		Runtime: generated.RuntimeConfig{EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1,
			WorkersEnabled: true, SchedulersEnabled: false, QdrantUrl: testutil.QdrantURL(t),
			WorkerFetchConcurrency: 4, WorkerFetchPerHostConcurrency: 1,
			WorkerGenerationConcurrency: 1, WorkerRetrievalConcurrency: 2, WorkerFeedPreparationConcurrency: 2, WorkerMaintenanceConcurrency: 1,
		},
	}, "test")
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, runtime.Shutdown(ctx))
	})
	var taskNames []string
	for _, task := range runtime.Tasks() {
		taskNames = append(taskNames, task.Name)
	}
	require.ElementsMatch(t, []string{"postgres-advisories", "job-worker", "feed-preparation-reconciler", "item-projection-reconciler", "model-transition-reconciler"}, taskNames)
}

func seedRetrievalModel(t *testing.T, store *postgres.Store, workspaceID uuid.UUID) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	require.NoError(t, store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), now))
	model, found, err := store.EmbeddingModels().Get(t.Context(), domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	require.True(t, found)
	model.InitializationStatus = domain.EmbeddingModelReady
	model.Version++
	model.UpdatedAt = now
	updated, err := store.EmbeddingModels().Update(t.Context(), model, model.Version-1)
	require.NoError(t, err)
	require.True(t, updated)

	model.ID = uuid.New()
	model.Name = "Integration model"
	model.Dimensions = 3
	require.NoError(t, store.EmbeddingModels().Create(t.Context(), model))
	defaults := settings.DefaultWorkspaceSettings(now)
	defaults.EmbeddingModelID = model.ID
	_, err = store.GetOrCreateWorkspaceSettings(t.Context(), workspaceID, defaults)
	require.NoError(t, err)
	require.NoError(t, store.EmbeddingModels().EnsureWorkspaceState(t.Context(), workspaceID, model.ID, now))
	return model.ID
}
