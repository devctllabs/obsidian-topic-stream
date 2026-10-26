//go:build integration && performance

package postgres_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/projection"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type modelDeltaTrace struct {
	queries []pgx.TraceQueryStartData
}

// TraceQueryStart records the repository's actual SQL for capacity evidence.
func (trace *modelDeltaTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.queries = append(trace.queries, data)
	return ctx
}

// TraceQueryEnd completes the query tracer contract.
func (*modelDeltaTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

type deltaDeliveryFactory struct {
	counts map[uuid.UUID]int
	embeds int
}

// Build records model-specific writes at the external index boundary.
func (f *deltaDeliveryFactory) Build(model domain.EmbeddingModel) (projection.Embedder, projection.Writer, error) {
	return f, deltaWriter{factory: f, modelID: model.ID}, nil
}

// Embed counts provider requests; unchanged canonical text should reuse existing vectors.
func (f *deltaDeliveryFactory) Embed(context.Context, string) ([]float32, error) {
	f.embeds++
	return []float32{1, 2, 3}, nil
}

// EmbedBatch counts each synthetic document while preserving this payload-reuse fixture's semantics.
func (f *deltaDeliveryFactory) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vectors[i], _ = f.Embed(ctx, text)
	}
	return vectors, nil
}

type deltaWriter struct {
	factory *deltaDeliveryFactory
	modelID uuid.UUID
}

// Ensure represents an existing index without network latency.
func (deltaWriter) Ensure(context.Context) error { return nil }

// CollectionIncarnation represents the existing synthetic collection instance.
func (deltaWriter) CollectionIncarnation(context.Context) (uuid.UUID, error) {
	return uuid.NameSpaceOID, nil
}

// SetCollectionIncarnation represents marker initialization at the external boundary.
func (deltaWriter) SetCollectionIncarnation(context.Context, uuid.UUID) error { return nil }

// UpdateProjectionPayload represents existing points with unchanged text.
func (w deltaWriter) UpdateProjectionPayload(context.Context, domain.ItemProjection) (bool, error) {
	w.factory.counts[w.modelID]++
	return true, nil
}

// UpsertProjection counts full writes if vector reuse unexpectedly fails.
func (w deltaWriter) UpsertProjection(context.Context, domain.ItemProjection, []float32) error {
	w.factory.counts[w.modelID]++
	return nil
}

