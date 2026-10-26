//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type cohortCaptureKey struct{}
type cohortCaptureGate struct{ arrived, release chan struct{} }

// TraceQueryStart identifies the real cohort capture statement.
func (*cohortCaptureGate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, query pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, cohortCaptureKey{}, strings.HasPrefix(query.SQL, "INSERT INTO retrieval_rebuild_cohort"))
}

// TraceQueryEnd pauses captured membership before its transaction becomes visible to ACKs.
func (g *cohortCaptureGate) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, result pgx.TraceQueryEndData) {
	if ctx.Value(cohortCaptureKey{}) == true && result.Err == nil {
		close(g.arrived)
		select {
		case <-g.release:
		case <-ctx.Done():
		}
	}
}

func TestReindexCohortCaptureSerializesRacingServingACK(t *testing.T) {
	f, entries := newProjectionAckFixture(t, 1)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	gate := &cohortCaptureGate{arrived: make(chan struct{}), release: make(chan struct{})}
	traced, err := postgres.OpenWithTracerForTest(t.Context(), f.dsn, gate)
	require.NoError(t, err)
	defer traced.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started := make(chan error, 1)
	go func() {
		_, beginErr := traced.Begin(ctx, domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID, JobID: jobID, StartedAt: f.now})
		started <- beginErr
	}()
	testutil.Receive(t, gate.arrived)
	finished := make(chan error, 1)
	go func() { _, ackErr := f.store.CompleteItemProjections(ctx, entries); finished <- ackErr }()
	early := false
	var ackErr error
	select {
	case ackErr = <-finished:
		early = true
	case <-time.After(100 * time.Millisecond):
	}
	close(gate.release)
	require.NoError(t, testutil.Receive(t, started))
	if !early {
		ackErr = testutil.Receive(t, finished)
	}
	require.NoError(t, ackErr)
	require.False(t, early, "an ACK cannot delete captured pending work before its cohort is visible")
	job, _, err := f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.EqualValues(t, 1, job.Progress.ProcessedItems)
}

func TestReindexProgressCountsFixedCohortOnlyAfterAcceptedACK(t *testing.T) {
	f, entries := newProjectionAckFixture(t, 3)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID, JobID: jobID, StartedAt: f.now}
	_, err := f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	job, found, err := f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, job.Progress)
	require.EqualValues(t, 3, *job.Progress.TotalItems)
	require.Zero(t, job.Progress.ProcessedItems)
	require.Equal(t, "indexing", job.Progress.Phase, "retained initialization is already complete")
	newItem := f.item(t)
	work, found, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, newItem)
	require.NoError(t, err)
	require.True(t, found)
	newEntry := domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: newItem, ModelID: domain.DefaultEmbeddingModelID, Revision: work.Projection.Revision}
	accepted, err := f.store.CompleteItemProjections(t.Context(), []domain.ItemProjectionDelivery{entries[0], newEntry})
	require.NoError(t, err)
	require.Equal(t, []bool{true, true}, accepted)
	job, _, err = f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.EqualValues(t, 3, *job.Progress.TotalItems, "new Items do not increase the frozen cohort")
	require.EqualValues(t, 1, job.Progress.ProcessedItems, "new Item ACK is not initial cohort work")
	stale := entries[1]
	stale.Revision = uuid.New()
	accepted, err = f.store.CompleteItemProjections(t.Context(), []domain.ItemProjectionDelivery{entries[0], stale})
	require.NoError(t, err)
	require.Equal(t, []bool{false, false}, accepted)
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	job, _, err = f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.EqualValues(t, 1, job.Progress.ProcessedItems)
	accepted, err = f.store.CompleteItemProjections(t.Context(), entries[1:])
	require.NoError(t, err)
	require.Equal(t, []bool{true, true}, accepted)
	job, _, err = f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.EqualValues(t, 3, job.Progress.ProcessedItems)
	require.Equal(t, "catching_up", job.Progress.Phase)
	cutover := domain.ModelTransitionCutover{WorkspaceID: f.workspaceID, ModelID: start.ModelID, JobID: jobID, ActivatedAt: f.now.Add(time.Second)}
	staleCutover := cutover
	staleCutover.JobID = uuid.New()
	require.ErrorIs(t, f.store.PrepareActivation(t.Context(), staleCutover), domain.ErrConflict)
	require.NoError(t, f.store.PrepareActivation(t.Context(), cutover))
	job, _, err = f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.Equal(t, "activating", job.Progress.Phase)
	require.Equal(t, domain.JobStatusQueued, job.Status, "phase does not claim overall success")
	require.NoError(t, f.store.Activate(t.Context(), cutover))
	job, _, err = f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.Equal(t, "completed", job.Progress.Phase)
}

