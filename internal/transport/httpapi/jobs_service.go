package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/google/uuid"
)

//go:generate go tool mockgen -source=jobs_service.go -destination=mocks/jobs.gen.go -package=mocks

// Jobs exposes the application operations consumed by its HTTP controller.
type Jobs interface {
	// List validates pagination and returns Jobs belonging to the selected Workspace.
	List(ctx context.Context, request jobs.ListRequest) (jobs.Page, error)
	// GetForWorkspace returns a Job only within the requested Workspace, otherwise ErrNotFound.
	GetForWorkspace(ctx context.Context, workspaceID, id uuid.UUID) (domain.Job, error)
	// RetryForWorkspace creates a fresh attempt sequence for dead or cancelled Workspace work with an
	// available handler.
	RetryForWorkspace(ctx context.Context, workspaceID, id uuid.UUID) (domain.Job, error)
	// CancelForWorkspace cancels queued work or requests cancellation of a running Workspace Job.
	CancelForWorkspace(ctx context.Context, workspaceID, id uuid.UUID) (domain.Job, error)
}
