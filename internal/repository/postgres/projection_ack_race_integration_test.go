//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type acknowledgementGateKey struct{}
type acknowledgementGate struct{ arrived, release chan struct{} }

// TraceQueryStart identifies acknowledgement's real shared-head lock.
func (*acknowledgementGate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, query pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, acknowledgementGateKey{}, strings.HasPrefix(query.SQL, "SELECT head_ordinal FROM feed_state"))
}

// TraceQueryEnd holds the granted lock until cancellation or explicit cleanup.
func (gate *acknowledgementGate) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, result pgx.TraceQueryEndData) {
	if ctx.Value(acknowledgementGateKey{}) == true && result.Err == nil {
		close(gate.arrived)
		select {
		case <-gate.release:
		case <-ctx.Done():
		}
	}
}

func TestCancelledProjectionBatchReleasesHeadAndPreservesRacingCanonicalChange(t *testing.T) {
	f, entries := newProjectionAckFixture(t, 2)
	gate := &acknowledgementGate{arrived: make(chan struct{}), release: make(chan struct{})}
	traced, err := postgres.OpenWithTracerForTest(t.Context(), f.dsn, gate)
	require.NoError(t, err)
	defer traced.Close()
	defer close(gate.release)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type outcome struct {
		accepted []bool
		err      error
	}
	completed := make(chan outcome, 1)
	go func() {
		accepted, err := traced.CompleteItemProjections(ctx, entries)
		completed <- outcome{accepted, err}
	}()
	testutil.Receive(t, gate.arrived)
	changed := make(chan error, 1)
	go func() {
		_, err := f.store.SetVisibility(t.Context(), f.workspaceID, entries[0].ItemID, domain.ItemVisibilityHidden)
		changed <- err
	}()
	before, found, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, entries[0].ItemID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, entries[0].Revision, before.Projection.Revision)
	cancel()
	result := testutil.Receive(t, completed)
	require.ErrorIs(t, result.err, context.Canceled)
	require.Empty(t, result.accepted)
	require.NoError(t, testutil.Receive(t, changed))
	after, found, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, entries[0].ItemID)
	require.NoError(t, err)
	require.True(t, found)
	require.NotEqual(t, entries[0].Revision, after.Projection.Revision)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	var pending, exact int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*),count(*) FILTER(WHERE pending.revision=state.revision)
 FROM retrieval_projection_pending pending JOIN item_projection_state state USING(workspace_id,item_id) WHERE pending.workspace_id=$1`, f.workspaceID).Scan(&pending, &exact))
	require.Equal(t, 2, pending, "cancelled ACK rolls back every entry")
	require.Equal(t, pending, exact, "canonical change keeps its latest pending revision after the ACK releases coordination")
}

func TestProjectionBatchRechecksAttemptAfterAcquiringSharedHead(t *testing.T) {
	f, entries := newProjectionAckFixture(t, 2)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	claim, err := f.store.Jobs().Claim(t.Context(), domain.JobClaimRequest{Queue: domain.JobQueueMaintenance, Owner: "reused-owner", Now: f.now, LeaseExpiresAt: f.now.Add(time.Minute)})
	require.NoError(t, err)
	for i := range entries {
		entries[i].Execution = domain.ProjectionExecution{JobID: jobID, LeaseOwner: "reused-owner", Attempt: claim.Job.Attempt}
	}
	gate := &acknowledgementGate{arrived: make(chan struct{}), release: make(chan struct{})}
	traced, err := postgres.OpenWithTracerForTest(t.Context(), f.dsn, gate)
	require.NoError(t, err)
	defer traced.Close()
	var once sync.Once
	release := func() { once.Do(func() { close(gate.release) }) }
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type outcome struct {
		accepted []bool
		err      error
	}
	completed := make(chan outcome, 1)
	go func() {
		accepted, err := traced.CompleteItemProjections(ctx, entries)
		completed <- outcome{accepted, err}
	}()
	testutil.Receive(t, gate.arrived)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET attempt=attempt+1,lease_expires_at=clock_timestamp()+interval '1 minute' WHERE id=$1`, jobID)
	require.NoError(t, err)
	release()
	result := testutil.Receive(t, completed)
	require.NoError(t, result.err)
	require.Equal(t, []bool{false, false}, result.accepted, "a newly current attempt invalidates every earlier token before DELETE")
	for i := range entries {
		entries[i].Execution.Attempt++
	}
	accepted, err := f.store.CompleteItemProjections(t.Context(), entries)
	require.NoError(t, err)
	require.Equal(t, []bool{true, true}, accepted)
}
