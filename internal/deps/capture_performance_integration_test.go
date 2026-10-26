//go:build integration && performance

package deps

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/testutil"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type captureMatrixSample struct {
	FirstCapture      bool
	SourceOffset      int
	TransactionMs     float64
	WALBytes          int64
	Statements        json.RawMessage
	MaintenanceBefore json.RawMessage
	MaintenanceAfter  json.RawMessage
}

type captureMatrixScenario struct {
	Topics            int
	Mixed             bool
	SourceOffset      int
	Samples           []captureMatrixSample
	TransactionMs     map[string]float64
	FirstCaptureMs    map[string]float64
	RepeatedCaptureMs map[string]float64
	HTTP              map[string]map[string]float64
}

func TestCaptureMatrixCapacity(t *testing.T) {
	require.NotEmpty(t, os.Getenv("TOPIC_STREAM_TEST_POSTGRES_DSN"), "use the capped disposable runner")
	f, conn, sourceID := newPerformanceFixture(t)
	if os.Getenv("TOPIC_STREAM_CAPTURE_STATEMENTS") == "true" {
		_, err := conn.Exec(t.Context(), `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`)
		require.NoError(t, err)
	}
	inactive := seedCaptureMatrixModels(t, f, conn)
	var results []captureMatrixScenario
	for topicIndex, topics := range []int{100, 500} {
		_, err := conn.Exec(t.Context(), `INSERT INTO topics(id,workspace_id,name,include_text)
 SELECT md5('topic:'||n)::uuid,$1,'Topic '||n,'' FROM generate_series(1,$2::integer) n ON CONFLICT DO NOTHING`, f.workspaceID, topics)
		require.NoError(t, err)
		_, err = conn.Exec(t.Context(), `INSERT INTO topic_candidate_state(workspace_id,topic_id)
 SELECT workspace_id,id FROM topics WHERE workspace_id=$1 ON CONFLICT DO NOTHING`, f.workspaceID)
		require.NoError(t, err)
		_, err = conn.Exec(t.Context(), "ANALYZE")
		require.NoError(t, err)
		for _, mixed := range []bool{false, true} {
			matrix := captureMatrixFixture{f: f, conn: conn, sourceID: sourceID, sourceOffset: captureCohortOffset(topicIndex, mixed)}
			results = append(results, matrix.measure(t, topics, mixed))
		}
	}
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending pending JOIN perf_seed seed ON seed.id=pending.item_id WHERE pending.workspace_id=$1 AND pending.embedding_model_id=$2`, f.workspaceID, inactive).Scan(&count))
	var storage int64
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT pg_total_relation_size('retrieval_projection_pending')`).Scan(&storage))
	var sharedBuffers string
	require.NoError(t, conn.QueryRow(t.Context(), `SHOW shared_buffers`).Scan(&sharedBuffers))
	report := map[string]any{"Items": 1000000, "Models": 3, "Fixture": "full 4KiB Markdown, Source Entries, 1M Feed publications/display rows, 1M Jobs, 1M inactive pending", "HTTP": "4 continuous clients rotating over seven Feed/Jobs GET routes; no pacing; current performanceBackground writes", "WAL": "whole transaction LSN range; mixed includes concurrent background WAL", "SamplesPerScenario": len(results[0].Samples), "SharedBuffers": sharedBuffers, "FreshCohorts": os.Getenv("TOPIC_STREAM_CAPTURE_FRESH_KEYS") == "true", "PendingStorageBytes": storage, "Scenarios": results}
	encoded, err := json.MarshalIndent(report, "", "  ")
	require.NoError(t, err)
	if path := os.Getenv("TOPIC_STREAM_PERF_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(path, append(encoded, '\n'), 0o600))
	}
	require.Equal(t, 1000000, count, "original inactive backlog remains coalesced and receives no delivery")
	for _, scenario := range results {
		for _, sample := range scenario.Samples {
			require.LessOrEqual(t, sample.TransactionMs, captureTargetMs(sample.FirstCapture, scenario.Mixed), "topics=%d mixed=%t first=%t offset=%d", scenario.Topics, scenario.Mixed, sample.FirstCapture, sample.SourceOffset)
		}
		for route, distribution := range scenario.HTTP {
			require.LessOrEqual(t, distribution["p95"], 200.0, "route=%s", route)
		}
	}
}

func captureTargetMs(first, mixed bool) float64 {
	if first {
		if mixed {
			return 3000
		}
		return 1500
	}
	if mixed {
		return 1000
	}
	return 500
}