func unseededProgressFixture(t *testing.T, count int) (feedFixture, domain.ModelTransitionStart, *pgx.Conn) {
	t.Helper()
	f, _ := newProjectionAckFixture(t, 0)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(t.Context())) })
	_, err = conn.Exec(t.Context(), `INSERT INTO items(id,workspace_id,details,title,url,canonical_url)
SELECT md5(n::text)::uuid,$1,'{"type":"article","article":{}}','Synthetic','https://fixture.example/'||n,'https://fixture.example/'||n FROM generate_series(1,$2::int) n`, f.workspaceID, count)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO item_projection_state(workspace_id,item_id,revision,updated_at)
SELECT workspace_id,id,$2,$3 FROM items WHERE workspace_id=$1`, f.workspaceID, uuid.New(), f.now)
	require.NoError(t, err)
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID,
		JobID: enqueueTransitionJob(t, f.store, f.workspaceID, f.now), StartedAt: f.now, Force: true}
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	return f, start, conn
}

func TestReindexProgressResumedInitialSeedExcludesPreviouslyAcknowledgedItems(t *testing.T) {
	f, start, _ := unseededProgressFixture(t, 2001)
	job, _, err := f.store.Jobs().Get(t.Context(), start.JobID)
	require.NoError(t, err)
	require.EqualValues(t, 2001, *job.Progress.TotalItems)
	require.Equal(t, "preparing", job.Progress.Phase)
	commits, err := f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: f.workspaceID, ModelID: start.ModelID, Limit: 2})
	require.NoError(t, err)
	require.Len(t, commits, 2)
	for _, commit := range commits {
		work, _, loadErr := f.store.LoadItemProjection(t.Context(), f.workspaceID, commit.ItemID)
		require.NoError(t, loadErr)
		delivery := domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: commit.ItemID, ModelID: start.ModelID,
			Revision: work.Projection.Revision, ModelGeneration: work.Models[0].Generation}
		accepted, completeErr := f.store.CompleteItemProjection(t.Context(), delivery)
		require.NoError(t, completeErr)
		require.True(t, accepted)
	}
	require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{WorkspaceID: f.workspaceID, ModelID: start.ModelID, JobID: start.JobID, FailedAt: f.now}))
	start.JobID = enqueueTransitionJob(t, f.store, f.workspaceID, f.now.Add(time.Second))
	start.Force = false
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	job, _, err = f.store.Jobs().Get(t.Context(), start.JobID)
	require.NoError(t, err)
	require.EqualValues(t, 1999, *job.Progress.TotalItems, "998 seeded pending plus 1001 unseeded; completed prefix is excluded")
}

func TestReindexProgressRetiredCohortsAreCleanedInBoundedMaintenanceBatches(t *testing.T) {
	f, start, conn := unseededProgressFixture(t, 2001)
	_, err := conn.Exec(t.Context(), `UPDATE jobs SET status='cancelled',cancellation_requested=true WHERE id=$1`, start.JobID)
	require.NoError(t, err)
	for _, remaining := range []int{1001, 1, 0} {
		_, err = f.store.ReconcileModelStates(t.Context(), domain.ModelReconciliationQuery{Limit: 1})
		require.NoError(t, err)
		var count int
		require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_rebuild_cohort WHERE job_id=$1`, start.JobID).Scan(&count))
		require.Equal(t, remaining, count)
	}
	job, _, err := f.store.Jobs().Get(t.Context(), start.JobID)
	require.NoError(t, err)
	require.EqualValues(t, 2001, *job.Progress.TotalItems)
	require.Zero(t, job.Progress.ProcessedItems, "cleanup preserves historical counters")
}

