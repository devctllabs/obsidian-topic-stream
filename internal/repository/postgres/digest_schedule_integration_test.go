//go:build integration

package postgres_test

import (
	"sync"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestScheduledDigestAdmissionSharesManualJobAndAdvancesOnce(t *testing.T) {
	for _, manualFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "manual first", false: "schedule first"}[manualFirst], func(t *testing.T) {
			checkScheduledDigestCoalescing(t, manualFirst)
		})
	}
}

// checkScheduledDigestCoalescing verifies one arrival order with concurrent scheduler replicas.
func checkScheduledDigestCoalescing(t *testing.T, manualFirst bool) {
	t.Helper()
	f := newFeedFixture(t)
	due := f.now.Add(-time.Hour)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Due", Enabled: true, Version: 1, Timezone: "UTC", NextRunAt: &due, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	manual := manualDigestRequest(t, definition, uuid.New())
	candidate := manualDigestRequest(t, definition, uuid.New()).Job
	candidate.Arguments = []byte(`{"digestId":"` + definition.ID.String() + `","scheduledFor":"` + due.Format(time.RFC3339Nano) + `"}`)
	request := domain.DigestScheduledBuild{Advance: domain.DigestScheduleAdvance{WorkspaceID: f.workspaceID, DigestID: definition.ID, Expected: due, NextRunAt: f.now.Add(time.Hour), UpdatedAt: f.now}, ExpectedVersion: 1, Job: candidate}
	var initial domain.DigestBuildAdmission
	var err error
	if manualFirst {
		initial, err = f.store.AdmitDigestBuild(t.Context(), manual)
	} else {
		initial, err = f.store.AdmitScheduledDigestBuild(t.Context(), request)
	}
	require.NoError(t, err)
	require.True(t, initial.Created)
	const replicas = 8
	start := make(chan struct{})
	results := make([]domain.DigestBuildAdmission, replicas)
	failures := make([]error, replicas)
	var wg sync.WaitGroup
	for i := range replicas {
		wg.Go(func() { <-start; results[i], failures[i] = f.store.AdmitScheduledDigestBuild(t.Context(), request) })
	}
	close(start)
	wg.Wait()
	covered := 0
	for i, result := range results {
		require.NoError(t, failures[i])
		require.False(t, result.Created)
		if result.Job.ID != uuid.Nil {
			covered++
			require.Equal(t, initial.Job, result.Job)
		}
	}
	if manualFirst {
		require.Equal(t, 1, covered)
	} else {
		require.Zero(t, covered)
	}
	alias, err := f.store.AdmitDigestBuild(t.Context(), manual)
	require.NoError(t, err)
	require.Equal(t, initial.Job, alias.Job)
	saved, found, err := f.store.GetDigest(t.Context(), f.workspaceID, definition.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, request.Advance.NextRunAt.Equal(*saved.NextRunAt))
}

func TestScheduledDigestAdmissionRollsBackAndRejectsStaleDisabledObservations(t *testing.T) {
	f := newFeedFixture(t)
	due := f.now.Add(-time.Hour)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Due", Enabled: true, Version: 1, Timezone: "UTC", NextRunAt: &due, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	request := domain.DigestScheduledBuild{Advance: domain.DigestScheduleAdvance{WorkspaceID: f.workspaceID, DigestID: definition.ID, Expected: due, NextRunAt: f.now.Add(time.Hour), UpdatedAt: f.now}, ExpectedVersion: 1, Job: manualDigestRequest(t, definition, uuid.New()).Job}
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `ALTER TABLE digest_definitions ADD CONSTRAINT fail_advance CHECK(last_scheduled_for IS NULL)`)
	require.NoError(t, err)
	_, err = f.store.AdmitScheduledDigestBuild(t.Context(), request)
	require.Error(t, err)
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE id=$1`, request.Job.ID).Scan(&count))
	require.Zero(t, count)
	saved, _, err := f.store.GetDigest(t.Context(), f.workspaceID, definition.ID)
	require.NoError(t, err)
	require.True(t, due.Equal(*saved.NextRunAt))
	_, err = conn.Exec(t.Context(), `ALTER TABLE digest_definitions DROP CONSTRAINT fail_advance`)
	require.NoError(t, err)
	request.Job.Queue = "invalid"
	_, err = f.store.AdmitScheduledDigestBuild(t.Context(), request)
	require.Error(t, err)
	saved, _, err = f.store.GetDigest(t.Context(), f.workspaceID, definition.ID)
	require.NoError(t, err)
	require.True(t, due.Equal(*saved.NextRunAt))
	request.Job = manualDigestRequest(t, definition, uuid.New()).Job
	// Lock a disabled version in a separate transaction before releasing the stale scheduler.
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	_, err = tx.Exec(t.Context(), `UPDATE digest_definitions SET enabled=false,next_run_at=NULL,version=2 WHERE id=$1`, definition.ID)
	require.NoError(t, err)
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		result, err := f.store.AdmitScheduledDigestBuild(t.Context(), request)
		if err == nil && result.Job.ID != uuid.Nil {
			done <- domain.ErrConflict
			return
		}
		done <- err
	}()
	<-started
	require.NoError(t, tx.Commit(t.Context()))
	require.NoError(t, <-done)
	manual, err := f.store.AdmitDigestBuild(t.Context(), manualDigestRequest(t, definition, uuid.New()))
	require.NoError(t, err)
	require.True(t, manual.Created)
}