func TestRetainedModelDeltaCapacity(t *testing.T) {
	require.NotEmpty(t, os.Getenv("TOPIC_STREAM_TEST_POSTGRES_DSN"), "use the capped disposable benchmark runner")
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	var models []uuid.UUID
	for range 2 {
		model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
		require.NoError(t, err)
		model.ID, model.Name = uuid.New(), uuid.NewString()
		require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
		models = append(models, model.ID)
	}
	returning, inactive := models[0], models[1]
	_, err = conn.Exec(t.Context(), `INSERT INTO items(id,workspace_id,details,title,url,canonical_url)
SELECT md5(n::text)::uuid,$1,'{"type":"article","article":{}}','Item '||n,'https://fixture.example/'||n,'https://fixture.example/'||n FROM generate_series(1,1000000) n`, f.workspaceID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO item_projection_state(workspace_id,item_id,revision,updated_at)
SELECT workspace_id,id,id,$2 FROM items WHERE workspace_id=$1`, f.workspaceID, f.now)
	require.NoError(t, err)
	// Simulate completed baseline indexing of retained models. C has a million undelivered changes.
	_, err = conn.Exec(t.Context(), `INSERT INTO workspace_model_progress(workspace_id,embedding_model_id,seed_complete,selected_at)
SELECT $1,model,true,$3 FROM unnest($2::uuid[]) model ON CONFLICT(workspace_id,embedding_model_id) DO UPDATE SET seed_complete=true`, f.workspaceID, models, f.now)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO retrieval_projection_pending(workspace_id,item_id,embedding_model_id,revision,updated_at)
SELECT workspace_id,item_id,$2,revision,$3 FROM item_projection_state WHERE workspace_id=$1`, f.workspaceID, inactive, f.now)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO retrieval_projection_pending(workspace_id,item_id,embedding_model_id,revision,updated_at)
SELECT workspace_id,item_id,$2,revision,$3 FROM item_projection_state WHERE workspace_id=$1 ORDER BY item_id LIMIT 500`, f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO jobs(id,workspace_id,kind,queue,idempotency_key)
SELECT item_id,workspace_id,'index-item','retrieval','index-item:'||item_id::text||':'||revision::text
FROM item_projection_state WHERE workspace_id=$1 ORDER BY item_id LIMIT 400`, f.workspaceID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), "ANALYZE")
	require.NoError(t, err)
	trace := &modelDeltaTrace{}
	traced, err := postgres.OpenWithTracerForTest(t.Context(), f.dsn, trace)
	require.NoError(t, err)
	t.Cleanup(traced.Close)
	metrics := map[string]any{"Items": 1000000, "InactivePending": 1000000, "AlreadyQueuedActivePrefix": 400, "Provider": "synthetic existing points; network latency excluded"}
	var cursor *domain.ProjectionRepairCursor
	var durations []float64
	eligible, passes := 0, 0
	for {
		started := time.Now()
		page, err := traced.ListPendingGlobalItemProjections(t.Context(), domain.ProjectionRepairQuery{Limit: 100, After: cursor})
		require.NoError(t, err)
		durations = append(durations, milliseconds(time.Since(started)))
		passes++
		eligible += len(page.Commits)
		cursor = page.NextCursor
		if cursor == nil {
			break
		}
		require.LessOrEqual(t, passes, 6, "inactive million-key backlog must never enter repair inspection")
	}
	require.Equal(t, 100, eligible)
	require.Equal(t, 7, passes)
	metrics["RepairMs"] = testutil.Percentiles(durations)
	metrics["RepairPasses"] = passes
	metrics["RepairEligibleItems"] = eligible
	metrics["RepairFirstBatchPlan"] = explainDeltaQuery(t, conn, trace.queries[0])
	_, err = conn.Exec(t.Context(), `DELETE FROM retrieval_projection_pending WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	job := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: returning, JobID: job, StartedAt: f.now}
	trace.queries = nil
	lsn := deltaLSN(t, conn)
	started := time.Now()
	_, err = traced.Begin(t.Context(), start)
	require.NoError(t, err)
	metrics["UnchangedReturnBeginMs"] = milliseconds(time.Since(started))
	metrics["UnchangedReturnWALBytes"] = deltaWAL(t, conn, lsn)
	for _, query := range trace.queries {
		require.False(t, strings.Contains(query.SQL, "WITH batch AS MATERIALIZED"), "a completed retained model must not scan canonical Items to seed again")
	}
	commits, err := f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: f.workspaceID, ModelID: returning, Limit: 100})
	require.NoError(t, err)
	require.Empty(t, commits)
	require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{WorkspaceID: f.workspaceID, ModelID: returning, JobID: job, FailedAt: f.now}))
	topicID := f.topic(t)
	_, err = conn.Exec(t.Context(), `INSERT INTO topic_corrections(topic_id,item_id,membership,updated_at)
SELECT $1,item_id,'include',$3 FROM item_projection_state WHERE workspace_id=$2 ORDER BY item_id LIMIT 10000`, topicID, f.workspaceID, f.now)
	require.NoError(t, err)
	topic, _, err := f.store.GetTopic(t.Context(), f.workspaceID, topicID)
	require.NoError(t, err)
	lsn = deltaLSN(t, conn)
	started = time.Now()
	cleared, err := f.store.UpdateTopic(t.Context(), domain.TopicUpdate{Topic: topic, ClearCorrections: true})
	require.NoError(t, err)
	require.Len(t, cleared.ProjectionCommits, 10000)
	metrics["Capture10000TransactionMs"] = milliseconds(time.Since(started))
	metrics["Capture10000WALBytes"] = deltaWAL(t, conn, lsn)
	for _, sentiment := range []string{"like", "dislike", "like"} {
		_, _, err = f.store.SetFeedback(t.Context(), f.workspaceID, cleared.ProjectionCommits[0].ItemID, domain.Feedback{Sentiment: sentiment, CreatedAt: f.now, UpdatedAt: f.now})
		require.NoError(t, err)
	}
	var pending int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, returning).Scan(&pending))
	require.Equal(t, 10000, pending, "repeated edits must not add delivery work")
	factory := &deltaDeliveryFactory{counts: map[uuid.UUID]int{}}
	service, err := projection.New(f.store, factory)
	require.NoError(t, err)
	work, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, cleared.ProjectionCommits[0].ItemID)
	require.NoError(t, err)
	require.NoError(t, service.DeliverItemProjection(t.Context(), domain.ItemProjectionCommit{WorkspaceID: f.workspaceID, ItemID: work.Projection.Item.ID, ProjectionRevision: work.Projection.Revision}))
	require.Equal(t, map[uuid.UUID]int{domain.DefaultEmbeddingModelID: 1}, factory.counts, "inactive models receive no index or embedding calls")
	start.JobID = enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	trace.queries = nil
	commits, err = traced.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: f.workspaceID, ModelID: returning, Limit: 100})
	require.NoError(t, err)
	require.Len(t, commits, 100)
	metrics["DeltaBatchPlan"] = explainDeltaQuery(t, conn, trace.queries[0])
	seen := map[uuid.UUID]bool{}
	started = time.Now()
	for len(commits) > 0 {
		for _, commit := range commits {
			require.False(t, seen[commit.ItemID])
			seen[commit.ItemID] = true
			require.NoError(t, service.DeliverItemProjection(t.Context(), commit))
		}
		commits, err = f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: f.workspaceID, ModelID: returning, Limit: 100})
		require.NoError(t, err)
	}
	metrics["DeltaDeliverySeconds"] = time.Since(started).Seconds()
	require.Len(t, seen, 10000)
	require.Equal(t, 10000, factory.counts[returning])
	require.Equal(t, 10000, factory.counts[domain.DefaultEmbeddingModelID])
	require.Zero(t, factory.counts[inactive])
	require.Zero(t, factory.embeds, "payload-only edits reuse vectors")
	metrics["ReturningModelPayloadWrites"] = factory.counts[returning]
	metrics["InactiveModelPayloadWrites"] = factory.counts[inactive]
	metrics["EmbeddingCalls"] = factory.embeds
	require.NoError(t, f.store.Activate(t.Context(), domain.ModelTransitionCutover{WorkspaceID: f.workspaceID, ModelID: returning, JobID: start.JobID, ActivatedAt: f.now}))
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*),pg_total_relation_size('retrieval_projection_pending') FROM retrieval_projection_pending WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, inactive).Scan(&pending, new(int64)))
	require.Equal(t, 1000000, pending, "inactive backlog remains intact")
	var storage int64
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT pg_total_relation_size('retrieval_projection_pending')`).Scan(&storage))
	metrics["PendingTableAndIndexesBytes"] = storage
	encoded, err := json.MarshalIndent(metrics, "", "  ")
	require.NoError(t, err)
	if path := os.Getenv("TOPIC_STREAM_PERF_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(path, append(encoded, '\n'), 0o600))
	}
	t.Log(string(encoded))
}

func explainDeltaQuery(t *testing.T, conn *pgx.Conn, query pgx.TraceQueryStartData) json.RawMessage {
	t.Helper()
	var plan json.RawMessage
	require.NoError(t, conn.QueryRow(t.Context(), "EXPLAIN(ANALYZE,BUFFERS,WAL,FORMAT JSON) "+query.SQL, query.Args...).Scan(&plan))
	return plan
}

func deltaLSN(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var lsn string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT pg_current_wal_insert_lsn()::text`).Scan(&lsn))
	return lsn
}

func deltaWAL(t *testing.T, conn *pgx.Conn, lsn string) int64 {
	t.Helper()
	var wal int64
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT pg_wal_lsn_diff(pg_current_wal_insert_lsn(),$1::pg_lsn)::bigint`, lsn).Scan(&wal))
	return wal
}
