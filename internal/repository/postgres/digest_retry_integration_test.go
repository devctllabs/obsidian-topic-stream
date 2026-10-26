//go:build integration

package postgres_test

import (
	"sync"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestDigestRetryRetainsSuccessorChainAndSeparatesManualIdentity(t *testing.T) {
	f := newFeedFixture(t)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Manual Retry", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	source := deadDigestJob(t, f, definition)
	manual, err := f.store.AdmitDigestBuild(t.Context(), manualDigestRequest(t, definition, source.ID))
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET status='succeeded' WHERE id=$1`, manual.Job.ID)
	require.NoError(t, err)
	request := manualDigestRequest(t, definition, source.ID)
	request.Kind = domain.DigestBuildRetry
	successor, err := f.store.AdmitDigestBuild(t.Context(), request)
	require.NoError(t, err)
	require.True(t, successor.Created)
	require.NotEqual(t, manual.Job.ID, successor.Job.ID)
	require.JSONEq(t, string(source.Arguments), string(successor.Job.Arguments))
	fresh, err := postgres.Open(t.Context(), f.dsn)
	require.NoError(t, err)
	defer fresh.Close()
	for _, status := range []domain.JobStatus{domain.JobStatusDead, domain.JobStatusCancelled, domain.JobStatusSucceeded} {
		_, err = conn.Exec(t.Context(), `UPDATE jobs SET status=$2 WHERE id=$1`, successor.Job.ID, status)
		require.NoError(t, err)
		replay, err := fresh.AdmitDigestBuild(t.Context(), request)
		require.NoError(t, err)
		require.False(t, replay.Created)
		require.Equal(t, successor.Job.ID, replay.Job.ID)
		require.Equal(t, status, replay.Job.Status)
	}
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET status='dead' WHERE id=$1`, successor.Job.ID)
	require.NoError(t, err)
	chain := manualDigestRequest(t, definition, successor.Job.ID)
	chain.Kind = domain.DigestBuildRetry
	next, err := fresh.AdmitDigestBuild(t.Context(), chain)
	require.NoError(t, err)
	require.True(t, next.Created)
	require.NotEqual(t, successor.Job.ID, next.Job.ID)
	require.JSONEq(t, string(source.Arguments), string(next.Job.Arguments))
	_, err = fresh.DeleteDigest(t.Context(), domain.DigestDelete{WorkspaceID: f.workspaceID, DigestID: definition.ID, ExpectedVersion: 1, DeletedAt: f.now})
	require.NoError(t, err)
	replay, err := fresh.AdmitDigestBuild(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, successor.Job.ID, replay.Job.ID)
	replay, err = fresh.AdmitDigestBuild(t.Context(), chain)
	require.NoError(t, err)
	require.Equal(t, next.Job.ID, replay.Job.ID)
	chain.RequestID = next.Job.ID
	_, err = fresh.AdmitDigestBuild(t.Context(), chain)
	require.ErrorIs(t, err, domain.ErrJobNotRetryable)
}

func TestDigestRetryManualAndScheduleRaceShareOneActiveJob(t *testing.T) {
	f := newFeedFixture(t)
	due := f.now.Add(-time.Hour)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Concurrent", Enabled: true, Timezone: "UTC", Version: 1, NextRunAt: &due, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	source := deadDigestJob(t, f, definition)
	const callers = 18
	requests := make([]domain.DigestBuildRequest, callers)
	for i := range requests {
		requests[i] = manualDigestRequest(t, definition, uuid.New())
		if i%3 == 0 {
			requests[i].Kind = domain.DigestBuildRetry
			requests[i].RequestID = source.ID
		}
	}
	schedule := domain.DigestScheduledBuild{Advance: domain.DigestScheduleAdvance{WorkspaceID: f.workspaceID, DigestID: definition.ID, Expected: due, NextRunAt: f.now.Add(time.Hour), UpdatedAt: f.now}, ExpectedVersion: 1, Job: manualDigestRequest(t, definition, uuid.New()).Job}
	start := make(chan struct{})
	results := make([]domain.DigestBuildAdmission, callers)
	failures := make([]error, callers)
	var wg sync.WaitGroup
	for i := range requests {
		wg.Go(func() {
			<-start
			if i%3 == 1 {
				results[i], failures[i] = f.store.AdmitScheduledDigestBuild(t.Context(), schedule)
			} else {
				results[i], failures[i] = f.store.AdmitDigestBuild(t.Context(), requests[i])
			}
		})
	}
	close(start)
	wg.Wait()
	var active uuid.UUID
	created := 0
	for i, result := range results {
		require.NoError(t, failures[i])
		if result.Job.ID == uuid.Nil {
			continue
		}
		if active == uuid.Nil {
			active = result.Job.ID
		}
		require.Equal(t, active, result.Job.ID)
		if result.Created {
			created++
		}
	}
	require.Equal(t, 1, created)
	replay, err := f.store.AdmitDigestBuild(t.Context(), requests[0])
	require.NoError(t, err)
	require.Equal(t, active, replay.Job.ID)
}

func TestDigestRetryRejectsMissingForeignAndNonretryableSources(t *testing.T) {
	f := newFeedFixture(t)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Retry", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	source := deadDigestJob(t, f, definition)
	request := manualDigestRequest(t, definition, uuid.New())
	request.Kind = domain.DigestBuildRetry
	_, err := f.store.AdmitDigestBuild(t.Context(), request)
	require.ErrorIs(t, err, domain.ErrJobNotFound)
	request.RequestID = source.ID
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	for _, status := range []domain.JobStatus{domain.JobStatusQueued, domain.JobStatusRunning, domain.JobStatusSucceeded} {
		_, err = conn.Exec(t.Context(), `UPDATE jobs SET status=$2 WHERE id=$1`, source.ID, status)
		require.NoError(t, err)
		_, err = f.store.AdmitDigestBuild(t.Context(), request)
		require.ErrorIs(t, err, domain.ErrJobNotRetryable)
	}
	otherWS := uuid.New()
	require.NoError(t, f.store.EnsureWorkspace(t.Context(), otherWS))
	other := definition
	other.ID, other.WorkspaceID = uuid.New(), otherWS
	require.NoError(t, f.store.CreateDigest(t.Context(), other))
	foreign := manualDigestRequest(t, other, source.ID)
	foreign.Kind = domain.DigestBuildRetry
	_, err = f.store.AdmitDigestBuild(t.Context(), foreign)
	require.ErrorIs(t, err, domain.ErrJobNotFound)
}

// deadDigestJob stores a failed scheduled source without starting a worker.
func deadDigestJob(t *testing.T, f feedFixture, definition domain.DigestDefinition) domain.Job {
	t.Helper()
	source := manualDigestRequest(t, definition, uuid.New()).Job
	source.Arguments = []byte(`{"digestId":"` + definition.ID.String() + `","scheduledFor":"` + f.now.Format(time.RFC3339Nano) + `"}`)
	source.Status = domain.JobStatusDead
	saved, created, err := f.store.Jobs().Enqueue(t.Context(), source)
	require.NoError(t, err)
	require.True(t, created)
	return saved
}
