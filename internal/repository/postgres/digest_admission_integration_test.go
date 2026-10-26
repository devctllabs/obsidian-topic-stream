//go:build integration

package postgres_test

import (
	"sync"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestManualDigestAdmissionCoalescesConcurrentIdentitiesAndRetainsTerminalBindings(t *testing.T) {
	f := newFeedFixture(t)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Manual only", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	const callers = 16
	requests := make([]domain.DigestBuildRequest, callers)
	for i := range requests {
		identity := uuid.New()
		if i > 0 && i%2 == 0 {
			identity = requests[0].RequestID
		}
		requests[i] = manualDigestRequest(t, definition, identity)
	}
	start := make(chan struct{})
	results := make([]domain.DigestBuildAdmission, callers)
	errors := make([]error, callers)
	var wg sync.WaitGroup
	for i := range requests {
		wg.Go(func() { <-start; results[i], errors[i] = f.store.AdmitDigestBuild(t.Context(), requests[i]) })
	}
	close(start)
	wg.Wait()
	created := 0
	for i, result := range results {
		require.NoError(t, errors[i])
		require.Equal(t, results[0].Job, result.Job)
		if result.Created {
			created++
		}
	}
	require.Equal(t, 1, created)
	saved, found, err := f.store.GetDigest(t.Context(), f.workspaceID, definition.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, saved.Enabled)
	require.Nil(t, saved.NextRunAt)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	for _, status := range []domain.JobStatus{domain.JobStatusRunning, domain.JobStatusRetryWait} {
		_, err = conn.Exec(t.Context(), `UPDATE jobs SET status=$2,cancellation_requested=true,attempt=1 WHERE id=$1`, results[0].Job.ID, status)
		require.NoError(t, err)
		active, err := f.store.AdmitDigestBuild(t.Context(), manualDigestRequest(t, definition, uuid.New()))
		require.NoError(t, err)
		require.False(t, active.Created)
		require.Equal(t, results[0].Job.ID, active.Job.ID)
	}
	// A fresh graph must replay each identity after every terminal outcome.
	fresh, err := postgres.Open(t.Context(), f.dsn)
	require.NoError(t, err)
	defer fresh.Close()
	for _, status := range []domain.JobStatus{domain.JobStatusSucceeded, domain.JobStatusDead, domain.JobStatusCancelled} {
		_, err = conn.Exec(t.Context(), `UPDATE jobs SET status=$2 WHERE id=$1`, results[0].Job.ID, status)
		require.NoError(t, err)
		for _, request := range requests {
			replayed, err := fresh.AdmitDigestBuild(t.Context(), request)
			require.NoError(t, err)
			require.False(t, replayed.Created)
			require.Equal(t, results[0].Job.ID, replayed.Job.ID)
			require.Equal(t, status, replayed.Job.Status)
		}
	}
	next, err := fresh.AdmitDigestBuild(t.Context(), manualDigestRequest(t, definition, uuid.New()))
	require.NoError(t, err)
	require.True(t, next.Created)
	require.NotEqual(t, results[0].Job.ID, next.Job.ID)
	deleted, err := fresh.DeleteDigest(t.Context(), domain.DigestDelete{WorkspaceID: f.workspaceID, DigestID: definition.ID, ExpectedVersion: 1, DeletedAt: f.now})
	require.NoError(t, err)
	require.True(t, deleted.Deleted)
	replay, err := fresh.AdmitDigestBuild(t.Context(), requests[0])
	require.NoError(t, err)
	require.Equal(t, results[0].Job.ID, replay.Job.ID)
	_, err = fresh.AdmitDigestBuild(t.Context(), manualDigestRequest(t, definition, uuid.New()))
	require.ErrorIs(t, err, domain.ErrDigestNotFound)
}

func TestManualDigestAdmissionScopesOwnershipAndRollsBackFailedBinding(t *testing.T) {
	f := newFeedFixture(t)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "One", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	identity := uuid.New()
	request := manualDigestRequest(t, definition, identity)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `ALTER TABLE digest_build_requests ADD CONSTRAINT fail_binding CHECK(request_kind <> 'manual')`)
	require.NoError(t, err)
	_, err = f.store.AdmitDigestBuild(t.Context(), request)
	require.Error(t, err)
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE id=$1`, request.Job.ID).Scan(&count))
	require.Zero(t, count)
	_, err = conn.Exec(t.Context(), `ALTER TABLE digest_build_requests DROP CONSTRAINT fail_binding`)
	require.NoError(t, err)
	first, err := f.store.AdmitDigestBuild(t.Context(), request)
	require.NoError(t, err)
	require.True(t, first.Created)
	otherWS := uuid.New()
	require.NoError(t, f.store.EnsureWorkspace(t.Context(), otherWS))
	foreign := request
	foreign.WorkspaceID = otherWS
	foreign.Job.WorkspaceID = otherWS
	_, err = f.store.AdmitDigestBuild(t.Context(), foreign)
	require.ErrorIs(t, err, domain.ErrDigestNotFound)
	for _, workspaceID := range []uuid.UUID{f.workspaceID, otherWS} {
		other := definition
		other.ID, other.WorkspaceID = uuid.New(), workspaceID
		require.NoError(t, f.store.CreateDigest(t.Context(), other))
		result, err := f.store.AdmitDigestBuild(t.Context(), manualDigestRequest(t, other, identity))
		require.NoError(t, err)
		require.True(t, result.Created)
		require.NotEqual(t, first.Job.ID, result.Job.ID)
	}
	invalid := manualDigestRequest(t, definition, uuid.New())
	invalid.Job.WorkspaceID = otherWS
	_, err = f.store.AdmitDigestBuild(t.Context(), invalid)
	require.ErrorIs(t, err, domain.ErrConflict)
}

// manualDigestRequest prepares an independent attempt candidate with the shared queue policy.
func manualDigestRequest(t *testing.T, definition domain.DigestDefinition, identity uuid.UUID) domain.DigestBuildRequest {
	t.Helper()
	job, err := jobs.Prepare(jobs.EnqueueRequest{WorkspaceID: definition.WorkspaceID,
		Definition: jobs.JobDefinition{Kind: "generate-digest", ArgumentsVersion: 1, Queue: domain.JobQueueGeneration,
			Retry: jobs.RetryPolicy{MaxAttempts: 3, BaseDelay: 30 * time.Second, MaxDelay: 15 * time.Minute}},
		Arguments: map[string]any{"digestId": definition.ID, "scheduledFor": nil}, IdempotencyKey: "generate-digest:" + definition.ID.String(),
	}, time.Now().UTC())
	require.NoError(t, err)
	return domain.DigestBuildRequest{WorkspaceID: definition.WorkspaceID, DigestID: definition.ID, RequestID: identity, Kind: domain.DigestBuildManual, Job: job}
}
