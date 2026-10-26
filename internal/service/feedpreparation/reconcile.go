package feedpreparation

//go:generate go tool mockgen -source=reconcile.go -destination=mocks/reconcile.gen.go -package=mocks

import (
	"context"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/google/uuid"
)

// DueRepository finds Workspaces whose candidate preparation needs repair or refill.
type DueRepository interface {
	// DueFeedWorkspaces returns a bounded page of Workspaces needing candidate preparation.
	DueFeedWorkspaces(ctx context.Context, query domain.FeedPreparationDueQuery) ([]uuid.UUID, error)
}

// Enqueuer submits durable Feed Preparation Jobs.
type Enqueuer interface {
	// Enqueue submits a durable Job and reuses an equivalent active idempotency key.
	Enqueue(ctx context.Context, request jobs.EnqueueRequest) (domain.Job, error)
}

// Reconciler repairs missing preparation Jobs from durable due state across Workspaces.
type Reconciler struct {
	repository  DueRepository
	queue       Enqueuer
	now         func() time.Time
	gateVersion string
}

// NewReconciler wires due-state discovery to bounded durable preparation submission.
func NewReconciler(repository DueRepository, queue Enqueuer, now func() time.Time, gateVersion string) *Reconciler {
	return &Reconciler{repository: repository, queue: queue, now: now, gateVersion: gateVersion}
}

// Reconcile pages through due Workspaces and submits idempotent preparation Jobs.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	var after *uuid.UUID
	for {
		ids, err := r.repository.DueFeedWorkspaces(ctx, domain.FeedPreparationDueQuery{At: r.now(), After: after, Limit: 100, GateVersion: r.gateVersion})
		if err != nil {
			return err
		}
		for _, workspaceID := range ids {
			if _, err := r.queue.Enqueue(ctx, jobs.EnqueueRequest{WorkspaceID: workspaceID, Definition: Definition, Arguments: struct{}{}, IdempotencyKey: domain.JobKindPrepareFeed}); err != nil {
				return err
			}
		}
		if len(ids) < 100 {
			return nil
		}
		last := ids[len(ids)-1]
		after = &last
	}
}

// Run periodically repairs due preparation work until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context) error {
	if err := r.Reconcile(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.Reconcile(ctx); err != nil {
				return err
			}
		}
	}
}
