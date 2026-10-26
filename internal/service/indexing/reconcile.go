package indexing

import (
	"context"
	"sync"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
)

// GlobalPending finds pending projection commits across all Workspaces.
type GlobalPending interface {
	// ListPendingGlobalItemProjections returns missing Jobs and progress through bounded pending keys.
	ListPendingGlobalItemProjections(ctx context.Context, query domain.ProjectionRepairQuery) (domain.ProjectionRepairPage, error)
}

// Reconciler repairs missing projection Jobs from pending commits across Workspaces.
type Reconciler struct {
	mu         sync.Mutex
	after      *domain.ProjectionRepairCursor
	repository GlobalPending
	queue      jobs.Queue
}

// NewReconciler wires global pending projections to durable repair submission.
func NewReconciler(repository GlobalPending, queue jobs.Queue) *Reconciler {
	return &Reconciler{repository: repository, queue: queue}
}

// Reconcile submits idempotent Jobs for a bounded batch of pending projection commits.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	page, err := r.repository.ListPendingGlobalItemProjections(ctx, domain.ProjectionRepairQuery{Limit: repairBatchSize, After: r.after})
	if err != nil {
		return err
	}
	for _, commit := range page.Commits {
		if _, err := enqueueItemProjection(ctx, r.queue, commit); err != nil {
			return err
		}
	}
	r.after = page.NextCursor
	return nil
}

// Run periodically repairs missing projection Jobs until ctx is cancelled.
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
