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

func TestProjectionBatchCompletesOnlyExactRevisionsAndRequestsServingPreparation(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	var deliveries []domain.ItemProjectionDelivery
	for range 3 {
		itemID := f.item(t)
		work, found, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
		require.NoError(t, err)
		require.True(t, found)
		deliveries = append(deliveries, domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: domain.DefaultEmbeddingModelID, Revision: work.Projection.Revision})
	}
	deliveries[1].Revision = uuid.New()
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	accepted, err := f.store.CompleteItemProjections(t.Context(), deliveries)
	require.NoError(t, err)
	require.Equal(t, []bool{true, false, true}, accepted)
	var pending, requests int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1`, f.workspaceID).Scan(&pending))
	require.Equal(t, 1, pending, "stale input leaves the exact current work pending")
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM feed_preparation_requests WHERE workspace_id=$1`, f.workspaceID).Scan(&requests))
	require.Equal(t, 1, requests, "accepted Serving entries coalesce one durable preparation request")
	accepted, err = f.store.CompleteItemProjections(t.Context(), deliveries)
	require.NoError(t, err)
	require.Equal(t, []bool{false, false, false}, accepted, "a lost-ACK retry cannot remove another revision")
}

func newProjectionAckFixture(t *testing.T, count int) (feedFixture, []domain.ItemProjectionDelivery) {
	t.Helper()
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	var entries []domain.ItemProjectionDelivery
	for range count {
		itemID := f.item(t)
		work, found, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
		require.NoError(t, err)
		require.True(t, found)
		entries = append(entries, domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: domain.DefaultEmbeddingModelID, Revision: work.Projection.Revision})
	}
	return f, entries
}

func TestProjectionBatchRejectsInvalidShapeWithoutRemovingWork(t *testing.T) {
	f, entries := newProjectionAckFixture(t, 1)
	accepted, err := f.store.CompleteItemProjections(t.Context(), nil)
	require.NoError(t, err)
	require.Empty(t, accepted)
	foreign := entries[0]
	foreign.WorkspaceID = uuid.New()
	tooMany := make([]domain.ItemProjectionDelivery, 1001)
	for i := range tooMany {
		tooMany[i] = entries[0]
	}
	for _, invalid := range [][]domain.ItemProjectionDelivery{tooMany, {entries[0], entries[0]}, {entries[0], foreign}} {
		accepted, err = f.store.CompleteItemProjections(t.Context(), invalid)
		require.Error(t, err)
		require.Empty(t, accepted)
	}
	accepted, err = f.store.CompleteItemProjections(t.Context(), entries)
	require.NoError(t, err)
	require.Equal(t, []bool{true}, accepted, "invalid batches leave the valid pending revision untouched")
}

func TestProjectionBatchFencesEachJobAttemptWithReusedOwner(t *testing.T) {
	f, entries := newProjectionAckFixture(t, 2)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	claim, err := f.store.Jobs().Claim(t.Context(), domain.JobClaimRequest{Queue: domain.JobQueueMaintenance, Owner: "same-owner", Now: f.now, LeaseExpiresAt: f.now.Add(time.Minute)})
	require.NoError(t, err)
	require.Equal(t, jobID, claim.Job.ID)
	_, err = f.store.Begin(t.Context(), domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID,
		JobID: jobID, LeaseOwner: "same-owner", StartedAt: f.now})
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET attempt=attempt+1 WHERE id=$1`, jobID)
	require.NoError(t, err)
	entries[0].Execution = domain.ProjectionExecution{JobID: jobID, LeaseOwner: "same-owner", Attempt: claim.Job.Attempt}
	entries[1].Execution = entries[0].Execution
	entries[1].Execution.Attempt++
	accepted, err := f.store.CompleteItemProjections(t.Context(), entries)
	require.NoError(t, err)
	require.Equal(t, []bool{false, true}, accepted)
	job, _, err := f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.EqualValues(t, 1, job.Progress.ProcessedItems, "a stale attempt cannot advance the cohort")
	entries[0].Execution.Attempt++
	accepted, err = f.store.CompleteItemProjections(t.Context(), entries[:1])
	require.NoError(t, err)
	require.Equal(t, []bool{true}, accepted)
}

func TestProjectionBatchSQLFailureRollsBackEveryAcknowledgement(t *testing.T) {
	f, entries := newProjectionAckFixture(t, 2)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	_, err := f.store.Begin(t.Context(), domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID, JobID: jobID, StartedAt: f.now})
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `CREATE FUNCTION reject_ack_preparation() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'injected ACK preparation failure'; END $$;
 CREATE TRIGGER reject_ack_preparation BEFORE INSERT ON feed_preparation_requests FOR EACH ROW EXECUTE FUNCTION reject_ack_preparation()`)
	require.NoError(t, err)
	accepted, err := f.store.CompleteItemProjections(t.Context(), entries)
	require.ErrorContains(t, err, "injected ACK preparation failure")
	require.Empty(t, accepted)
	var pending int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1`, f.workspaceID).Scan(&pending))
	require.Equal(t, 2, pending, "all deletes roll back when durable Serving preparation fails")
	job, _, err := f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.Zero(t, job.Progress.ProcessedItems, "cohort deletion and counters roll back with the ACK")
	_, err = conn.Exec(t.Context(), `DROP TRIGGER reject_ack_preparation ON feed_preparation_requests`)
	require.NoError(t, err)
	accepted, err = f.store.CompleteItemProjections(t.Context(), entries)
	require.NoError(t, err)
	require.Equal(t, []bool{true, true}, accepted)
}