type captureMatrixFixture struct {
	f            stableAPIFixture
	conn         *pgx.Conn
	sourceID     uuid.UUID
	sourceOffset int
}

func captureCohortOffset(topicIndex int, mixed bool) int {
	if os.Getenv("TOPIC_STREAM_CAPTURE_FRESH_KEYS") != "true" {
		return 0
	}
	if mixed {
		return topicIndex*100000 + 50000
	}
	return topicIndex * 100000
}

func (matrix captureMatrixFixture) measure(t *testing.T, topics int, mixed bool) captureMatrixScenario {
	t.Helper()
	stop := func() {}
	if mixed {
		stop = performanceBackground(t, matrix.f.store, matrix.f.workspaceID, matrix.sourceID)
	}
	load := startCaptureHTTP(t, matrix.f, mixed)
	scenario := captureMatrixScenario{Topics: topics, Mixed: mixed, SourceOffset: matrix.sourceOffset}
	freshCohorts := os.Getenv("TOPIC_STREAM_CAPTURE_FRESH_KEYS") == "true"
	sampleCount := 5
	if freshCohorts {
		// Five distinct first captures, each followed by an edit coalescing the same keys.
		sampleCount = 10
	}
	var itemIDs []uuid.UUID
	for sample := range sampleCount {
		capture := matrix
		if freshCohorts {
			capture.sourceOffset += (sample / 2) * 10000
		}
		first := firstCaptureSample(freshCohorts, topics, mixed, sample)
		if sample == 0 || first {
			itemIDs = capture.cohort(t)
		}
		topicID := uuid.UUID(md5.Sum([]byte("topic:1")))
		capture.seedCorrections(t, topicID, itemIDs)
		topic, _, err := matrix.f.store.GetTopic(t.Context(), matrix.f.workspaceID, topicID)
		require.NoError(t, err)
		lsn, err := switchingLSN(t.Context(), matrix.conn)
		require.NoError(t, err)
		maintenance := matrix.maintenance(t)
		(capacitySQLProfile{conn: matrix.conn}).resetStatements(t)
		started := time.Now()
		result, err := matrix.f.store.UpdateTopic(t.Context(), domain.TopicUpdate{Topic: topic, ClearCorrections: true})
		require.NoError(t, err)
		elapsed := float64(time.Since(started)) / float64(time.Millisecond)
		require.Len(t, result.ProjectionCommits, 10000)
		wal, err := switchingWAL(t.Context(), matrix.conn, lsn)
		require.NoError(t, err)
		scenario.Samples = append(scenario.Samples, captureMatrixSample{FirstCapture: first, SourceOffset: capture.sourceOffset, TransactionMs: elapsed, WALBytes: wal, Statements: (capacitySQLProfile{conn: matrix.conn}).statements(t), MaintenanceBefore: maintenance, MaintenanceAfter: matrix.maintenance(t)})
		t.Logf("capture topics=%d mixed=%t sample=%d first=%t offset=%d elapsed_ms=%.3f WAL=%d", topics, mixed, sample+1, first, capture.sourceOffset, elapsed, wal)
	}
	load.stop()
	stop()
	scenario.HTTP = load.distributions(t)
	scenario.summarize()
	return scenario
}

func firstCaptureSample(fresh bool, topics int, mixed bool, sample int) bool {
	if fresh {
		return sample%2 == 0
	}
	return topics == 100 && !mixed && sample == 0
}

func (scenario *captureMatrixScenario) summarize() {
	var times, firstTimes, repeatedTimes []float64
	for _, sample := range scenario.Samples {
		times = append(times, sample.TransactionMs)
		if sample.FirstCapture {
			firstTimes = append(firstTimes, sample.TransactionMs)
		} else {
			repeatedTimes = append(repeatedTimes, sample.TransactionMs)
		}
	}
	scenario.TransactionMs = testutil.Percentiles(times)
	if len(firstTimes) > 0 {
		scenario.FirstCaptureMs = testutil.Percentiles(firstTimes)
	}
	scenario.RepeatedCaptureMs = testutil.Percentiles(repeatedTimes)
}

