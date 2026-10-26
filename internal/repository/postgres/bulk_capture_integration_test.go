//go:build integration

package postgres_test

import (
	"fmt"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestBulkCaptureKeepsLatestRevisionForEveryRetainedGeneration(t *testing.T) {
	// The current capture MERGE and display refresh both chunk at 1000 Items.
	// Destination fan-out is independent of empty/single/boundary input shape.
	for _, scenario := range []struct{ models, items int }{
		{1, 0}, {1, 1}, {1, 1000}, {1, 1001}, {3, 1001},
	} {
		t.Run(fmt.Sprintf("models=%d/items=%d", scenario.models, scenario.items), func(t *testing.T) {
			f := newFeedFixture(t)
			conn, err := pgx.Connect(t.Context(), f.dsn)
			require.NoError(t, err)
			defer func() { require.NoError(t, conn.Close(t.Context())) }()
			seedBulkCaptureModels(t, f, scenario.models)
			topicID := f.topic(t)
			_, err = conn.Exec(t.Context(), `INSERT INTO items(id,workspace_id,details,title,url,canonical_url)
 SELECT md5(n::text)::uuid,$1,'{"type":"article","article":{}}','Item '||n,'https://bulk.example/'||n,'https://bulk.example/'||n FROM generate_series(1,$2::integer) n`, f.workspaceID, scenario.items)
			require.NoError(t, err)
			_, err = conn.Exec(t.Context(), `INSERT INTO item_projection_state(workspace_id,item_id,revision,updated_at)
 SELECT workspace_id,id,id,$2 FROM items WHERE workspace_id=$1`, f.workspaceID, f.now)
			require.NoError(t, err)
			for iteration := range 2 {
				_, err = conn.Exec(t.Context(), `INSERT INTO topic_corrections(topic_id,item_id,membership,updated_at)
 SELECT $1,id,'include',$3 FROM items WHERE workspace_id=$2`, topicID, f.workspaceID, f.now)
				require.NoError(t, err)
				topic, _, err := f.store.GetTopic(t.Context(), f.workspaceID, topicID)
				require.NoError(t, err)
				result, err := f.store.UpdateTopic(t.Context(), domain.TopicUpdate{Topic: topic, ClearCorrections: true})
				require.NoError(t, err)
				require.Len(t, result.ProjectionCommits, scenario.items)
				var pending, valid int
				require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*),count(*) FILTER(WHERE pending.revision=state.revision AND pending.model_generation=progress.generation AND state.version=$2)
 FROM retrieval_projection_pending pending JOIN item_projection_state state USING(workspace_id,item_id)
 JOIN workspace_model_progress progress USING(workspace_id,embedding_model_id) WHERE pending.workspace_id=$1`, f.workspaceID, iteration+2).Scan(&pending, &valid))
				require.Equal(t, scenario.items*scenario.models, pending)
				require.Equal(t, pending, valid)
			}
		})
	}
}

func seedBulkCaptureModels(t *testing.T, f feedFixture, count int) {
	t.Helper()
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	for i := range count {
		model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
		require.NoError(t, err)
		if i > 0 {
			model.ID, model.Name = uuid.New(), uuid.NewString()
			require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
		}
		_, err = conn.Exec(t.Context(), `INSERT INTO workspace_model_progress(workspace_id,embedding_model_id,generation,seed_complete,selected_at)
 VALUES($1,$2,$3,true,$4) ON CONFLICT(workspace_id,embedding_model_id) DO UPDATE SET generation=EXCLUDED.generation`, f.workspaceID, model.ID, uuid.New(), f.now)
		require.NoError(t, err)
	}
}

func TestBulkCaptureCreatesMissingProjectionState(t *testing.T) {
	f := newFeedFixture(t)
	seedBulkCaptureModels(t, f, 3)
	itemID, topicID := f.item(t), f.topic(t)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `DELETE FROM item_projection_state WHERE workspace_id=$1 AND item_id=$2`, f.workspaceID, itemID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO topic_corrections(topic_id,item_id,membership,updated_at) VALUES($1,$2,'include',$3)`, topicID, itemID, f.now)
	require.NoError(t, err)
	topic, _, err := f.store.GetTopic(t.Context(), f.workspaceID, topicID)
	require.NoError(t, err)
	result, err := f.store.UpdateTopic(t.Context(), domain.TopicUpdate{Topic: topic, ClearCorrections: true})
	require.NoError(t, err)
	require.Len(t, result.ProjectionCommits, 1)
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending pending
 JOIN item_projection_state state USING(workspace_id,item_id) WHERE pending.workspace_id=$1 AND pending.item_id=$2
 AND pending.revision=state.revision AND state.version=1`, f.workspaceID, itemID).Scan(&count))
	require.Equal(t, 3, count)
}
