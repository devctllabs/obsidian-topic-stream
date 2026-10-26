//go:build integration

package postgres_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestDeletingDigestCancelsNonterminalBuildsAndPreservesBoundReplays(t *testing.T) {
	for _, status := range []domain.JobStatus{domain.JobStatusQueued, domain.JobStatusRetryWait, domain.JobStatusRunning} {
		t.Run(string(status), func(t *testing.T) {
			f := newFeedFixture(t)
			definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Delete", Enabled: true, Version: 1, Timezone: "UTC", NextRunAt: &f.now, CreatedAt: f.now, UpdatedAt: f.now}
			require.NoError(t, f.store.CreateDigest(t.Context(), definition))
			request := manualDigestRequest(t, definition, uuid.New())
			admission, err := f.store.AdmitDigestBuild(t.Context(), request)
			require.NoError(t, err)
			conn, err := pgx.Connect(t.Context(), f.dsn)
			require.NoError(t, err)
			defer func() { require.NoError(t, conn.Close(t.Context())) }()
			_, err = conn.Exec(t.Context(), `UPDATE jobs SET status=$2 WHERE id=$1`, admission.Job.ID, status)
			require.NoError(t, err)
			deleted, err := f.store.DeleteDigest(t.Context(), domain.DigestDelete{WorkspaceID: f.workspaceID, DigestID: definition.ID, ExpectedVersion: 1, DeletedAt: f.now})
			require.NoError(t, err)
			require.True(t, deleted.Deleted)
			job, found, err := f.store.Jobs().Get(t.Context(), admission.Job.ID)
			require.NoError(t, err)
			require.True(t, found)
			if status == domain.JobStatusRunning {
				require.Equal(t, status, job.Status)
				require.True(t, job.CancellationRequested)
			} else {
				require.Equal(t, domain.JobStatusCancelled, job.Status)
			}
			replay, err := f.store.AdmitDigestBuild(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, job, replay.Job)
			request.RequestID = uuid.New()
			_, err = f.store.AdmitDigestBuild(t.Context(), request)
			require.ErrorIs(t, err, domain.ErrDigestNotFound)
			request.Kind, request.RequestID = domain.DigestBuildRetry, admission.Job.ID
			_, err = f.store.AdmitDigestBuild(t.Context(), request)
			require.ErrorIs(t, err, domain.ErrJobNotRetryable)
			due, err := f.store.ListDueDigests(t.Context(), f.workspaceID, f.now.Add(time.Hour))
			require.NoError(t, err)
			require.Empty(t, due)
			if status != domain.JobStatusRunning {
				claim, err := f.store.Jobs().Claim(t.Context(), domain.JobClaimRequest{Queue: domain.JobQueueGeneration, Owner: "after delete", Now: f.now, LeaseExpiresAt: f.now.Add(time.Minute)})
				require.NoError(t, err)
				require.Equal(t, uuid.Nil, claim.Job.ID)
			}
		})
	}
}

func TestDeletingDigestRacesWithAllAdmissionTriggers(t *testing.T) {
	f := newFeedFixture(t)
	due := f.now.Add(-time.Minute)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Race deletion", Enabled: true, Version: 1, Timezone: "UTC", NextRunAt: &due, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	source := deadDigestJob(t, f, definition)
	manual := manualDigestRequest(t, definition, uuid.New())
	retry := manualDigestRequest(t, definition, source.ID)
	retry.Kind = domain.DigestBuildRetry
	schedule := domain.DigestScheduledBuild{Advance: domain.DigestScheduleAdvance{WorkspaceID: f.workspaceID, DigestID: definition.ID, Expected: due, NextRunAt: f.now.Add(time.Hour), UpdatedAt: f.now}, ExpectedVersion: 1, Job: manualDigestRequest(t, definition, uuid.New()).Job}
	operations := []func() error{
		func() error { _, err := f.store.AdmitDigestBuild(t.Context(), manual); return err },
		func() error { _, err := f.store.AdmitDigestBuild(t.Context(), retry); return err },
		func() error { _, err := f.store.AdmitScheduledDigestBuild(t.Context(), schedule); return err },
		func() error {
			result, err := f.store.DeleteDigest(t.Context(), domain.DigestDelete{WorkspaceID: f.workspaceID, DigestID: definition.ID, ExpectedVersion: 1, DeletedAt: f.now})
			if err == nil && !result.Deleted {
				return domain.ErrConflict
			}
			return err
		},
	}
	start := make(chan struct{})
	failures := make([]error, len(operations))
	var wg sync.WaitGroup
	for i, operation := range operations {
		wg.Go(func() { <-start; failures[i] = operation() })
	}
	close(start)
	wg.Wait()
	for _, err := range failures {
		require.True(t, err == nil || errors.Is(err, domain.ErrDigestNotFound) || errors.Is(err, domain.ErrJobNotRetryable), "unexpected admission error: %v", err)
	}
	_, found, err := f.store.GetDigest(t.Context(), f.workspaceID, definition.ID)
	require.NoError(t, err)
	require.False(t, found)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	var active int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE workspace_id=$1 AND idempotency_key=$2 AND status IN ('queued','running','retry_wait')`, f.workspaceID, "generate-digest:"+definition.ID.String()).Scan(&active))
	require.Zero(t, active, "no accepted queued work survives committed deletion")
}

func TestDeletingDigestRollsBackIfJobCancellationFails(t *testing.T) {
	f := newFeedFixture(t)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Atomic delete", Enabled: true, Version: 1, Timezone: "UTC", NextRunAt: &f.now, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	admission, err := f.store.AdmitDigestBuild(t.Context(), manualDigestRequest(t, definition, uuid.New()))
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `ALTER TABLE jobs ADD CONSTRAINT fail_cancel CHECK(status<>'cancelled')`)
	require.NoError(t, err)
	_, err = f.store.DeleteDigest(t.Context(), domain.DigestDelete{WorkspaceID: f.workspaceID, DigestID: definition.ID, ExpectedVersion: 1, DeletedAt: f.now})
	require.Error(t, err)
	saved, found, err := f.store.GetDigest(t.Context(), f.workspaceID, definition.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, saved.Enabled)
	job, found, err := f.store.Jobs().Get(t.Context(), admission.Job.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, domain.JobStatusQueued, job.Status)
}
