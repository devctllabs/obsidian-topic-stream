package settings_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type cacheRepositoryFunc func(context.Context, uuid.UUID, domain.WorkspaceSettings) (domain.WorkspaceSettings, error)

// GetOrCreateWorkspaceSettings delegates loading to the callback selected by the test.
func (f cacheRepositoryFunc) GetOrCreateWorkspaceSettings(
	ctx context.Context, workspaceID uuid.UUID, defaults domain.WorkspaceSettings,
) (domain.WorkspaceSettings, error) {
	return f(ctx, workspaceID, defaults)
}

func TestCacheReloadCachedCollectsErrorsForEveryWorkspace(t *testing.T) {
	t.Parallel()

	firstID, secondID := uuid.New(), uuid.New()
	firstError, secondError := errors.New("first workspace unavailable"), errors.New("second workspace unavailable")
	failures := map[uuid.UUID]error{firstID: firstError, secondID: secondError}
	reads := make(map[uuid.UUID]int)
	cache := settings.NewCache(cacheRepositoryFunc(func(
		_ context.Context, workspaceID uuid.UUID, _ domain.WorkspaceSettings,
	) (domain.WorkspaceSettings, error) {
		reads[workspaceID]++
		return domain.WorkspaceSettings{}, failures[workspaceID]
	}), time.Now)
	original := settings.DefaultWorkspaceSettings(time.Now())
	cache.Publish(firstID, original)
	cache.Publish(secondID, original)

	err := cache.ReloadCached(t.Context())

	require.ErrorIs(t, err, firstError)
	require.ErrorIs(t, err, secondError)
	require.Equal(t, map[uuid.UUID]int{firstID: 1, secondID: 1}, reads)
	for _, workspaceID := range []uuid.UUID{firstID, secondID} {
		observed, err := cache.Snapshot(t.Context(), workspaceID)
		require.NoError(t, err)
		require.Equal(t, original, observed)
	}
}

func TestCacheReloadCachedPreservesFailedSnapshotAndRecoversWithoutDowngrading(t *testing.T) {
	t.Parallel()

	failedID, healthyID := uuid.New(), uuid.New()
	original := settings.DefaultWorkspaceSettings(time.Now())
	updated := original
	updated.Version++
	updated.Retrieval.CandidateLimit++
	failure := errors.New("workspace temporarily unavailable")
	cache := settings.NewCache(cacheRepositoryFunc(func(
		_ context.Context, workspaceID uuid.UUID, _ domain.WorkspaceSettings,
	) (domain.WorkspaceSettings, error) {
		if workspaceID == failedID && failure != nil {
			return domain.WorkspaceSettings{}, failure
		}
		return updated, nil
	}), time.Now)
	cache.Publish(failedID, original)
	cache.Publish(healthyID, original)

	require.ErrorIs(t, cache.ReloadCached(t.Context()), failure)
	failed, err := cache.Snapshot(t.Context(), failedID)
	require.NoError(t, err)
	require.Equal(t, original, failed)
	healthy, err := cache.Snapshot(t.Context(), healthyID)
	require.NoError(t, err)
	require.Equal(t, updated, healthy)

	newer := updated
	newer.Version++
	cache.Publish(healthyID, newer)
	failure = nil
	require.NoError(t, cache.ReloadCached(t.Context()))
	failed, err = cache.Snapshot(t.Context(), failedID)
	require.NoError(t, err)
	require.Equal(t, updated, failed)
	healthy, err = cache.Snapshot(t.Context(), healthyID)
	require.NoError(t, err)
	require.Equal(t, newer, healthy)
}

func TestCacheReloadCachedStopsOnCancellation(t *testing.T) {
	t.Parallel()

	reads := 0
	cache := settings.NewCache(cacheRepositoryFunc(func(
		context.Context, uuid.UUID, domain.WorkspaceSettings,
	) (domain.WorkspaceSettings, error) {
		reads++
		return domain.WorkspaceSettings{}, nil
	}), time.Now)
	cache.Publish(uuid.New(), settings.DefaultWorkspaceSettings(time.Now()))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, cache.ReloadCached(ctx), context.Canceled)
	require.Zero(t, reads)
}
