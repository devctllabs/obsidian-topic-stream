package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/sources"
	"github.com/google/uuid"
)

//go:generate go tool mockgen -source=sources_service.go -destination=mocks/sources.gen.go -package=mocks

// Sources exposes the application operations consumed by its HTTP controller.
type Sources interface {
	// List returns Sources belonging to the service's Workspace.
	List(ctx context.Context) ([]domain.Source, error)
	// Create validates Source configuration and persists a new Workspace Source.
	Create(ctx context.Context, input sources.CreateInput) (domain.Source, error)
	// SyncAll enqueues durable synchronization of enabled Workspace Sources.
	SyncAll(ctx context.Context) (domain.Job, error)
	// Get returns a Workspace Source or ErrNotFound.
	Get(ctx context.Context, sourceID uuid.UUID) (domain.Source, error)
	// Update applies supplied Source fields after validating the resulting configuration.
	Update(ctx context.Context, sourceID uuid.UUID, input sources.UpdateInput) (domain.Source, error)
	// Delete removes a Workspace Source, returning ErrNotFound when absent.
	Delete(ctx context.Context, sourceID uuid.UUID) error
	// Sync enqueues durable work and deduplicates concurrent requests for the same Source.
	Sync(ctx context.Context, sourceID uuid.UUID) (domain.Job, error)
}
