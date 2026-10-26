//go:build integration && performance

package deps

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type switchingCapacity struct {
	f                   stableAPIFixture
	conn                *pgx.Conn
	inactive, alternate uuid.UUID
}

func TestModelSwitchingCapacity(t *testing.T) {
	require.NotEmpty(t, os.Getenv("TOPIC_STREAM_TEST_POSTGRES_DSN"), "use the capped disposable runner")
	f, conn, _ := newPerformanceFixture(t)
	fixture := prepareSwitchingCapacity(t, f, conn)
	results := map[string]any{"Items": 1000000, "InactivePending": 1000000,
		"ExternalIndex": "synthetic existing baseline; full seed only, then real PostgreSQL target ACKs; provider/network throughput excluded"}
	for _, topics := range []int{100, 500} {
		_, err := conn.Exec(t.Context(), `INSERT INTO topics(id,workspace_id,name,include_text)
SELECT md5('topic:'||n)::uuid,$1,'Topic '||n,'' FROM generate_series(1,$2::integer) n ON CONFLICT DO NOTHING`, f.workspaceID, topics)
		require.NoError(t, err)
		_, err = conn.Exec(t.Context(), `INSERT INTO topic_candidate_state(workspace_id,topic_id)
SELECT workspace_id,id FROM topics WHERE workspace_id=$1 ON CONFLICT DO NOTHING`, f.workspaceID)
		require.NoError(t, err)
		active, _, err := f.store.GetActive(t.Context(), f.workspaceID)
		require.NoError(t, err)
		seed := fixture.start(t, active.ModelID, true)
		results[fmt.Sprintf("Topics%dFullSeed", topics)] = measureSwitchingWork(t, f, func(ctx context.Context) (map[string]any, error) {
			return fixture.seed(ctx, seed)
		})
		// Only the seed was timed above. Simulate completed external delivery to set up delta catch-up.
		_, err = conn.Exec(t.Context(), `DELETE FROM retrieval_projection_pending WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, active.ModelID)
		require.NoError(t, err)
		require.NoError(t, fixture.activate(t.Context(), seed))
		target := fixture.alternate
		if active.ModelID == target {
			target = domain.DefaultEmbeddingModelID
		}
		fixture.selectModel(t, target)
		delta := fixture.start(t, target, false)
		results[fmt.Sprintf("Topics%dDelta", topics)] = measureSwitchingWork(t, f, func(ctx context.Context) (map[string]any, error) {
			return fixture.delta(ctx, delta)
		})
		var count int
		require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, fixture.inactive).Scan(&count))
		require.Equal(t, 1000000, count)
	}
	var bytes int64
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT pg_total_relation_size('retrieval_projection_pending')`).Scan(&bytes))
	results["PendingTableAndIndexesBytes"] = bytes
	encoded, err := json.MarshalIndent(results, "", "  ")
	require.NoError(t, err)
	if path := os.Getenv("TOPIC_STREAM_PERF_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(path, append(encoded, '\n'), 0o600))
	}
	t.Logf("completed 1M-Item switching/HTTP matrix; pending storage=%d bytes", bytes)
}

