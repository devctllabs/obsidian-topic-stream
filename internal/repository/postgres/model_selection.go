package postgres

import (
	"context"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type modelSelection struct {
	workspaceID, modelID uuid.UUID
	at                   time.Time
}

func validateModelSelectionChange(ctx context.Context, tx pgx.Tx, previousModelID, modelID uuid.UUID) error {
	if previousModelID == modelID {
		return nil
	}
	var selectable bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM embedding_models WHERE id=$1 AND enabled AND initialization_status='ready')`, modelID).Scan(&selectable); err != nil {
		return err
	}
	return domain.CheckCondition("embeddingModelId", domain.ValidationReady, selectable)
}

func replaceModelSelection(ctx context.Context, tx pgx.Tx, selection modelSelection) error {
	workspaceID, at := selection.workspaceID, selection.at
	var generation int64
	if err := tx.QueryRow(ctx, `INSERT INTO workspace_retrieval_state(workspace_id,selection_generation,updated_at,reconcile_requested,maintenance_requested)
VALUES($1,1,$2,true,true) ON CONFLICT(workspace_id) DO UPDATE SET
 selection_generation=workspace_retrieval_state.selection_generation+1,
 transition_model_id=NULL,transition_job_id=NULL,latest_transition_job_id=NULL,transition_lease_owner='',updated_at=EXCLUDED.updated_at,
 reconcile_requested=true,maintenance_requested=true
RETURNING selection_generation`, workspaceID, at).Scan(&generation); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs SET
 cancellation_requested=status='running',
 status=CASE WHEN status='running' THEN status ELSE 'cancelled' END,
 reason='superseded model selection',
 finished_at=CASE WHEN status='running' THEN finished_at ELSE $2 END,updated_at=$2
WHERE workspace_id=$1 AND kind='reindex-workspace' AND status IN ('queued','running','retry_wait')`, workspaceID, at); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE retrieval_rebuilds SET superseded=true,active=false WHERE workspace_id=$1 AND NOT superseded`, workspaceID); err != nil {
		return err
	}
	return retainModelProgress(ctx, tx, domain.ModelTransitionStart{
		WorkspaceID: workspaceID, ModelID: selection.modelID, StartedAt: at, SelectionGeneration: generation,
	})
}
