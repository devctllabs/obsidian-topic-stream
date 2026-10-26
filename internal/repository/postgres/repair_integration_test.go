//go:build integration

package postgres_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestGlobalRepairAdvancesPastActiveAndStalePendingKeys(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	var ids []uuid.UUID
	for range 7 {
		ids = append(ids, f.item(t))
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `INSERT INTO jobs(id,workspace_id,kind,queue,idempotency_key)
SELECT item_id,workspace_id,'index-item','retrieval','index-item:'||item_id::text||':'||revision::text
FROM item_projection_state WHERE item_id=ANY($1::uuid[])`, ids[:2])
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE retrieval_projection_pending SET revision=$2 WHERE item_id=$1`, ids[2], uuid.New())
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `DELETE FROM item_projection_state WHERE item_id=$1`, ids[4])
	require.NoError(t, err)
	query := domain.ProjectionRepairQuery{Limit: 2}
	page, err := f.store.ListPendingGlobalItemProjections(t.Context(), query)
	require.NoError(t, err)
	require.Empty(t, page.Commits)
	require.NotNil(t, page.NextCursor, "active Jobs must not prevent inspection progress")
	require.Equal(t, ids[1], page.NextCursor.ItemID)
	var recovered []uuid.UUID
	for page.NextCursor != nil {
		query.After = page.NextCursor
		page, err = f.store.ListPendingGlobalItemProjections(t.Context(), query)
		require.NoError(t, err)
		for _, commit := range page.Commits {
			recovered = append(recovered, commit.ItemID)
		}
	}
	require.Equal(t, []uuid.UUID{ids[3], ids[5], ids[6]}, recovered)
}

func TestGlobalRepairSkipsStaleModelGenerationBeforeEnqueue(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	itemID := f.item(t)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `UPDATE retrieval_projection_pending SET model_generation=$2 WHERE item_id=$1`, itemID, uuid.New())
	require.NoError(t, err)
	page, err := f.store.ListPendingGlobalItemProjections(t.Context(), domain.ProjectionRepairQuery{Limit: 100})
	require.NoError(t, err)
	require.Empty(t, page.Commits, "a stale destination generation must not create delivery Jobs")
	require.NotNil(t, page.NextCursor, "stale keys still advance bounded inspection")
}
