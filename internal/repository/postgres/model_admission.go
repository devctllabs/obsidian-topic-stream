package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type modelJobInput struct {
	ModelID             uuid.UUID `json:"modelId"`
	SelectionGeneration int64     `json:"selectionGeneration"`
	Force               bool      `json:"force"`
}

func modelJobArguments(job domain.Job) (modelJobInput, bool) {
	var input modelJobInput
	valid := job.Kind == "reindex-workspace" && job.ArgumentsVersion == 3 && job.Queue == domain.JobQueueMaintenance &&
		json.Unmarshal(job.Arguments, &input) == nil && input.ModelID != uuid.Nil && input.SelectionGeneration >= 0
	return input, valid
}

func (s *Store) admitModelJob(ctx context.Context, job domain.Job, input modelJobInput) (domain.Job, bool, error) {
	tx, err := s.beginFeedChange(ctx, job.WorkspaceID)
	if err != nil {
		return domain.Job{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	latest, err := lockModelJobSelection(ctx, tx, job.WorkspaceID, input)
	if err != nil {
		return domain.Job{}, false, err
	}
	if latest != nil {
		previous, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id=$1`, *latest))
		if err != nil {
			return domain.Job{}, false, err
		}
		if !previous.Status.Terminal() || !input.Force {
			return previous, false, tx.Commit(ctx)
		}
	}
	stored, created, err := bindModelJob(ctx, tx, job, input)
	if err != nil {
		return domain.Job{}, false, err
	}
	return stored, created, tx.Commit(ctx)
}

func lockModelJobSelection(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, input modelJobInput) (*uuid.UUID, error) {
	var latest *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT state.latest_transition_job_id FROM workspace_retrieval_state state
 JOIN workspace_settings settings USING(workspace_id)
 WHERE state.workspace_id=$1 AND settings.embedding_model_id=$2 AND state.selection_generation=$3
 FOR UPDATE OF state`, workspaceID, input.ModelID, input.SelectionGeneration).Scan(&latest)
	if err == pgx.ErrNoRows {
		return nil, domain.ErrConflict
	}
	return latest, err
}

func bindModelJob(ctx context.Context, tx pgx.Tx, job domain.Job, input modelJobInput) (domain.Job, bool, error) {
	stored, created, err := enqueueJob(ctx, tx, job)
	if err != nil {
		return domain.Job{}, false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO model_transition_jobs(job_id,workspace_id,embedding_model_id,selection_generation)
 VALUES($1,$2,$3,$4) ON CONFLICT(job_id) DO NOTHING`, stored.ID, stored.WorkspaceID, input.ModelID, input.SelectionGeneration)
	if err != nil {
		return domain.Job{}, false, fmt.Errorf("bind model operation: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE workspace_retrieval_state SET latest_transition_job_id=$2,reconcile_requested=false,maintenance_requested=true
 WHERE workspace_id=$1`, job.WorkspaceID, stored.ID)
	return stored, created, err
}
