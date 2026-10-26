package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/jackc/pgx/v5"
)

func selectSummaryRetry(ctx context.Context, tx pgx.Tx, request domain.SummaryAdmission) (summarySelection, error) {
	job, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE workspace_id=$1 AND id=(
 SELECT successor_job_id FROM item_summary_retries WHERE workspace_id=$1 AND source_job_id=$2)`, request.Input.WorkspaceID, *request.RetrySourceJobID))
	if err == nil {
		return summarySelection{job: job, preserveContext: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return summarySelection{}, fmt.Errorf("resolve summary retry binding: %w", err)
	}
	if err := validateSummaryRetry(ctx, tx, request); err != nil {
		return summarySelection{}, err
	}
	selection, found, err := activeSummarySelection(ctx, tx, request.Input.SummaryInputKey)
	if err != nil || found {
		return selection, err
	}
	selection, found, err = cachedSummarySelection(ctx, tx, request)
	if err != nil || found {
		return selection, err
	}
	job, created, err := enqueueJob(ctx, tx, request.Job)
	return summarySelection{job: job, created: created, updateShared: true}, err
}

func validateSummaryRetry(ctx context.Context, tx pgx.Tx, request domain.SummaryAdmission) error {
	job, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, request.Input.WorkspaceID, *request.RetrySourceJobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrJobNotFound
	}
	if err != nil {
		return fmt.Errorf("lock summary retry source: %w", err)
	}
	if !matchingSummaryJob(job, request.Input.SummaryInputKey) || (job.Status != domain.JobStatusDead && job.Status != domain.JobStatusCancelled) {
		return domain.ErrJobNotRetryable
	}
	_, ready, err := summaryForJob(ctx, tx, request.Input.WorkspaceID, job.ID)
	if err != nil {
		return fmt.Errorf("check summary retry result: %w", err)
	}
	if ready {
		return domain.ErrJobNotRetryable
	}
	return nil
}

func bindSummaryRetry(ctx context.Context, tx pgx.Tx, request domain.SummaryAdmission, job domain.Job) error {
	if request.RetrySourceJobID == nil {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO item_summary_retries(workspace_id,source_job_id,successor_job_id,created_at)
 VALUES($1,$2,$3,$4) ON CONFLICT(source_job_id) DO NOTHING`, request.Input.WorkspaceID, *request.RetrySourceJobID, job.ID, request.Job.CreatedAt)
	if err != nil {
		return fmt.Errorf("bind summary retry successor: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE digest_item_summaries SET job_id=$3
 WHERE workspace_id=$1 AND job_id=$2 AND summary_id IS NULL`, request.Input.WorkspaceID, *request.RetrySourceJobID, job.ID)
	if err != nil {
		return fmt.Errorf("advance missing Digest annotation retry: %w", err)
	}
	return nil
}