func (matrix captureMatrixFixture) maintenance(t *testing.T) json.RawMessage {
	t.Helper()
	if os.Getenv("TOPIC_STREAM_CAPTURE_STATEMENTS") != "true" {
		return nil
	}
	var encoded json.RawMessage
	err := matrix.conn.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'vacuum',COALESCE((SELECT jsonb_agg(to_jsonb(work)) FROM pg_stat_progress_vacuum work),'[]'::jsonb),
 'checkpointer',(SELECT to_jsonb(checkpointer) FROM pg_stat_checkpointer checkpointer))`).Scan(&encoded)
	require.NoError(t, err)
	return encoded
}

func (matrix captureMatrixFixture) cohort(t *testing.T) []uuid.UUID {
	t.Helper()
	rows, err := matrix.conn.Query(t.Context(), `SELECT item_id FROM item_projection_state WHERE workspace_id=$1 ORDER BY item_id LIMIT 10000 OFFSET $2`, matrix.f.workspaceID, matrix.sourceOffset)
	require.NoError(t, err)
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	require.NoError(t, err)
	require.Len(t, ids, 10000)
	return ids
}

func (matrix captureMatrixFixture) seedCorrections(t *testing.T, topicID uuid.UUID, itemIDs []uuid.UUID) {
	t.Helper()
	tx, err := matrix.conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, err = tx.Exec(t.Context(), `INSERT INTO topic_corrections(topic_id,item_id,membership,updated_at)
 SELECT $1,item_id,'include',clock_timestamp() FROM unnest($2::uuid[]) item_id`, topicID, itemIDs)
	require.NoError(t, err)
	// Reproduce the effective display before Clear, including explicit additions beyond publication Topics.
	_, err = tx.Exec(t.Context(), `UPDATE feed_display_entries display SET topic_ids=ARRAY(
 SELECT DISTINCT topic_id FROM unnest(display.topic_ids||ARRAY[$1::uuid]) topic_id ORDER BY topic_id)
 FROM topic_corrections correction WHERE correction.topic_id=$1 AND correction.item_id=display.item_id
 AND display.workspace_id=$2 AND NOT ($1=ANY(display.topic_ids))`, topicID, matrix.f.workspaceID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
}

func seedCaptureMatrixModels(t *testing.T, f stableAPIFixture, conn *pgx.Conn) uuid.UUID {
	t.Helper()
	var models []uuid.UUID
	for range 2 {
		model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
		require.NoError(t, err)
		model.ID, model.Name = uuid.New(), uuid.NewString()
		require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
		models = append(models, model.ID)
	}
	_, err := conn.Exec(t.Context(), `INSERT INTO workspace_model_progress(workspace_id,embedding_model_id,seed_complete,selected_at)
 SELECT $1,id,true,clock_timestamp() FROM unnest($2::uuid[]) id ON CONFLICT DO NOTHING`, f.workspaceID, models)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE retrieval_projection_pending SET embedding_model_id=$2 WHERE workspace_id=$1`, f.workspaceID, models[0])
	require.NoError(t, err)
	return models[0]
}

type captureHTTP struct {
	cancel  context.CancelFunc
	workers sync.WaitGroup
	mutex   sync.Mutex
	times   map[string][]float64
	failed  int
}

func startCaptureHTTP(t *testing.T, f stableAPIFixture, enabled bool) *captureHTTP {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	load := &captureHTTP{cancel: cancel, times: make(map[string][]float64)}
	t.Cleanup(load.stop)
	if !enabled {
		return load
	}
	routes := performanceRoutes(t, f.runtime.server.Handler, f.workspaceID)
	for worker := range 4 {
		load.workers.Go(func() {
			for index := worker; ctx.Err() == nil; index++ {
				route := routes[index%len(routes)]
				request := httptest.NewRequest(http.MethodGet, route, nil).WithContext(ctx)
				response := httptest.NewRecorder()
				started := time.Now()
				f.runtime.server.Handler.ServeHTTP(response, request)
				elapsed := float64(time.Since(started)) / float64(time.Millisecond)
				if ctx.Err() != nil {
					return
				}
				load.mutex.Lock()
				load.times[route] = append(load.times[route], elapsed)
				if response.Code != http.StatusOK {
					load.failed++
				}
				load.mutex.Unlock()
			}
		})
	}
	return load
}

func (load *captureHTTP) stop() { load.cancel(); load.workers.Wait() }

func (load *captureHTTP) distributions(t *testing.T) map[string]map[string]float64 {
	t.Helper()
	require.Zero(t, load.failed)
	result := make(map[string]map[string]float64)
	for route, times := range load.times {
		distribution := testutil.Percentiles(times)
		distribution["samples"] = float64(len(times))
		result[route] = distribution
	}
	return result
}
