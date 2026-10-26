//go:build integration

package deps

import (
	"context"
	"testing"
	"time"

	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestAPIConstructionDoesNotEnqueuePendingBacklog(t *testing.T) {
	cfg, conn := startupRepairFixture(t, 201)
	runtime, err := NewAPI(t.Context(), cfg, "startup-repair")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(context.Background())) })
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE kind='index-item'`).Scan(&count))
	require.Zero(t, count, "HTTP graph construction must not enqueue the backlog")
}

func TestWorkerRepairRecoversEveryRevisionAcrossRestartWithoutDuplicateJobs(t *testing.T) {
	cfg, conn := startupRepairFixture(t, 201)
	cfg.HTTP.Enabled = false
	cfg.Runtime = generated.RuntimeConfig{EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1, WorkersEnabled: true, QdrantUrl: testutil.QdrantURL(t),
		WorkerFetchConcurrency: 1, WorkerFetchPerHostConcurrency: 1, WorkerGenerationConcurrency: 1,
		WorkerRetrievalConcurrency: 1, WorkerFeedPreparationConcurrency: 1, WorkerMaintenanceConcurrency: 1}
	runtime, err := NewAPI(t.Context(), cfg, "repair-worker")
	require.NoError(t, err)
	require.NoError(t, runtime.projectionReconciler.Reconcile(t.Context()))
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE kind='index-item'`).Scan(&count))
	require.Equal(t, 100, count)
	require.NoError(t, runtime.Shutdown(t.Context()))
	restarted, err := NewAPI(t.Context(), cfg, "repair-worker-restarted")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, restarted.Shutdown(context.Background())) })
	for range 4 {
		require.NoError(t, restarted.projectionReconciler.Reconcile(t.Context()))
	}
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE kind='index-item'`).Scan(&count))
	require.Equal(t, 201, count)
	var missing int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM item_projection_state s WHERE NOT EXISTS(
SELECT 1 FROM jobs j WHERE j.workspace_id=s.workspace_id AND j.idempotency_key='index-item:'||s.item_id::text||':'||s.revision::text)`).Scan(&missing))
	require.Zero(t, missing)
}

func startupRepairFixture(t *testing.T, count int) (generated.Config, *pgx.Conn) {
	t.Helper()
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := postgres.Open(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	workspaceID := uuid.New()
	now := time.Now().UTC()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	require.NoError(t, store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), now))
	model := domain.EmbeddingModel{ID: uuid.New(), EmbeddingModelDescriptor: testutil.DefaultEmbeddingDescriptor(), Enabled: true,
		InitializationStatus: domain.EmbeddingModelReady, Version: 1, CreatedAt: now, UpdatedAt: now}
	model.Name = "Startup repair fixture " + model.ID.String()
	require.NoError(t, store.EmbeddingModels().Create(t.Context(), model))
	defaults := settings.DefaultWorkspaceSettings(now)
	defaults.EmbeddingModelID = model.ID
	_, err = store.GetOrCreateWorkspaceSettings(t.Context(), workspaceID, defaults)
	require.NoError(t, err)
	require.NoError(t, store.EmbeddingModels().EnsureWorkspaceState(t.Context(), workspaceID, model.ID, now))
	client, err := vectorindex.NewClient(testutil.QdrantURL(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		collection := vectorindex.CollectionName(model.ID)
		exists, lookupErr := client.CollectionExists(ctx, collection)
		require.NoError(t, lookupErr)
		if exists {
			require.NoError(t, client.DeleteCollection(ctx, collection))
		}
	})
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(context.Background())) })
	_, err = conn.Exec(t.Context(), `INSERT INTO items(id,workspace_id,title,url,canonical_url,details)
SELECT md5('repair:'||n)::uuid,$1,'Item '||n,'https://repair/'||n,'https://repair/'||n,'{"type":"article","article":{}}'
FROM generate_series(1,$2::integer) n`, workspaceID, count)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO item_projection_state(workspace_id,item_id,revision,updated_at)
SELECT workspace_id,id,id,now() FROM items WHERE workspace_id=$1`, workspaceID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO retrieval_projection_pending(workspace_id,item_id,embedding_model_id,revision,updated_at)
SELECT workspace_id,item_id,$1,revision,updated_at FROM item_projection_state`, model.ID)
	require.NoError(t, err)
	return generated.Config{
		Bootstrap: generated.BootstrapConfig{BootstrapEmbeddingEndpoint: "http://fixture.example/v1", BootstrapEmbeddingModel: "synthetic", BootstrapEmbeddingDimensions: 3},
		DBPrimary: generated.DBPrimaryConfig{Kind: "postgres", PostgresDSN: dsn},
		HTTP:      generated.HTTPConfig{Enabled: true, Address: "127.0.0.1:0"},
		Runtime:   generated.RuntimeConfig{EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1, WorkspaceId: workspaceID.String(), QdrantUrl: testutil.QdrantURL(t), ShutdownTimeout: time.Second},
	}, conn
}
