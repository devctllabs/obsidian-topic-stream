//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	repository "github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestJobRecoverySkipsLockedRowsAndBoundsEachBatch(t *testing.T) {
	f := newFeedFixture(t)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `INSERT INTO jobs
(id,workspace_id,kind,queue,status,attempt,max_attempts,lease_owner,lease_expires_at)
SELECT md5(n::text)::uuid,$1,'expired','maintenance','running',1,1,'lost',$2::timestamptz - interval '1 minute'
FROM generate_series(1,202) n`, f.workspaceID, f.now)
	require.NoError(t, err)
	lockedID := uuid.MustParse("c4ca4238-a0b9-2382-0dcc-509a6f75849b") // md5('1')
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(t.Context(), `SELECT id FROM jobs WHERE id=$1 FOR UPDATE`, lockedID)
	require.NoError(t, err)
	request := domain.JobClaimRequest{Queue: domain.JobQueueMaintenance, Owner: "recovery", Now: f.now, LeaseExpiresAt: f.now.Add(time.Minute)}
	// The lock remains held throughout Claim; the deadline only prevents a broken query hanging.
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	claim, err := f.store.Jobs().Claim(ctx, request)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, claim.Job.ID)
	require.Len(t, claim.ChangedJobs, 100)
	for _, identity := range claim.ChangedJobs {
		require.NotEqual(t, lockedID, identity.ID)
	}
	claim, err = f.store.Jobs().Claim(t.Context(), request)
	require.NoError(t, err)
	require.Len(t, claim.ChangedJobs, 100)
	require.NoError(t, tx.Rollback(t.Context()))
	claim, err = f.store.Jobs().Claim(t.Context(), request)
	require.NoError(t, err)
	require.Len(t, claim.ChangedJobs, 2)
	claim, err = f.store.Jobs().Claim(t.Context(), request)
	require.NoError(t, err)
	require.Empty(t, claim.ChangedJobs)
}

func TestStoreConcurrentClaimsAreUniqueAndQueueScoped(t *testing.T) {
	store, err := repository.Open(t.Context(), testutil.MigratedPostgresSchema(t))
	require.NoError(t, err)
	t.Cleanup(store.Close)
	queues := []domain.JobQueue{domain.JobQueueFetch, domain.JobQueueRetrieval, domain.JobQueueMaintenance}
	expected := seedConcurrentJobs(t, store, queues)
	now := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	type result struct {
		queue domain.JobQueue
		claim domain.JobClaim
		err   error
	}
	const workersPerQueue = 6
	results := make(chan result, len(queues)*workersPerQueue)
	start := make(chan struct{})
	for _, queue := range queues {
		for worker := range workersPerQueue {
			go func() {
				<-start
				claim, err := store.Jobs().Claim(t.Context(), domain.JobClaimRequest{
					Queue: queue, Owner: fmt.Sprintf("%s-worker-%d", queue, worker),
					Now: now, LeaseExpiresAt: now.Add(time.Minute),
				})
				results <- result{queue: queue, claim: claim, err: err}
			}()
		}
	}
	close(start)
	seen := make(map[uuid.UUID]bool)
	for range cap(results) {
		result := testutil.Receive(t, results)
		require.NoError(t, result.err)
		job := result.claim.Job
		if job.ID == uuid.Nil {
			continue
		}
		require.Contains(t, expected, job.ID)
		require.NotContains(t, seen, job.ID, "a Job must never be leased to two workers")
		require.Equal(t, result.queue, job.Queue)
		require.Equal(t, expected[job.ID].WorkspaceID, job.WorkspaceID)
		require.Equal(t, 1, job.Attempt)
		seen[job.ID] = true
	}
	require.Len(t, seen, len(expected))
	for _, queue := range queues {
		claim, err := store.Jobs().Claim(t.Context(), domain.JobClaimRequest{
			Queue: queue, Owner: "late-worker", Now: now, LeaseExpiresAt: now.Add(time.Minute),
		})
		require.NoError(t, err)
		require.Equal(t, uuid.Nil, claim.Job.ID, "leased Jobs must remain unavailable")
	}
}

func seedConcurrentJobs(t *testing.T, store *repository.Store, queues []domain.JobQueue) map[uuid.UUID]domain.Job {
	t.Helper()
	now := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	jobs := make(map[uuid.UUID]domain.Job)
	for range 2 {
		workspaceID := uuid.New()
		require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
		for _, queue := range queues {
			for range 2 {
				job := domain.Job{
					ID: uuid.New(), WorkspaceID: workspaceID, Kind: "integration", Queue: queue,
					ArgumentsVersion: 1, Arguments: json.RawMessage(`{}`),
					Status: domain.JobStatusQueued, MaxAttempts: 3,
					AvailableAt: now, CreatedAt: now, UpdatedAt: now,
				}
				_, created, err := store.Jobs().Enqueue(t.Context(), job)
				require.NoError(t, err)
				require.True(t, created)
				jobs[job.ID] = job
			}
		}
	}
	return jobs
}
