package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/topics"
	"github.com/google/uuid"
)

//go:generate go tool mockgen -source=topics_service.go -destination=mocks/topics.gen.go -package=mocks

// Topics exposes the application operations consumed by its HTTP controller.
type Topics interface {
	// List returns Topics belonging to the service's Workspace.
	List(ctx context.Context) ([]domain.Topic, error)
	// Create validates Topic scope and criteria, then persists its initial semantic revision.
	Create(ctx context.Context, input topics.CreateInput) (domain.Topic, error)
	// Update increments the semantic revision only when scope, criteria, or mode changes; display and
	// enabled-state edits do not invalidate historical ranking inputs.
	Update(ctx context.Context, topicID uuid.UUID, input topics.UpdateInput) (domain.Topic, error)
	// Delete removes a Workspace Topic, returning ErrNotFound when absent.
	Delete(ctx context.Context, topicID uuid.UUID) error
}
