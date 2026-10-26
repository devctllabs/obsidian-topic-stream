//go:build integration

package postgres_test

import (
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	repository "github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPreferenceProfilePreservesHistoricalRevisionsAndRejectsStaleWrites(t *testing.T) {
	store, err := repository.Open(t.Context(), testutil.MigratedPostgresSchema(t))
	require.NoError(t, err)
	t.Cleanup(store.Close)
	workspaceID, otherID := uuid.New(), uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	require.NoError(t, store.EnsureWorkspace(t.Context(), otherID))
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	original, err := store.GetOrCreatePreferenceProfile(t.Context(), workspaceID, domain.PreferenceProfile{Version: 1, Revision: 1, UpdatedAt: now})
	require.NoError(t, err)
	updated := original
	updated.Prefer = "distributed systems"
	updated.Version = 2
	updated.Revision = 2
	updated.UpdatedAt = now.Add(time.Hour)
	stored, changed, err := store.UpdatePreferenceProfile(t.Context(), workspaceID, 1, updated)
	require.NoError(t, err)
	require.True(t, changed)
	requireProfile(t, updated, stored)
	stale := updated
	stale.Prefer = "must not overwrite"
	stale.Version = 3
	stale.Revision = 3
	_, changed, err = store.UpdatePreferenceProfile(t.Context(), workspaceID, 1, stale)
	require.NoError(t, err)
	require.False(t, changed)
	for _, expected := range []domain.PreferenceProfile{original, updated} {
		revision, found, err := store.GetPreferenceProfileRevision(t.Context(), workspaceID, expected.Revision)
		require.NoError(t, err)
		require.True(t, found)
		requireProfile(t, expected, revision)
	}
	_, found, err := store.GetPreferenceProfileRevision(t.Context(), workspaceID, 3)
	require.NoError(t, err)
	require.False(t, found)
	_, found, err = store.GetPreferenceProfileRevision(t.Context(), otherID, 1)
	require.NoError(t, err)
	require.False(t, found)
}

