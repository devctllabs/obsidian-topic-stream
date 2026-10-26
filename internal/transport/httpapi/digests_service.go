package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
	"github.com/google/uuid"
)

//go:generate go tool mockgen -source=digests_service.go -destination=mocks/digests.gen.go -package=mocks

// Digests exposes the application operations consumed by its HTTP controller.
type Digests interface {
	// ListIssues returns saved Workspace issues, optionally restricted to one Digest Definition.
	ListIssues(ctx context.Context, request digests.IssueListRequest) (digests.IssuePage, error)
	// GetIssue returns a saved Workspace issue or ErrNotFound.
	GetIssue(ctx context.Context, issueID uuid.UUID) (domain.DigestIssue, error)
	// List returns the Workspace's Digest Definitions.
	List(ctx context.Context) ([]domain.DigestDefinition, error)
	// Create validates sections and schedule, then stores the initial version and next run.
	Create(ctx context.Context, input digests.Input) (domain.DigestDefinition, error)
	// Delete removes a definition using expectedVersion; stale edits return ErrConflict.
	Delete(ctx context.Context, digestID uuid.UUID, expectedVersion int) error
	// Update applies supplied fields using expectedVersion; stale edits return ErrConflict.
	Update(ctx context.Context, digestID uuid.UUID, expectedVersion int, patch digests.Patch) (domain.DigestDefinition, error)
	// Generate enqueues on-demand generation and reuses equivalent active work for the definition.
	Generate(ctx context.Context, digestID, requestID uuid.UUID) (domain.Job, error)
}