func prepareSwitchingCapacity(t *testing.T, f stableAPIFixture, conn *pgx.Conn) switchingCapacity {
	t.Helper()
	fixture := switchingCapacity{f: f, conn: conn, inactive: uuid.New(), alternate: uuid.New()}
	model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	model.InitializationStatus, model.Version = domain.EmbeddingModelReady, model.Version+1
	_, err = f.store.EmbeddingModels().Update(t.Context(), model, model.Version-1)
	require.NoError(t, err)
	for _, id := range []uuid.UUID{fixture.inactive, fixture.alternate} {
		model.ID, model.Name = id, id.String()
		require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
	}
	_, err = conn.Exec(t.Context(), `INSERT INTO workspace_model_progress(workspace_id,embedding_model_id,seed_complete,selected_at)
SELECT $1,id,true,clock_timestamp() FROM unnest($2::uuid[]) id`, f.workspaceID, []uuid.UUID{fixture.inactive, fixture.alternate})
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE retrieval_projection_pending SET embedding_model_id=$2 WHERE workspace_id=$1`, f.workspaceID, fixture.inactive)
	require.NoError(t, err)
	// A/B vector delivery is synthetic here; actual Qdrant is used only for collection identity.
	client, err := vectorindex.NewClient(testutil.QdrantURL(t))
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	index, err := vectorindex.New(client, vectorindex.CollectionName(fixture.alternate), model.Dimensions)
	require.NoError(t, err)
	require.NoError(t, index.Ensure(t.Context()))
	marker, err := index.CollectionIncarnation(t.Context())
	require.NoError(t, err)
	require.NoError(t, f.store.ObserveModelCollection(t.Context(), domain.ModelCollectionObservation{WorkspaceID: f.workspaceID, ModelID: fixture.alternate, Incarnation: marker}))
	_, err = conn.Exec(t.Context(), "ANALYZE")
	require.NoError(t, err)
	return fixture
}

func (f switchingCapacity) selectModel(t *testing.T, modelID uuid.UUID) {
	t.Helper()
	settings, _, err := f.f.store.GetWorkspaceSettings(t.Context(), f.f.workspaceID)
	require.NoError(t, err)
	version := settings.Version
	settings.Version++
	settings.EmbeddingModelID, settings.UpdatedAt = modelID, time.Now()
	_, changed, err := f.f.store.UpdateWorkspaceSettings(t.Context(), f.f.workspaceID, version, settings)
	require.NoError(t, err)
	require.True(t, changed)
}

func (f switchingCapacity) start(t *testing.T, modelID uuid.UUID, force bool) domain.ModelTransitionStart {
	t.Helper()
	now := time.Now()
	job, _, err := f.f.store.Jobs().Enqueue(t.Context(), domain.Job{ID: uuid.New(), WorkspaceID: f.f.workspaceID, Kind: "reindex-workspace", Queue: domain.JobQueueMaintenance,
		Status: domain.JobStatusQueued, ArgumentsVersion: 3, Arguments: []byte(`{}`), MaxAttempts: 3, AvailableAt: now, CreatedAt: now, UpdatedAt: now})
	require.NoError(t, err)
	claim, err := f.f.store.Jobs().Claim(t.Context(), domain.JobClaimRequest{Queue: domain.JobQueueMaintenance, Owner: "capacity-worker", Now: now, LeaseExpiresAt: now.Add(time.Hour)})
	require.NoError(t, err)
	require.Equal(t, job.ID, claim.Job.ID)
	active, _, err := f.f.store.GetActive(t.Context(), f.f.workspaceID)
	require.NoError(t, err)
	return domain.ModelTransitionStart{WorkspaceID: f.f.workspaceID, ModelID: modelID, JobID: job.ID, StartedAt: now,
		SelectionGeneration: active.SelectionGeneration, LeaseOwner: "capacity-worker", Force: force}
}

func (f switchingCapacity) seed(ctx context.Context, start domain.ModelTransitionStart) (map[string]any, error) {
	lsn, err := switchingLSN(ctx, f.conn)
	if err != nil {
		return nil, err
	}
	initial := time.Now()
	_, err = f.f.store.Begin(ctx, start)
	if err != nil {
		return nil, err
	}
	metrics := map[string]any{"BeginMs": float64(time.Since(initial)) / float64(time.Millisecond)}
	var batches []float64
	for {
		started := time.Now()
		done, err := f.f.store.SeedModelProjections(ctx, domain.ModelProjectionSeedCommand{WorkspaceID: start.WorkspaceID, ModelID: start.ModelID, JobID: start.JobID, LeaseOwner: start.LeaseOwner, Limit: 1000})
		if err != nil {
			return nil, err
		}
		batches = append(batches, float64(time.Since(started))/float64(time.Millisecond))
		if done {
			break
		}
	}
	var count int
	if err := f.conn.QueryRow(ctx, `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1 AND embedding_model_id=$2`, start.WorkspaceID, start.ModelID).Scan(&count); err != nil {
		return nil, err
	}
	if count != 1000000 {
		return nil, fmt.Errorf("seed count=%d", count)
	}
	metrics["SeededItems"], metrics["SeedBatchesAfterBegin"], metrics["SeedSeconds"] = count, len(batches), time.Since(initial).Seconds()
	metrics["SeedBatchMs"] = testutil.Percentiles(batches)
	metrics["WALBytes"], err = switchingWAL(ctx, f.conn, lsn)
	return metrics, err
}

func (f switchingCapacity) delta(ctx context.Context, start domain.ModelTransitionStart) (map[string]any, error) {
	lsn, err := switchingLSN(ctx, f.conn)
	if err != nil {
		return nil, err
	}
	initial := time.Now()
	_, err = f.f.store.Begin(ctx, start)
	if err != nil {
		return nil, err
	}
	metrics := map[string]any{"RetainedBeginMs": float64(time.Since(initial)) / float64(time.Millisecond)}
	topicID := uuid.UUID(md5.Sum([]byte("topic:1")))
	_, err = f.conn.Exec(ctx, `INSERT INTO topic_corrections(topic_id,item_id,membership,updated_at)
SELECT $1,item_id,'include',clock_timestamp() FROM item_projection_state WHERE workspace_id=$2 ORDER BY item_id LIMIT 10000`, topicID, f.f.workspaceID)
	if err != nil {
		return nil, err
	}
	topic, _, err := f.f.store.GetTopic(ctx, f.f.workspaceID, topicID)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	cleared, err := f.f.store.UpdateTopic(ctx, domain.TopicUpdate{Topic: topic, ClearCorrections: true})
	if err != nil {
		return nil, err
	}
	metrics["CaptureTransactionMs"] = float64(time.Since(started)) / float64(time.Millisecond)
	if len(cleared.ProjectionCommits) != 10000 {
		return nil, fmt.Errorf("capture count=%d", len(cleared.ProjectionCommits))
	}
	acknowledged, err := f.acknowledgeDelta(ctx, start)
	if err != nil {
		return nil, err
	}
	for key, value := range acknowledged {
		metrics[key] = value
	}
	started = time.Now()
	if err := f.activate(ctx, start); err != nil {
		return nil, err
	}
	metrics["CutoverMs"] = float64(time.Since(started)) / float64(time.Millisecond)
	metrics["WALBytes"], err = switchingWAL(ctx, f.conn, lsn)
	return metrics, err
}

func (f switchingCapacity) acknowledgeDelta(ctx context.Context, start domain.ModelTransitionStart) (map[string]any, error) {
	var generation uuid.UUID
	var attempt int
	if err := f.conn.QueryRow(ctx, `SELECT progress.generation,job.attempt FROM workspace_model_progress progress
 JOIN jobs job ON job.id=$3 AND job.workspace_id=progress.workspace_id
 WHERE progress.workspace_id=$1 AND progress.embedding_model_id=$2`, start.WorkspaceID, start.ModelID, start.JobID).Scan(&generation, &attempt); err != nil {
		return nil, err
	}
	count := 0
	var acknowledgements []float64
	started := time.Now()
	for {
		pending, err := f.f.store.ListPendingModelProjections(ctx, domain.ModelProjectionQuery{WorkspaceID: start.WorkspaceID, ModelID: start.ModelID, Limit: 100})
		if err != nil {
			return nil, err
		}
		if len(pending) == 0 {
			break
		}
		for _, commit := range pending {
			at := time.Now()
			completed, err := f.f.store.CompleteItemProjection(ctx, domain.ItemProjectionDelivery{WorkspaceID: start.WorkspaceID, ModelID: start.ModelID, ItemID: commit.ItemID,
				Revision: commit.ProjectionRevision, ModelGeneration: generation, Execution: domain.ProjectionExecution{JobID: start.JobID, LeaseOwner: start.LeaseOwner, Attempt: attempt}})
			if err != nil {
				return nil, err
			}
			if !completed {
				return nil, fmt.Errorf("target ACK rejected")
			}
			acknowledgements = append(acknowledgements, float64(time.Since(at))/float64(time.Millisecond))
			count++
		}
	}
	if count != 10000 {
		return nil, fmt.Errorf("delta count=%d", count)
	}
	return map[string]any{"TargetAcknowledgements": count, "ACKSeconds": time.Since(started).Seconds(), "ACKMs": testutil.Percentiles(acknowledgements)}, nil

}

func (f switchingCapacity) activate(ctx context.Context, start domain.ModelTransitionStart) error {
	if err := f.f.store.Activate(ctx, domain.ModelTransitionCutover{WorkspaceID: start.WorkspaceID, ModelID: start.ModelID, JobID: start.JobID,
		SelectionGeneration: start.SelectionGeneration, LeaseOwner: start.LeaseOwner, ActivatedAt: time.Now()}); err != nil {
		return err
	}
	_, err := f.f.store.Jobs().Finish(ctx, domain.JobFinish{ID: start.JobID, Owner: start.LeaseOwner, Status: domain.JobStatusSucceeded, UpdatedAt: time.Now()})
	return err
}

func measureSwitchingWork(t *testing.T, f stableAPIFixture, operation func(context.Context) (map[string]any, error)) map[string]any {
	t.Helper()
	type outcome struct {
		metrics map[string]any
		err     error
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	done := make(chan outcome, 1)
	joined := false
	defer func() {
		cancel()
		if !joined {
			testutil.Receive(t, done)
		}
	}()
	routes := performanceRoutes(t, f.runtime.server.Handler, f.workspaceID)
	go func() { metrics, err := operation(ctx); done <- outcome{metrics, err} }()
	measurements := map[string]map[string]any{}
	for {
		for _, route := range routes {
			result := measurePerformanceRoute(t, f.runtime.server.Handler, route)
			require.LessOrEqual(t, result.P95, 200.0, "route=%s", route)
			row := measurements[route]
			if row == nil {
				row = map[string]any{"Samples": 0, "MaxBatchP95Ms": 0.0, "MaxBatchP99Ms": 0.0}
				measurements[route] = row
			}
			row["Samples"] = row["Samples"].(int) + result.Samples
			row["MaxBatchP95Ms"] = max(row["MaxBatchP95Ms"].(float64), result.P95)
			row["MaxBatchP99Ms"] = max(row["MaxBatchP99Ms"].(float64), result.P99)
		}
		select {
		case result := <-done:
			joined = true
			require.NoError(t, result.err)
			result.metrics["HTTP"] = measurements
			return result.metrics
		default:
		}
	}
}

func switchingLSN(ctx context.Context, conn *pgx.Conn) (string, error) {
	var lsn string
	err := conn.QueryRow(ctx, `SELECT pg_current_wal_insert_lsn()::text`).Scan(&lsn)
	return lsn, err
}

func switchingWAL(ctx context.Context, conn *pgx.Conn, lsn string) (int64, error) {
	var bytes int64
	err := conn.QueryRow(ctx, `SELECT pg_wal_lsn_diff(pg_current_wal_insert_lsn(),$1::pg_lsn)::bigint`, lsn).Scan(&bytes)
	return bytes, err
}
