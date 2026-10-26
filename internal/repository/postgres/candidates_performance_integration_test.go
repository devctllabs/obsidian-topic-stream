//go:build integration && performance

package postgres_test

import (
	"encoding/json"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestCandidateCapacity(t *testing.T) {
	require.NotEmpty(t, os.Getenv("TOPIC_STREAM_TEST_POSTGRES_DSN"))
	f := newFeedFixture(t)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	seedCandidateCapacity(t, f, conn)
	results := map[string]any{}
	for name, statement := range map[string]string{
		"deleteByItem": `DELETE FROM topic_candidates WHERE workspace_id=$1 AND item_id=md5('candidate-item:25001')::uuid`,
		"fallbackKeys": `SELECT id FROM items WHERE workspace_id=$1 ORDER BY COALESCE(published_at,discovered_at) DESC,id DESC LIMIT 100`,
	} {
		tx, beginErr := conn.Begin(t.Context())
		require.NoError(t, beginErr)
		var plan json.RawMessage
		require.NoError(t, tx.QueryRow(t.Context(), "EXPLAIN(ANALYZE,BUFFERS,WAL,FORMAT JSON) "+statement, f.workspaceID).Scan(&plan))
		require.NoError(t, tx.Rollback(t.Context()))
		results[name] = plan
	}
	for _, table := range []string{"topic_candidates", "items"} {
		var size int64
		require.NoError(t, conn.QueryRow(t.Context(), `SELECT pg_total_relation_size($1::regclass)`, table).Scan(&size))
		results[table+"Bytes"] = size
	}
	var allocatedBefore, allocatedAfter runtime.MemStats
	runtime.ReadMemStats(&allocatedBefore)
	started := time.Now()
	candidates, err := f.store.ReadFeedCandidates(t.Context(), domain.FeedCandidateQuery{WorkspaceID: f.workspaceID, At: f.now, CandidateLimit: 100})
	require.NoError(t, err)
	require.Len(t, candidates, 50000, "all compatible matches must survive aggregation")
	runtime.ReadMemStats(&allocatedAfter)
	results["candidateReadMilliseconds"] = milliseconds(time.Since(started))
	results["candidateReadAllocatedBytes"] = allocatedAfter.TotalAlloc - allocatedBefore.TotalAlloc
	seen := make(map[uuid.UUID]int)
	for _, candidate := range candidates {
		seen[candidate.TopicID]++
	}
	require.Len(t, seen, 500)
	for _, count := range seen {
		require.Equal(t, 100, count)
	}
	var projectionMS, fallbackMS []float64
	var itemID uuid.UUID
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT md5('candidate-item:1')::uuid`).Scan(&itemID))
	for range 120 {
		started = time.Now()
		work, found, readErr := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
		require.NoError(t, readErr)
		require.True(t, found)
		require.Equal(t, int64(1), work.Projection.Version)
		projectionMS = append(projectionMS, milliseconds(time.Since(started)))
		started = time.Now()
		items, readErr := f.store.RecentUnpublishedFeed(t.Context(), domain.FeedCandidateQuery{WorkspaceID: f.workspaceID, At: f.now, CandidateLimit: 100})
		require.NoError(t, readErr)
		require.Len(t, items, 100)
		fallbackMS = append(fallbackMS, milliseconds(time.Since(started)))
	}
	results["projectionMilliseconds"], results["fallbackMilliseconds"] = testutil.Percentiles(projectionMS), testutil.Percentiles(fallbackMS)
	for _, legacy := range []bool{false, true} {
		tx, beginErr := conn.Begin(t.Context())
		require.NoError(t, beginErr)
		_, err = tx.Exec(t.Context(), `DELETE FROM topic_candidates WHERE topic_id=md5('candidate-topic:1')::uuid`)
		require.NoError(t, err)
		columns, values, name := "", "", "currentInsert100"
		if legacy {
			_, err = tx.Exec(t.Context(), `DROP INDEX topic_candidates_item_idx; ALTER TABLE topic_candidates ADD COLUMN inputs jsonb`)
			require.NoError(t, err)
			columns, values, name = ",inputs", ",jsonb_build_object('TopicRevision',1,'CorrectionEpoch',1,'CorrectionVersion',0,'CandidateLimit',100,'RRFK',60,'ModelID','00000000-0000-0000-0000-000000000000','GateVersion','','EmbeddingKey','','GateInclude',0,'GateExclude',0)", "legacyInsert100"
		}
		var plan json.RawMessage
		require.NoError(t, tx.QueryRow(t.Context(), `EXPLAIN(ANALYZE,BUFFERS,WAL,FORMAT JSON)
INSERT INTO topic_candidates(workspace_id,topic_id,item_id,position,score`+columns+`)
SELECT $1,md5('candidate-topic:1')::uuid,md5('candidate-item:'||n)::uuid,n,0.8`+values+` FROM generate_series(1,100) n`, f.workspaceID).Scan(&plan))
		results[name] = plan
		require.NoError(t, tx.Rollback(t.Context()))
	}
	encoded, err := json.MarshalIndent(results, "", "  ")
	require.NoError(t, err)
	if path := os.Getenv("TOPIC_STREAM_PERF_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(path, append(encoded, '\n'), 0o600))
	}
	t.Log(string(encoded))
}

func seedCandidateCapacity(t *testing.T, f feedFixture, conn *pgx.Conn) {
	t.Helper()
	_, err := conn.Exec(t.Context(), `INSERT INTO topics(id,workspace_id,name,include_text)
SELECT md5('candidate-topic:'||n)::uuid,$1,'Topic '||n,'Research' FROM generate_series(1,500) n`, f.workspaceID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO items(id,workspace_id,title,url,canonical_url,details,discovered_at)
SELECT md5('candidate-item:'||n)::uuid,$1,'Item '||n,'https://candidate/'||n,'https://candidate/'||n,
'{"type":"article","article":{"description":"Data"}}',$2 FROM generate_series(1,50000) n`, f.workspaceID, f.now)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO item_projection_state(workspace_id,item_id,revision,updated_at)
SELECT workspace_id,id,id,now() FROM items WHERE workspace_id=$1`, f.workspaceID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO workspace_settings(workspace_id,embedding_model_id,version,document,updated_at) VALUES($1,$2,1,'{"retrieval":{"candidateLimit":100,"rrfK":60}}',now())`, f.workspaceID, domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	inputs, err := json.Marshal(domain.FeedCandidateInputs{TopicRevision: 1, CorrectionEpoch: 1, CandidateLimit: 100, RRFK: 60})
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO topic_candidate_state(workspace_id,topic_id,ready_version,prepared_input_version,inputs)
SELECT workspace_id,id,1,1,$2::jsonb FROM topics WHERE workspace_id=$1`, f.workspaceID, inputs)
	require.NoError(t, err)
	var hasInputs bool
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='topic_candidates' AND column_name='inputs')`).Scan(&hasInputs))
	columns, values := "", ""
	if hasInputs {
		columns, values = ",inputs", ",$2::jsonb"
	}
	args := []any{f.workspaceID}
	if hasInputs {
		args = append(args, inputs)
	}
	_, err = conn.Exec(t.Context(), `INSERT INTO topic_candidates(workspace_id,topic_id,item_id,position,score`+columns+`)
SELECT $1,md5('candidate-topic:'||((n-1)/100+1))::uuid,md5('candidate-item:'||n)::uuid,(n-1)%100+1,0.8`+values+`
FROM generate_series(1,50000) n`, args...)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `ANALYZE`)
	require.NoError(t, err)
}
