//go:build integration

package postgres_test

import (
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestProjectionAcknowledgementRequiresCurrentJobLease(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	itemID := f.item(t)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	claim, err := f.store.Jobs().Claim(t.Context(), domain.JobClaimRequest{Queue: domain.JobQueueMaintenance,
		Owner: "old-worker", Now: f.now, LeaseExpiresAt: f.now.Add(time.Minute)})
	require.NoError(t, err)
	work, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	delivery := domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID,
		ModelID: domain.DefaultEmbeddingModelID, Revision: work.Projection.Revision,
		Execution: domain.ProjectionExecution{JobID: jobID, LeaseOwner: "old-worker", Attempt: claim.Job.Attempt}}
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	for _, mutation := range []string{
		`UPDATE jobs SET lease_owner='new-worker' WHERE id=$1`,
		`UPDATE jobs SET lease_owner='old-worker',lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`,
		`UPDATE jobs SET lease_expires_at=clock_timestamp()+interval '1 minute',cancellation_requested=true WHERE id=$1`,
	} {
		_, err = conn.Exec(t.Context(), mutation, jobID)
		require.NoError(t, err)
		completed, err := f.store.CompleteItemProjection(t.Context(), delivery)
		require.NoError(t, err)
		require.False(t, completed, "replaced, expired and cancelled executions cannot acknowledge work")
	}
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET cancellation_requested=false WHERE id=$1`, jobID)
	require.NoError(t, err)
	completed, err := f.store.CompleteItemProjection(t.Context(), delivery)
	require.NoError(t, err)
	require.True(t, completed)
}

func TestProjectionAcknowledgementRejectsEarlierAttemptWithSameOwner(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	itemID := f.item(t)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	claim, err := f.store.Jobs().Claim(t.Context(), domain.JobClaimRequest{Queue: domain.JobQueueMaintenance,
		Owner: "reused-owner", Now: f.now, LeaseExpiresAt: f.now.Add(time.Minute)})
	require.NoError(t, err)
	require.Equal(t, jobID, claim.Job.ID)
	work, found, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, found)
	delivery := domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID,
		ModelID: domain.DefaultEmbeddingModelID, Revision: work.Projection.Revision,
		Execution: domain.ProjectionExecution{JobID: jobID, LeaseOwner: "reused-owner", Attempt: claim.Job.Attempt}}
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET attempt=attempt+1,lease_expires_at=clock_timestamp()+interval '1 minute' WHERE id=$1`, jobID)
	require.NoError(t, err)
	accepted, err := f.store.CompleteItemProjection(t.Context(), delivery)
	require.NoError(t, err)
	require.False(t, accepted, "an earlier attempt cannot acknowledge a later attempt's work under the same lease owner")
	delivery.Execution.Attempt++
	accepted, err = f.store.CompleteItemProjection(t.Context(), delivery)
	require.NoError(t, err)
	require.True(t, accepted, "the current attempt still acknowledges the exact pending revision")
}

func TestPendingModelProjectionsSkipStaleRevisionsAndFillOrderedBatch(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	modelID := domain.DefaultEmbeddingModelID
	for range 6 {
		f.item(t)
	}
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `INSERT INTO retrieval_projection_pending(workspace_id,item_id,embedding_model_id,revision,updated_at)
SELECT workspace_id,item_id,$2,revision,updated_at FROM item_projection_state WHERE workspace_id=$1`, f.workspaceID, modelID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE retrieval_projection_pending SET revision=$3 WHERE workspace_id=$1 AND embedding_model_id=$2 AND item_id IN
(SELECT item_id FROM item_projection_state WHERE workspace_id=$1 ORDER BY item_id LIMIT 2)`, f.workspaceID, modelID, uuid.New())
	require.NoError(t, err)
	rows, err := conn.Query(t.Context(), `SELECT item_id FROM item_projection_state WHERE workspace_id=$1 ORDER BY item_id OFFSET 2`, f.workspaceID)
	require.NoError(t, err)
	expected, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	require.NoError(t, err)
	pending, err := f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: f.workspaceID, ModelID: modelID, Limit: 2})
	require.NoError(t, err)
	require.Len(t, pending, 2)
	for index, commit := range pending {
		require.Equal(t, expected[index], commit.ItemID)
		var revision uuid.UUID
		require.NoError(t, conn.QueryRow(t.Context(), `SELECT revision FROM item_projection_state WHERE workspace_id=$1 AND item_id=$2`, f.workspaceID, commit.ItemID).Scan(&revision))
		require.Equal(t, revision, commit.ProjectionRevision)
	}
	after := pending[len(pending)-1].ItemID
	next, err := f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: f.workspaceID, ModelID: modelID, AfterItemID: &after, Limit: 2})
	require.NoError(t, err)
	require.Len(t, next, 2)
	require.Equal(t, expected[2], next[0].ItemID)
	require.Equal(t, expected[3], next[1].ItemID)
	empty, err := f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: f.workspaceID, ModelID: uuid.New(), Limit: 2})
	require.NoError(t, err)
	require.Empty(t, empty)
}
