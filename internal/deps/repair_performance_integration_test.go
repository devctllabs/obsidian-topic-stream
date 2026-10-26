//go:build integration && performance

package deps

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/devctllabs/go-libs/di"
	"github.com/ethernity/obsidian-topic-stream/internal/service/indexing"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestProjectionRepairCapacity(t *testing.T) {
	require.NotEmpty(t, os.Getenv("TOPIC_STREAM_TEST_POSTGRES_DSN"))
	f, conn, _ := newPerformanceFixture(t)
	queue, err := di.Resolve[*jobs.Service](f.runtime.container)
	require.NoError(t, err)
	results := map[string]any{}
	results["missingJobs"] = measureRepairCapacity(t, f, conn, indexing.NewReconciler(f.store, queue))
	// Recreate the pathological full active prefix without a million Go enqueue round trips.
	_, err = conn.Exec(t.Context(), `INSERT INTO jobs(id,workspace_id,kind,queue,idempotency_key)
SELECT md5('active-repair:'||s.item_id)::uuid,s.workspace_id,'index-item','retrieval',
'index-item:'||s.item_id::text||':'||s.revision::text FROM item_projection_state s
WHERE NOT EXISTS(SELECT 1 FROM jobs j WHERE j.workspace_id=s.workspace_id
AND j.idempotency_key='index-item:'||s.item_id::text||':'||s.revision::text AND j.status IN ('queued','running','retry_wait'))`)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `ANALYZE jobs`)
	require.NoError(t, err)
	var legacyPlan json.RawMessage
	require.NoError(t, conn.QueryRow(t.Context(), `EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON)
SELECT state.workspace_id,state.item_id,state.revision FROM item_projection_state state
WHERE EXISTS(SELECT 1 FROM retrieval_projection_pending p WHERE p.workspace_id=state.workspace_id AND p.item_id=state.item_id AND p.revision=state.revision)
AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.workspace_id=state.workspace_id AND j.idempotency_key='index-item:'||state.item_id::text||':'||state.revision::text AND j.status IN ('queued','running','retry_wait'))
ORDER BY state.workspace_id,state.item_id LIMIT 100`).Scan(&legacyPlan))
	results["legacyAllActivePlan"] = legacyPlan
	results["allActive"] = measureRepairCapacity(t, f, conn, indexing.NewReconciler(f.store, queue))
	var active, distinct int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*),count(DISTINCT idempotency_key) FROM jobs WHERE kind='index-item'`).Scan(&active, &distinct))
	require.Equal(t, 1000000, active)
	require.Equal(t, active, distinct)
	encoded, err := json.MarshalIndent(results, "", "  ")
	require.NoError(t, err)
	if path := os.Getenv("TOPIC_STREAM_PERF_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(path, append(encoded, '\n'), 0o600))
	}
	t.Log(string(encoded))
}

type repairMeasurements struct {
	durations []float64
	err       error
}

func measureRepairCapacity(t *testing.T, f stableAPIFixture, conn *pgx.Conn, reconciler *indexing.Reconciler) map[string]any {
	t.Helper()
	var initialJobs int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE kind='index-item'`).Scan(&initialJobs))
	var lsn string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT pg_current_wal_insert_lsn()::text`).Scan(&lsn))
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	done := make(chan repairMeasurements, 1)
	started := time.Now()
	go func() {
		var measured repairMeasurements
		for ctx.Err() == nil || len(measured.durations) < 120 {
			start := time.Now()
			if err := reconciler.Reconcile(t.Context()); err != nil {
				measured.err = err
				break
			}
			measured.durations = append(measured.durations, float64(time.Since(start))/float64(time.Millisecond))
		}
		done <- measured
	}()
	var http []performanceResult
	for _, route := range []string{"/api/v1/feed", "/api/v1/jobs", "/api/v1/jobs?status=queued"} {
		result := measurePerformanceRoute(t, f.runtime.server.Handler, route)
		require.LessOrEqual(t, result.P95, 200.0)
		http = append(http, result)
	}
	stop()
	measured := <-done
	require.NoError(t, measured.err)
	elapsed := time.Since(started).Seconds()
	var finalJobs int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE kind='index-item'`).Scan(&finalJobs))
	var wal int64
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT pg_wal_lsn_diff(pg_current_wal_insert_lsn(),$1::pg_lsn)::bigint`, lsn).Scan(&wal))
	slices.Sort(measured.durations)
	n := len(measured.durations)
	return map[string]any{"batches": n, "inspectedItems": n * 100, "newJobs": finalJobs - initialJobs,
		"elapsedSeconds": elapsed, "jobsPerSecond": float64(finalJobs-initialJobs) / elapsed,
		"p50Milliseconds": measured.durations[n/2], "p95Milliseconds": measured.durations[n*95/100],
		"p99Milliseconds": measured.durations[n*99/100], "WALBytes": wal, "http": http}
}