func TestReindexProgressCountsConcurrentACKsOnceAndRepeatedChangesRemainCatchUp(t *testing.T) {
	f, entries := newProjectionAckFixture(t, 20)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	_, err := f.store.Begin(t.Context(), domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID, JobID: jobID, StartedAt: f.now})
	require.NoError(t, err)
	type outcome struct {
		accepted []bool
		err      error
	}
	finished := make(chan outcome, 2)
	for range 2 {
		go func() {
			accepted, completeErr := f.store.CompleteItemProjections(t.Context(), entries)
			finished <- outcome{accepted, completeErr}
		}()
	}
	acceptedCount := 0
	for range 2 {
		result := <-finished
		require.NoError(t, result.err)
		for _, accepted := range result.accepted {
			if accepted {
				acceptedCount++
			}
		}
	}
	require.Equal(t, 20, acceptedCount)
	job, _, err := f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.EqualValues(t, 20, job.Progress.ProcessedItems)
	_, err = f.store.SetVisibility(t.Context(), f.workspaceID, entries[0].ItemID, domain.ItemVisibilityHidden)
	require.NoError(t, err)
	updated := entries[0]
	work, found, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, updated.ItemID)
	require.NoError(t, err)
	require.True(t, found)
	updated.Revision = work.Projection.Revision
	accepted, err := f.store.CompleteItemProjections(t.Context(), []domain.ItemProjectionDelivery{updated})
	require.NoError(t, err)
	require.Equal(t, []bool{true}, accepted)
	job, _, err = f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.EqualValues(t, 20, job.Progress.ProcessedItems, "changes after cohort completion are not counted twice")
}

func TestReindexProgressRetainedReturnCapturesOnlyRemainingItemsAndPreservesCancelledHistory(t *testing.T) {
	f, entries := newProjectionAckFixture(t, 3)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID, JobID: jobID, StartedAt: f.now}
	_, err := f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	_, err = f.store.CompleteItemProjections(t.Context(), entries[:1])
	require.NoError(t, err)
	require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{WorkspaceID: f.workspaceID, ModelID: start.ModelID, JobID: jobID, FailedAt: f.now}))
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET status='cancelled',cancellation_requested=true WHERE id=$1`, jobID)
	require.NoError(t, err)
	start.JobID = enqueueTransitionJob(t, f.store, f.workspaceID, f.now.Add(time.Second))
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	job, _, err := f.store.Jobs().Get(t.Context(), start.JobID)
	require.NoError(t, err)
	require.EqualValues(t, 2, *job.Progress.TotalItems)
	_, err = f.store.CompleteItemProjections(t.Context(), entries[1:])
	require.NoError(t, err)
	job, _, err = f.store.Jobs().Get(t.Context(), start.JobID)
	require.NoError(t, err)
	require.EqualValues(t, 2, job.Progress.ProcessedItems)
	oldJob, _, err := f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.EqualValues(t, 3, *oldJob.Progress.TotalItems)
	require.EqualValues(t, 1, oldJob.Progress.ProcessedItems, "later work cannot rewrite a cancelled operation's history")
}

func TestReindexProgressEmptyCohortAndRetryAfterActivationPreparation(t *testing.T) {
	f, _ := newProjectionAckFixture(t, 0)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	job, _, err := f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.NotNil(t, job.Progress)
	require.Nil(t, job.Progress.TotalItems)
	require.Equal(t, "preparing", job.Progress.Phase)
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID, JobID: jobID, StartedAt: f.now}
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	cutover := domain.ModelTransitionCutover{WorkspaceID: f.workspaceID, ModelID: start.ModelID, JobID: jobID, ActivatedAt: f.now}
	require.NoError(t, f.store.PrepareActivation(t.Context(), cutover))
	require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{WorkspaceID: f.workspaceID, ModelID: start.ModelID, JobID: jobID, FailedAt: f.now}))
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	job, _, err = f.store.Jobs().Get(t.Context(), jobID)
	require.NoError(t, err)
	require.Zero(t, *job.Progress.TotalItems)
	require.Equal(t, "catching_up", job.Progress.Phase, "retry restores a phase that describes its remaining work")
}