func TestCurrentFeedbackIsWorkspaceScopedOrderedAndRemovedOnClear(t *testing.T) {
	f := newFeedFixture(t)
	likedID, dislikedID := f.item(t), f.item(t)
	_ = f.item(t) // An Item without feedback must not appear in the result.
	other := f
	other.workspaceID = uuid.New()
	other.sourceID = uuid.New()
	require.NoError(t, f.store.EnsureWorkspace(t.Context(), other.workspaceID))
	require.NoError(t, f.store.Create(t.Context(), domain.Source{ID: other.sourceID, WorkspaceID: other.workspaceID, Name: "Other", Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://other.example/feed"}, Enabled: true, PollIntervalMinutes: 60, CreatedAt: f.now, UpdatedAt: f.now}))
	otherItem := other.item(t)
	first := domain.Feedback{Sentiment: "like", CreatedAt: f.now, UpdatedAt: f.now}
	second := domain.Feedback{Sentiment: "dislike", CreatedAt: f.now.Add(time.Minute), UpdatedAt: f.now.Add(time.Minute)}
	_, found, err := f.store.SetFeedback(t.Context(), f.workspaceID, likedID, first)
	require.NoError(t, err)
	require.True(t, found)
	_, found, err = f.store.SetFeedback(t.Context(), f.workspaceID, dislikedID, second)
	require.NoError(t, err)
	require.True(t, found)
	_, found, err = f.store.SetFeedback(t.Context(), other.workspaceID, otherItem, first)
	require.NoError(t, err)
	require.True(t, found)
	feedback, err := f.store.ListCurrentFeedback(t.Context(), f.workspaceID)
	require.NoError(t, err)
	requireCurrentFeedback(t, []domain.ItemFeedback{{ItemID: dislikedID, Feedback: second}, {ItemID: likedID, Feedback: first}}, feedback)
	_, found, err = f.store.ClearFeedback(t.Context(), f.workspaceID, dislikedID, f.now.Add(2*time.Minute))
	require.NoError(t, err)
	require.True(t, found)
	feedback, err = f.store.ListCurrentFeedback(t.Context(), f.workspaceID)
	require.NoError(t, err)
	requireCurrentFeedback(t, []domain.ItemFeedback{{ItemID: likedID, Feedback: first}}, feedback)
}

func TestJobListUsesWorkspaceStatusAndStableCursorBoundaries(t *testing.T) {
	store, err := repository.Open(t.Context(), testutil.MigratedPostgresSchema(t))
	require.NoError(t, err)
	t.Cleanup(store.Close)
	workspaceID, otherID := uuid.New(), uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	require.NoError(t, store.EnsureWorkspace(t.Context(), otherID))
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	ids := []uuid.UUID{uuid.MustParse("00000000-0000-4000-8000-000000000001"), uuid.MustParse("00000000-0000-4000-8000-000000000002"), uuid.MustParse("00000000-0000-4000-8000-000000000003")}
	for _, id := range ids {
		_, created, err := store.Jobs().Enqueue(t.Context(), domain.Job{ID: id, WorkspaceID: workspaceID, Kind: "page", Queue: domain.JobQueueMaintenance, ArgumentsVersion: 1, Arguments: []byte(`{}`), Status: domain.JobStatusQueued, MaxAttempts: 3, AvailableAt: now, CreatedAt: now, UpdatedAt: now})
		require.NoError(t, err)
		require.True(t, created)
	}
	_, _, err = store.Jobs().Enqueue(t.Context(), domain.Job{ID: uuid.New(), WorkspaceID: otherID, Kind: "private", Queue: domain.JobQueueMaintenance, ArgumentsVersion: 1, Arguments: []byte(`{}`), Status: domain.JobStatusQueued, MaxAttempts: 3, AvailableAt: now, CreatedAt: now, UpdatedAt: now})
	require.NoError(t, err)
	status := domain.JobStatusQueued
	first, err := store.Jobs().List(t.Context(), domain.JobList{WorkspaceID: workspaceID, Status: &status, Limit: 2})
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.Equal(t, ids[2], first[0].ID)
	require.Equal(t, ids[1], first[1].ID)
	second, err := store.Jobs().List(t.Context(), domain.JobList{WorkspaceID: workspaceID, Status: &status, Limit: 2, Cursor: &domain.JobCursor{ID: first[1].ID, CreatedAt: first[1].CreatedAt}})
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, ids[0], second[0].ID)
	all, err := store.Jobs().List(t.Context(), domain.JobList{WorkspaceID: workspaceID, Limit: 2})
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.Equal(t, ids[2], all[0].ID)
	unfilteredNext, err := store.Jobs().List(t.Context(), domain.JobList{WorkspaceID: workspaceID, Limit: 2, Cursor: &domain.JobCursor{ID: all[1].ID, CreatedAt: all[1].CreatedAt}})
	require.NoError(t, err)
	require.Len(t, unfilteredNext, 1)
	require.Equal(t, ids[0], unfilteredNext[0].ID)
	status = domain.JobStatusDead
	dead, err := store.Jobs().List(t.Context(), domain.JobList{WorkspaceID: workspaceID, Status: &status, Limit: 10})
	require.NoError(t, err)
	require.Empty(t, dead)
}

func requireProfile(t *testing.T, expected, actual domain.PreferenceProfile) {
	t.Helper()
	expected.UpdatedAt = expected.UpdatedAt.UTC()
	actual.UpdatedAt = actual.UpdatedAt.UTC()
	require.Equal(t, expected, actual)
}
func requireCurrentFeedback(t *testing.T, expected, actual []domain.ItemFeedback) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for index, want := range expected {
		got := actual[index]
		got.Feedback.CreatedAt = got.Feedback.CreatedAt.UTC()
		got.Feedback.UpdatedAt = got.Feedback.UpdatedAt.UTC()
		want.Feedback.CreatedAt = want.Feedback.CreatedAt.UTC()
		want.Feedback.UpdatedAt = want.Feedback.UpdatedAt.UTC()
		require.Equal(t, want, got)
	}
}

func TestQueuedJobCancellationIsImmediateAndTerminalStateIsStable(t *testing.T) {
	store, err := repository.Open(t.Context(), testutil.MigratedPostgresSchema(t))
	require.NoError(t, err)
	t.Cleanup(store.Close)
	workspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	job := domain.Job{ID: uuid.New(), WorkspaceID: workspaceID, Kind: "cancel", Queue: domain.JobQueueMaintenance, ArgumentsVersion: 1, Arguments: []byte(`{}`), Status: domain.JobStatusQueued, MaxAttempts: 3, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	_, created, err := store.Jobs().Enqueue(t.Context(), job)
	require.NoError(t, err)
	require.True(t, created)
	cancelled, found, err := store.Jobs().RequestCancel(t.Context(), job.ID, now.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, domain.JobStatusCancelled, cancelled.Status)
	require.NotNil(t, cancelled.FinishedAt)
	repeated, found, err := store.Jobs().RequestCancel(t.Context(), job.ID, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, cancelled, repeated)
	claim, err := store.Jobs().Claim(t.Context(), domain.JobClaimRequest{Queue: domain.JobQueueMaintenance, Owner: "worker", Now: now.Add(3 * time.Minute), LeaseExpiresAt: now.Add(4 * time.Minute)})
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, claim.Job.ID)
}
