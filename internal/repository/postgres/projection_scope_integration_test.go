//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestProjectionForeignKeysRejectItemFromAnotherWorkspace(t *testing.T) {
	f := newFeedFixture(t)
	seedBulkCaptureModels(t, f, 3)
	itemID, otherWorkspace := f.item(t), uuid.New()
	require.NoError(t, f.store.EnsureWorkspace(t.Context(), otherWorkspace))
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	for _, table := range []string{"item_projection_state", "retrieval_projection_pending"} {
		t.Run(table, func(t *testing.T) {
			var err error
			if table == "item_projection_state" {
				_, err = conn.Exec(t.Context(), `INSERT INTO item_projection_state(workspace_id,item_id,revision,updated_at) VALUES($1,$2,$3,$4)`, otherWorkspace, itemID, uuid.New(), f.now)
			} else {
				_, err = conn.Exec(t.Context(), `INSERT INTO retrieval_projection_pending(workspace_id,item_id,embedding_model_id,revision,updated_at) VALUES($1,$2,$3,$4,$5)`, otherWorkspace, itemID, domain.DefaultEmbeddingModelID, uuid.New(), f.now)
			}
			var violation *pgconn.PgError
			require.ErrorAs(t, err, &violation)
			require.Equal(t, "23503", violation.Code)
			constraint := "item_projection_state_item_scope_fkey"
			if table == "retrieval_projection_pending" {
				constraint = "retrieval_projection_pending_state_fkey"
			}
			require.Equal(t, constraint, violation.ConstraintName)
		})
	}
}

func TestProjectionStateDeletionCascadesPendingAndRejectsOrphanWork(t *testing.T) {
	f := newFeedFixture(t)
	seedBulkCaptureModels(t, f, 3)
	itemID := f.item(t)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `DELETE FROM item_projection_state WHERE workspace_id=$1 AND item_id=$2`, f.workspaceID, itemID)
	require.NoError(t, err)
	var pending int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1 AND item_id=$2`, f.workspaceID, itemID).Scan(&pending))
	require.Zero(t, pending)
	_, err = conn.Exec(t.Context(), `INSERT INTO retrieval_projection_pending(workspace_id,item_id,embedding_model_id,revision,updated_at) VALUES($1,$2,$3,$4,$5)`, f.workspaceID, itemID, domain.DefaultEmbeddingModelID, uuid.New(), f.now)
	require.Error(t, err)
}
