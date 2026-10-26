package deps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/config"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/retrieval"
	"github.com/google/uuid"
)

// Reindex owns the storage resources used to enqueue a retrieval rebuild.
type Reindex struct {
	maintenance *retrieval.Maintenance
	store       *postgres.Store
}

// NewReindex resolves configuration and owns resources for enqueuing a rebuild.
func NewReindex(ctx context.Context) (*Reindex, error) {
	cfg, err := config.Load(ctx)
	if err != nil {
		return nil, err
	}
	return newReindex(ctx, cfg)
}

func newReindex(ctx context.Context, cfg generated.Config) (*Reindex, error) {
	workspaceID, err := validateReindexConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	store, err := postgres.Open(ctx, cfg.DBPrimary.PostgresDSN)
	if err != nil {
		return nil, err
	}
	queue := jobs.New(store.Jobs(), time.Now, jobs.DefaultConfig())
	maintenance, err := retrieval.NewMaintenance(retrieval.MaintenanceConfig{
		WorkspaceID: workspaceID,
		Catalog:     store,
		Queue:       queue,
	})
	if err != nil {
		store.Close()
		return nil, err
	}
	return &Reindex{maintenance: maintenance, store: store}, nil
}

// Rebuild enqueues a model-preserving rebuild using the acquired Workspace dependencies.
func (r *Reindex) Rebuild(ctx context.Context) (domain.Job, error) {
	return r.maintenance.Rebuild(ctx)
}

// Shutdown releases dependencies acquired by this runtime.
func (r *Reindex) Shutdown(_ context.Context) error {
	r.store.Close()
	return nil
}

func validateReindexConfig(ctx context.Context, cfg generated.Config) (uuid.UUID, error) {
	if ctx == nil {
		return uuid.Nil, errors.New("runtime context is required")
	}
	if err := cfg.Validate(); err != nil {
		return uuid.Nil, fmt.Errorf("validate runtime configuration: %w", err)
	}
	workspaceID, err := uuid.Parse(cfg.Runtime.WorkspaceId)
	if err != nil {
		return uuid.Nil, fmt.Errorf("workspace ID must be a UUID: %w", err)
	}
	if cfg.DBPrimary.Kind != "postgres" {
		return uuid.Nil, fmt.Errorf("database kind %q is not supported", cfg.DBPrimary.Kind)
	}
	if strings.TrimSpace(cfg.DBPrimary.PostgresDSN) == "" {
		return uuid.Nil, errors.New("postgres DSN is required")
	}
	return workspaceID, nil
}
