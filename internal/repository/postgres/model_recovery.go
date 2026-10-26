package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/jackc/pgx/v5"
)

// RetryModelTransition admits replacement work only for the latest failed selection operation.
func (s *Store) RetryModelTransition(ctx context.Context, source domain.Job, replacement domain.Job) (domain.Job, bool, error) {
	input, valid := modelJobArguments(replacement)
	if !valid || source.WorkspaceID != replacement.WorkspaceID {
		return domain.Job{}, false, domain.ErrJobNotRetryable
	}
	tx, err := s.beginFeedChange(ctx, replacement.WorkspaceID)
	if err != nil {
		return domain.Job{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockModelRetry(ctx, tx, source, input); err != nil {
		return domain.Job{}, false, err
	}
	input, err = retryModelInput(ctx, tx, source, input)
	if err != nil {
		return domain.Job{}, false, err
	}
	replacement.Arguments, err = json.Marshal(input)
	if err != nil {
		return domain.Job{}, false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE workspace_retrieval_state SET transition_model_id=NULL,transition_job_id=NULL,
 transition_lease_owner='' WHERE workspace_id=$1 AND transition_job_id=$2`, source.WorkspaceID, source.ID); err != nil {
		return domain.Job{}, false, err
	}
	stored, created, err := bindModelJob(ctx, tx, replacement, input)
	if err != nil {
		return domain.Job{}, false, err
	}
	return stored, created, tx.Commit(ctx)
}

func retryModelInput(ctx context.Context, tx pgx.Tx, source domain.Job, input modelJobInput) (modelJobInput, error) {
	current, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, source.ID, source.WorkspaceID))
	if err == pgx.ErrNoRows {
		return input, domain.ErrJobNotRetryable
	}
	if err != nil {
		return input, err
	}
	parsed, eligible := matchingModelRetry(current, input)
	if !eligible {
		return input, domain.ErrJobNotRetryable
	}
	var linked, started bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_transition_jobs WHERE job_id=$1 AND workspace_id=$2
 AND embedding_model_id=$3 AND selection_generation=$4),EXISTS(SELECT 1 FROM retrieval_rebuilds WHERE job_id=$1)`, source.ID, source.WorkspaceID, input.ModelID, input.SelectionGeneration).Scan(&linked, &started)
	if err != nil {
		return input, err
	}
	if !linked {
		return input, domain.ErrJobNotRetryable
	}
	// A new Retry Job resumes seeded work; an explicit rebuild not yet begun must still reset once.
	input.Force = parsed.Force && !started
	return input, nil
}

func lockModelRetry(ctx context.Context, tx pgx.Tx, source domain.Job, input modelJobInput) error {
	latest, err := lockModelJobSelection(ctx, tx, source.WorkspaceID, input)
	if errors.Is(err, domain.ErrConflict) {
		return domain.ErrJobNotRetryable
	}
	if err != nil {
		return err
	}
	if latest == nil || *latest != source.ID {
		return domain.ErrJobNotRetryable
	}
	return nil
}

func matchingModelRetry(current domain.Job, input modelJobInput) (modelJobInput, bool) {
	parsed, valid := modelJobArguments(current)
	terminal := current.Status == domain.JobStatusDead || current.Status == domain.JobStatusCancelled
	return parsed, valid && terminal && parsed.ModelID == input.ModelID && parsed.SelectionGeneration == input.SelectionGeneration
}
