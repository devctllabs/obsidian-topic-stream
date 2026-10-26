package postgres

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ReconcileModelStates repairs bounded Workspace metadata and returns durable enqueue requests.
func (s *Store) ReconcileModelStates(ctx context.Context, query domain.ModelReconciliationQuery) (domain.ModelReconciliationPage, error) {
	if query.Limit < 1 || query.Limit > 1000 {
		return domain.ModelReconciliationPage{}, fmt.Errorf("model reconciliation limit must be between 1 and 1000")
	}
	if err := s.cleanupRetiredRebuildCohorts(ctx); err != nil {
		return domain.ModelReconciliationPage{}, err
	}
	ids, err := s.listModelMaintenanceWorkspaces(ctx, query)
	if err != nil {
		return domain.ModelReconciliationPage{}, err
	}
	page := domain.ModelReconciliationPage{Requests: make([]domain.ModelReconciliationRequest, 0, len(ids))}
	for _, id := range ids {
		request, needed, err := s.reconcileWorkspaceModel(ctx, id)
		if err != nil {
			return domain.ModelReconciliationPage{}, err
		}
		if needed {
			page.Requests = append(page.Requests, request)
		}
	}
	if len(ids) == query.Limit {
		page.NextCursor = &ids[len(ids)-1]
	}
	return page, nil
}

func (s *Store) listModelMaintenanceWorkspaces(ctx context.Context, query domain.ModelReconciliationQuery) ([]uuid.UUID, error) {
	statement := `SELECT workspace_id FROM workspace_retrieval_state WHERE maintenance_requested`
	args := []any{query.Limit}
	if query.AfterWorkspaceID != nil {
		statement += ` AND workspace_id>$2`
		args = append(args, *query.AfterWorkspaceID)
	}
	statement += ` ORDER BY workspace_id LIMIT $1`
	rows, err := s.pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

func (s *Store) reconcileWorkspaceModel(ctx context.Context, workspaceID uuid.UUID) (domain.ModelReconciliationRequest, bool, error) {
	tx, err := s.beginFeedChange(ctx, workspaceID)
	if err != nil {
		return domain.ModelReconciliationRequest{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockRetrievalState(ctx, tx, workspaceID); err != nil {
		return domain.ModelReconciliationRequest{}, false, err
	}
	if err := refreshModelIncarnations(ctx, tx, workspaceID); err != nil {
		return domain.ModelReconciliationRequest{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE workspace_retrieval_state state SET transition_model_id=NULL,
transition_job_id=NULL,transition_lease_owner='',updated_at=clock_timestamp()
FROM jobs job WHERE state.workspace_id=$1 AND job.id=state.transition_job_id
AND (job.status IN ('succeeded','dead','cancelled') OR job.cancellation_requested)`, workspaceID); err != nil {
		return domain.ModelReconciliationRequest{}, false, err
	}
	if err := cleanupModelPending(ctx, tx, workspaceID); err != nil {
		return domain.ModelReconciliationRequest{}, false, err
	}
	request, needed, err := reconcileModelRequest(ctx, tx, workspaceID)
	if err != nil {
		return domain.ModelReconciliationRequest{}, false, err
	}
	_, err = tx.Exec(ctx, `UPDATE workspace_retrieval_state state SET maintenance_requested=
state.reconcile_requested OR state.transition_job_id IS NOT NULL OR EXISTS(
 SELECT 1 FROM workspace_model_progress progress
 JOIN LATERAL(SELECT 1 FROM retrieval_projection_pending pending
  WHERE pending.workspace_id=progress.workspace_id AND pending.embedding_model_id=progress.embedding_model_id LIMIT 1) pending ON true
 WHERE progress.workspace_id=state.workspace_id AND NOT progress.retained LIMIT 1)
WHERE state.workspace_id=$1`, workspaceID)
	if err != nil {
		return domain.ModelReconciliationRequest{}, false, err
	}
	return request, needed, tx.Commit(ctx)
}

func reconcileModelRequest(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID) (domain.ModelReconciliationRequest, bool, error) {
	request := domain.ModelReconciliationRequest{WorkspaceID: workspaceID}
	var requested, initialized bool
	var modelID, servingID *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT COALESCE(settings.embedding_model_id,state.serving_model_id),state.serving_model_id,
state.selection_generation,state.reconcile_requested,COALESCE(progress.seed_complete,true)
FROM workspace_retrieval_state state LEFT JOIN workspace_settings settings ON settings.workspace_id=state.workspace_id
LEFT JOIN workspace_model_progress progress ON progress.workspace_id=state.workspace_id
 AND progress.embedding_model_id=COALESCE(settings.embedding_model_id,state.serving_model_id)
WHERE state.workspace_id=$1`, workspaceID).Scan(&modelID, &servingID, &request.SelectionGeneration, &requested, &initialized)
	if err != nil {
		return domain.ModelReconciliationRequest{}, false, err
	}
	needed := requested && modelID != nil && (servingID == nil || *modelID != *servingID || !initialized)
	if modelID != nil {
		request.ModelID = *modelID
	}
	if requested && !needed {
		if _, err := tx.Exec(ctx, `UPDATE workspace_retrieval_state SET reconcile_requested=false WHERE workspace_id=$1`, workspaceID); err != nil {
			return domain.ModelReconciliationRequest{}, false, err
		}
	}
	return request, needed, nil
}

// ModelRequestHasJob finds any durable request, including terminal cancellation or exhausted retry.
func (s *Store) ModelRequestHasJob(ctx context.Context, workspaceID uuid.UUID, key string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE workspace_id=$1 AND idempotency_key=$2 AND kind='reindex-workspace')`, workspaceID, key).Scan(&exists)
	return exists, err
}

// CompleteModelRequest acknowledges enqueue only while its selection generation remains current.
func (s *Store) CompleteModelRequest(ctx context.Context, request domain.ModelReconciliationRequest) error {
	_, err := s.pool.Exec(ctx, `UPDATE workspace_retrieval_state SET reconcile_requested=false
WHERE workspace_id=$1 AND selection_generation=$2`, request.WorkspaceID, request.SelectionGeneration)
	return err
}
