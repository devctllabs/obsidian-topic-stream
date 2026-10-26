//go:build integration && performance

package deps

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/testutil"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type ackCapacitySample struct {
	ACKPhaseMs         float64
	ListingMs          float64
	OperationMs        map[string]float64
	OperationSamplesMs []float64
	WALBytes           int64
	Statements         json.RawMessage
}

type ackCapacityScenario struct {
	Topics     int
	Mixed      bool
	Samples    []ackCapacitySample
	ACKPhaseMs map[string]float64
	HTTP       map[string]map[string]float64
}

type ackCapacityFixture struct {
	f          stableAPIFixture
	conn       *pgx.Conn
	sourceID   uuid.UUID
	modelID    uuid.UUID
	inactive   uuid.UUID
	generation uuid.UUID
	execution  domain.ProjectionExecution
	itemIDs    []uuid.UUID
}

func TestProjectionACKMatrixCapacity(t *testing.T) {
	require.NotEmpty(t, os.Getenv("TOPIC_STREAM_TEST_POSTGRES_DSN"), "use the capped disposable runner")
	f, conn, sourceID := newPerformanceFixture(t)
	fixture := newACKCapacityFixture(t, f, conn, sourceID)
	if os.Getenv("TOPIC_STREAM_CAPTURE_STATEMENTS") == "true" {
		_, err := conn.Exec(t.Context(), `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`)
		require.NoError(t, err)
	}
	var scenarios []ackCapacityScenario
	for _, topics := range []int{100, 500} {
		_, err := conn.Exec(t.Context(), `INSERT INTO topics(id,workspace_id,name,include_text)
 SELECT md5('topic:'||n)::uuid,$1,'Topic '||n,'' FROM generate_series(1,$2::integer) n ON CONFLICT DO NOTHING`, f.workspaceID, topics)
		require.NoError(t, err)
		_, err = conn.Exec(t.Context(), `INSERT INTO topic_candidate_state(workspace_id,topic_id)
 SELECT workspace_id,id FROM topics WHERE workspace_id=$1 ON CONFLICT DO NOTHING`, f.workspaceID)
		require.NoError(t, err)
		for _, mixed := range []bool{false, true} {
			scenarios = append(scenarios, fixture.measure(t, topics, mixed))
			writeACKCapacityReport(t, scenarios)
		}
	}
	var inactive int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending pending JOIN perf_seed seed ON seed.id=pending.item_id
 WHERE pending.workspace_id=$1 AND pending.embedding_model_id=$2`, f.workspaceID, fixture.inactive).Scan(&inactive))
	require.Equal(t, 1000000, inactive, "original inactive backlog receives no delivery")
	for _, scenario := range scenarios {
		target := 1000.0
		if scenario.Mixed {
			target = 3000
		}
		require.LessOrEqual(t, scenario.ACKPhaseMs["p95"], target, "topics=%d mixed=%t", scenario.Topics, scenario.Mixed)
		for route, d := range scenario.HTTP {
			require.LessOrEqual(t, d["p95"], 200.0, "route=%s", route)
		}
	}
}

func newACKCapacityFixture(t *testing.T, f stableAPIFixture, conn *pgx.Conn, sourceID uuid.UUID) ackCapacityFixture {
	t.Helper()
	switching := prepareSwitchingCapacity(t, f, conn)
	switching.selectModel(t, switching.alternate)
	start := switching.start(t, switching.alternate, false)
	_, err := f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	// External delivery before this 10k delta is synthetic, as in the historical ACK fixture.
	_, err = conn.Exec(t.Context(), `DELETE FROM retrieval_projection_pending WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, start.ModelID)
	require.NoError(t, err)
	fixture := ackCapacityFixture{f: f, conn: conn, sourceID: sourceID, modelID: start.ModelID, inactive: switching.inactive, execution: domain.ProjectionExecution{JobID: start.JobID, LeaseOwner: start.LeaseOwner}}
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT attempt FROM jobs WHERE id=$1`, start.JobID).Scan(&fixture.execution.Attempt))
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT generation FROM workspace_model_progress WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, start.ModelID).Scan(&fixture.generation))
	rows, err := conn.Query(t.Context(), `SELECT item_id FROM item_projection_state WHERE workspace_id=$1 ORDER BY item_id LIMIT 10000`, f.workspaceID)
	require.NoError(t, err)
	fixture.itemIDs, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	require.NoError(t, err)
	require.Len(t, fixture.itemIDs, 10000)
	return fixture
}

func (fixture ackCapacityFixture) measure(t *testing.T, topics int, mixed bool) ackCapacityScenario {
	t.Helper()
	stop := func() {}
	if mixed {
		stop = performanceBackground(t, fixture.f.store, fixture.f.workspaceID, fixture.sourceID)
	}
	load := startCaptureHTTP(t, fixture.f, mixed)
	scenario := ackCapacityScenario{Topics: topics, Mixed: mixed}
	var elapsed []float64
	for sample := range 5 {
		entry := fixture.measureSample(t)
		scenario.Samples = append(scenario.Samples, entry)
		elapsed = append(elapsed, entry.ACKPhaseMs)
		t.Logf("ACK topics=%d mixed=%t sample=%d phase_ms=%.3f listing_ms=%.3f", topics, mixed, sample+1, entry.ACKPhaseMs, entry.ListingMs)
	}
	load.stop()
	stop()
	scenario.HTTP = load.distributions(t)
	scenario.ACKPhaseMs = testutil.Percentiles(elapsed)
	return scenario
}

func (fixture ackCapacityFixture) measureSample(t *testing.T) ackCapacitySample {
	t.Helper()
	_, err := fixture.conn.Exec(t.Context(), `INSERT INTO retrieval_projection_pending(workspace_id,item_id,embedding_model_id,revision,updated_at,model_generation)
 SELECT workspace_id,item_id,$2,revision,clock_timestamp(),$3 FROM item_projection_state WHERE workspace_id=$1 AND item_id=ANY($4::uuid[])
 ON CONFLICT(workspace_id,item_id,embedding_model_id) DO UPDATE SET revision=EXCLUDED.revision,model_generation=EXCLUDED.model_generation`, fixture.f.workspaceID, fixture.modelID, fixture.generation, fixture.itemIDs)
	require.NoError(t, err)
	listed := time.Now()
	commits := fixture.listCohort(t)
	entry := ackCapacitySample{ListingMs: float64(time.Since(listed)) / float64(time.Millisecond)}
	profile := capacitySQLProfile{conn: fixture.conn}
	profile.resetStatements(t)
	lsn, err := switchingLSN(t.Context(), fixture.conn)
	require.NoError(t, err)
	started := time.Now()
	operations := fixture.acknowledge(t, commits)
	entry.ACKPhaseMs = float64(time.Since(started)) / float64(time.Millisecond)
	entry.OperationMs = testutil.Percentiles(operations)
	entry.OperationSamplesMs = operations
	entry.WALBytes, err = switchingWAL(t.Context(), fixture.conn, lsn)
	require.NoError(t, err)
	entry.Statements = profile.statements(t)
	return entry
}

func ackCapacityBatchSize() int {
	if os.Getenv("TOPIC_STREAM_ACK_BATCH") == "true" {
		return 100
	}
	return 1
}

func (fixture ackCapacityFixture) acknowledge(t *testing.T, commits []domain.ItemProjectionCommit) []float64 {
	t.Helper()
	batchSize := ackCapacityBatchSize()
	var times []float64
	for offset := 0; offset < len(commits); offset += batchSize {
		var deliveries []domain.ItemProjectionDelivery
		for _, commit := range commits[offset:min(offset+batchSize, len(commits))] {
			deliveries = append(deliveries, domain.ItemProjectionDelivery{WorkspaceID: fixture.f.workspaceID, ItemID: commit.ItemID, Revision: commit.ProjectionRevision, ModelID: fixture.modelID, ModelGeneration: fixture.generation, Execution: fixture.execution})
		}
		started := time.Now()
		if batchSize == 1 {
			accepted, err := fixture.f.store.CompleteItemProjection(t.Context(), deliveries[0])
			require.NoError(t, err)
			require.True(t, accepted)
		} else {
			accepted, err := fixture.f.store.CompleteItemProjections(t.Context(), deliveries)
			require.NoError(t, err)
			require.Len(t, accepted, len(deliveries))
			for _, ok := range accepted {
				require.True(t, ok)
			}
		}
		times = append(times, float64(time.Since(started))/float64(time.Millisecond))
	}
	return times
}

func (fixture ackCapacityFixture) listCohort(t *testing.T) []domain.ItemProjectionCommit {
	t.Helper()
	wanted := make(map[uuid.UUID]bool, len(fixture.itemIDs))
	for _, id := range fixture.itemIDs {
		wanted[id] = true
	}
	var commits []domain.ItemProjectionCommit
	var after *uuid.UUID
	for len(commits) < len(fixture.itemIDs) {
		page, err := fixture.f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: fixture.f.workspaceID, ModelID: fixture.modelID, AfterItemID: after, Limit: 100})
		require.NoError(t, err)
		require.NotEmpty(t, page, "all original cohort entries must remain pending")
		for _, commit := range page {
			if wanted[commit.ItemID] {
				commits = append(commits, commit)
			}
		}
		last := page[len(page)-1].ItemID
		after = &last
	}
	require.Len(t, commits, 10000)
	return commits
}

func writeACKCapacityReport(t *testing.T, scenarios []ackCapacityScenario) {
	t.Helper()
	report := map[string]any{"Items": 1000000, "Models": 3, "ACKsPerSample": 10000, "SamplesPerScenario": 5, "Fixture": "full 4KiB Markdown, 1M publications/display/Jobs, 1M inactive pending; transition ACKs with real running Job, model generation and collection binding; prior external delivery synthetic", "HTTP": "4 continuous in-process clients / seven Feed and Jobs GET routes, background claim/ACK/ingestion/preparation", "Scope": "whole database ACK phase; listing separate; provider/Qdrant delivery excluded; mixed WAL includes background work", "Scenarios": scenarios}
	report["BatchSize"] = ackCapacityBatchSize()
	encoded, err := json.MarshalIndent(report, "", "  ")
	require.NoError(t, err)
	if path := os.Getenv("TOPIC_STREAM_PERF_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(path, append(encoded, '\n'), 0o600))
	}
}
