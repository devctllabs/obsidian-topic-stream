//go:build integration && performance

package postgres_test

import (
	"encoding/json"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestFeedDisplayRefreshBoundsCorrectionWorkAfterWorkspaceTopicGrowth(t *testing.T) {
	f := newFeedFixture(t)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `ALTER TABLE topics SET (autovacuum_enabled=false)`)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO workspaces(id) SELECT md5('other-workspace:'||n)::uuid FROM generate_series(1,500) n`)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO topics(id,workspace_id,name,include_text)
    SELECT md5('other-topic:'||n)::uuid,md5('other-workspace:'||n)::uuid,'Other','' FROM generate_series(1,500) n`)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `ANALYZE topics`)
	require.NoError(t, err)
	first, second := f.topic(t), f.topic(t)
	_, err = conn.Exec(t.Context(), `INSERT INTO topics(id,workspace_id,name,include_text)
    SELECT md5('new-topic:'||n)::uuid,$1,'New '||n,'' FROM generate_series(1,498) n`, f.workspaceID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO items(id,workspace_id,details,title,url,canonical_url)
SELECT md5(n::text)::uuid,$1,'{"type":"article","article":{}}','Item','https://plan.example/'||n,'https://plan.example/'||n FROM generate_series(1,2000) n`, f.workspaceID)
	require.NoError(t, err)
	seed := `INSERT INTO topic_corrections(topic_id,item_id,membership,updated_at)
SELECT topic,id,CASE WHEN topic=$1 THEN 'exclude' ELSE 'include' END,clock_timestamp()
FROM items CROSS JOIN unnest(ARRAY[$1::uuid,$2::uuid]) topic WHERE workspace_id=$3`
	_, err = conn.Exec(t.Context(), `ALTER TABLE topic_corrections SET (autovacuum_enabled=false)`)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), seed, first, second, f.workspaceID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `ANALYZE topic_corrections`)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO feed_publications(workspace_id,item_id,batch_id,published_at,ordinal,matched_topic_ids,score,pipeline,model_id)
SELECT $1,id,$2,clock_timestamp(),row_number() OVER(ORDER BY id),ARRAY[$3::uuid],0.8,'fixture',$4 FROM items WHERE workspace_id=$1 ORDER BY id LIMIT 2000`, f.workspaceID, uuid.New(), first, uuid.New())
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO feed_display_entries(workspace_id,item_id,ordinal,topic_ids)
SELECT workspace_id,item_id,ordinal,matched_topic_ids FROM feed_publications WHERE workspace_id=$1`, f.workspaceID)
	require.NoError(t, err)
	rows, err := conn.Query(t.Context(), `SELECT item_id FROM feed_publications WHERE workspace_id=$1 ORDER BY item_id`, f.workspaceID)
	require.NoError(t, err)
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	require.NoError(t, err)
	plan, err := postgres.ExplainFeedDisplayForTest(t.Context(), f.store, f.workspaceID, ids)
	require.NoError(t, err)
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(string(plan))
		}
	})
	var decoded []struct {
		Plan struct {
			Hits  int64 `json:"Shared Hit Blocks"`
			Reads int64 `json:"Shared Read Blocks"`
		}
	}
	require.NoError(t, json.Unmarshal(plan, &decoded))
	require.Len(t, decoded, 1)
	blocks := decoded[0].Plan.Hits + decoded[0].Plan.Reads
	t.Logf("refresh 2000 Items after Workspace Topic growth: shared blocks=%d", blocks)
	require.Less(t, blocks, int64(100000), "corrections must not be rescanned once per Item")
	require.NoError(t, postgres.RefreshFeedDisplayForTest(t.Context(), f.store, f.workspaceID, ids))
	var correct int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM feed_display_entries WHERE workspace_id=$1 AND topic_ids=ARRAY[$2::uuid]`, f.workspaceID, second).Scan(&correct))
	require.Equal(t, 2000, correct)
}
