package retrieval

import (
	"context"
	"sync"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

// ReconciliationRepository repairs bounded Workspace state and exposes durable model requests.
type ReconciliationRepository interface {
	// ReconcileModelStates cleans obsolete transition state and returns requests from bounded Workspaces.
	ReconcileModelStates(ctx context.Context, query domain.ModelReconciliationQuery) (domain.ModelReconciliationPage, error)
	// ModelRequestHasJob includes terminal Jobs so automatic reconciliation respects explicit cancellation.
	ModelRequestHasJob(ctx context.Context, workspaceID uuid.UUID, key string) (bool, error)
	// CompleteModelRequest clears only the matching selection generation after durable enqueue.
	CompleteModelRequest(ctx context.Context, request domain.ModelReconciliationRequest) error
}

// Reconciler repairs interrupted transition enqueue and terminal-worker cleanup across Workspaces.
type Reconciler struct {
	mu         sync.Mutex
	after      *uuid.UUID
	repository ReconciliationRepository
	queue      ReindexQueue
}

// NewReconciler wires durable model requests to the existing maintenance queue.
func NewReconciler(repository ReconciliationRepository, queue ReindexQueue) *Reconciler {
	return &Reconciler{repository: repository, queue: queue}
}

// Reconcile inspects one bounded page and completes only durably represented requests.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	page, err := r.repository.ReconcileModelStates(ctx, domain.ModelReconciliationQuery{AfterWorkspaceID: r.after, Limit: 100})
	if err != nil {
		return err
	}
	for _, request := range page.Requests {
		exists, err := r.repository.ModelRequestHasJob(ctx, request.WorkspaceID, reindexKey(request.ModelID, request.SelectionGeneration))
		if err != nil {
			return err
		}
		if !exists {
			if _, err := enqueueRebuild(ctx, r.queue, request.WorkspaceID, reindexArguments{ModelID: request.ModelID, SelectionGeneration: request.SelectionGeneration}); err != nil {
				return err
			}
		}
		if err := r.repository.CompleteModelRequest(ctx, request); err != nil {
			return err
		}
	}
	r.after = page.NextCursor
	return nil
}

// Run reconciles requested maintenance until cancellation, without startup work in HTTP graphs.
func (r *Reconciler) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := r.Reconcile(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
