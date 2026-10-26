package settings

//go:generate go tool mockgen -source=cache.go -destination=mocks/cache.gen.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

// CacheRepository loads or initializes persistent Workspace Settings snapshots.
type CacheRepository interface {
	// GetOrCreateWorkspaceSettings returns persisted settings, creating the supplied defaults when absent.
	GetOrCreateWorkspaceSettings(ctx context.Context, workspaceID uuid.UUID, defaults domain.WorkspaceSettings) (domain.WorkspaceSettings, error)
}

// Cache keeps one immutable Workspace Settings snapshot per Workspace. Callers receive copies so
// an operation can freeze its settings without holding a lock or observing a partial update.
type Cache struct {
	repository CacheRepository
	now        func() time.Time

	// mu protects snapshots; returned settings are cloned so callers do not mutate cached pointers.
	mu        sync.RWMutex
	snapshots map[uuid.UUID]domain.WorkspaceSettings
}

// NewCache creates a lazy per-Workspace snapshot cache backed by persistent settings.
func NewCache(repository CacheRepository, now func() time.Time) *Cache {
	return &Cache{repository: repository, now: now, snapshots: make(map[uuid.UUID]domain.WorkspaceSettings)}
}

// Snapshot lazily loads one Workspace and returns a copy isolated from later cache updates.
func (c *Cache) Snapshot(ctx context.Context, workspaceID uuid.UUID) (domain.WorkspaceSettings, error) {
	c.mu.RLock()
	snapshot, found := c.snapshots[workspaceID]
	c.mu.RUnlock()
	if found {
		return cloneWorkspaceSettings(snapshot), nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if snapshot, found = c.snapshots[workspaceID]; found {
		return cloneWorkspaceSettings(snapshot), nil
	}
	snapshot, err := c.repository.GetOrCreateWorkspaceSettings(ctx, workspaceID, DefaultWorkspaceSettings(c.now()))
	if err != nil {
		return domain.WorkspaceSettings{}, fmt.Errorf("load workspace settings snapshot: %w", err)
	}
	c.snapshots[workspaceID] = cloneWorkspaceSettings(snapshot)
	return cloneWorkspaceSettings(snapshot), nil
}

// Publish replaces a Workspace's cached snapshot with an isolated copy.
func (c *Cache) Publish(workspaceID uuid.UUID, settings domain.WorkspaceSettings) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshots[workspaceID] = cloneWorkspaceSettings(settings)
}

// Reload fetches a Workspace when the advisory version is newer and publishes only a newer stored
// snapshot.
func (c *Cache) Reload(ctx context.Context, workspaceID uuid.UUID, version int) error {
	c.mu.RLock()
	current, found := c.snapshots[workspaceID]
	c.mu.RUnlock()
	if found && current.Version >= version {
		return nil
	}

	loaded, err := c.load(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("reload workspace settings snapshot: %w", err)
	}
	c.publishNewer(workspaceID, loaded)
	return nil
}

// ReloadCached closes a LISTEN/NOTIFY delivery gap by rereading every Workspace this process has
// already observed. Failed loads preserve the last snapshot and do not prevent other Workspaces
// from being refreshed. Workspaces not present in memory remain lazy.
func (c *Cache) ReloadCached(ctx context.Context) error {
	c.mu.RLock()
	workspaceIDs := make([]uuid.UUID, 0, len(c.snapshots))
	for workspaceID := range c.snapshots {
		workspaceIDs = append(workspaceIDs, workspaceID)
	}
	c.mu.RUnlock()
	var reloadErrors []error
	for _, workspaceID := range workspaceIDs {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(reloadErrors, err)...)
		}
		loaded, err := c.load(ctx, workspaceID)
		if err != nil {
			reloadErrors = append(reloadErrors, fmt.Errorf("reload cached workspace %s: %w", workspaceID, err))
			continue
		}
		c.publishNewer(workspaceID, loaded)
	}
	return errors.Join(reloadErrors...)
}

func (c *Cache) load(ctx context.Context, workspaceID uuid.UUID) (domain.WorkspaceSettings, error) {
	return c.repository.GetOrCreateWorkspaceSettings(ctx, workspaceID, DefaultWorkspaceSettings(c.now()))
}

func (c *Cache) publishNewer(workspaceID uuid.UUID, loaded domain.WorkspaceSettings) {
	c.mu.Lock()
	defer c.mu.Unlock()
	current, found := c.snapshots[workspaceID]
	if !found || loaded.Version > current.Version {
		c.snapshots[workspaceID] = cloneWorkspaceSettings(loaded)
	}
}

func cloneWorkspaceSettings(source domain.WorkspaceSettings) domain.WorkspaceSettings {
	clone := source
	clone.Generation.Endpoint = cloneString(source.Generation.Endpoint)
	clone.Generation.SummaryModel = cloneString(source.Generation.SummaryModel)
	clone.Generation.RerankModel = cloneString(source.Generation.RerankModel)
	if source.Retention.FullContentDays != nil {
		value := *source.Retention.FullContentDays
		clone.Retention.FullContentDays = &value
	}
	return clone
}

func cloneString(source *string) *string {
	if source == nil {
		return nil
	}
	value := *source
	return &value
}
