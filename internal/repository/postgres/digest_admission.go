package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AdmitDigestBuild atomically binds an identity to shared active generation work.
func (s *Store) AdmitDigestBuild(ctx context.Context, request domain.DigestBuildRequest) (domain.DigestBuildAdmission, error) {
	if request.RequestID == uuid.Nil || request.Job.WorkspaceID != request.WorkspaceID {
		return domain.DigestBuildAdmission{}, domain.ErrConflict
	}
	if request.Kind != domain.DigestBuildManual && request.Kind != domain.DigestBuildRetry {
		return domain.DigestBuildAdmission{}, domain.ErrConflict
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.DigestBuildAdmission{}, fmt.Errorf("begin digest admission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var deletedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT deleted_at FROM digest_definitions WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, request.WorkspaceID, request.DigestID).Scan(&deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DigestBuildAdmission{}, domain.ErrDigestNotFound
	}
	if err != nil {
		return domain.DigestBuildAdmission{}, fmt.Errorf("lock digest definition: %w", err)
	}
	admission, err := admitBoundDigestBuild(ctx, tx, request, deletedAt != nil)
	if err != nil {
		return domain.DigestBuildAdmission{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.DigestBuildAdmission{}, fmt.Errorf("commit digest admission: %w", err)
	}
	return admission, nil
}

func admitBoundDigestBuild(ctx context.Context, tx pgx.Tx, request domain.DigestBuildRequest, deleted bool) (domain.DigestBuildAdmission, error) {
	job, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE workspace_id=$1 AND id=(
 SELECT job_id FROM digest_build_requests WHERE workspace_id=$1 AND digest_id=$2 AND request_kind=$4 AND identity_id=$3
)`, request.WorkspaceID, request.DigestID, request.RequestID, request.Kind))
	if err == nil {
		return domain.DigestBuildAdmission{Job: job}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.DigestBuildAdmission{}, fmt.Errorf("resolve digest binding: %w", err)
	}
	if deleted {
		if request.Kind == domain.DigestBuildRetry {
			return domain.DigestBuildAdmission{}, domain.ErrJobNotRetryable
		}
		return domain.DigestBuildAdmission{}, domain.ErrDigestNotFound
	}
	if request.Kind == domain.DigestBuildRetry {
		candidate, err := retryDigestCandidate(ctx, tx, request)
		if err != nil {
			return domain.DigestBuildAdmission{}, err
		}
		request.Job = candidate
	}
	job, created, err := admitActiveDigestJob(ctx, tx, request.Job)
	if err != nil {
		return domain.DigestBuildAdmission{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO digest_build_requests(workspace_id,digest_id,request_kind,identity_id,job_id,created_at)
 VALUES($1,$2,$6,$3,$4,$5)`, request.WorkspaceID, request.DigestID, request.RequestID, job.ID, request.Job.CreatedAt, request.Kind)
	if err != nil {
		return domain.DigestBuildAdmission{}, fmt.Errorf("bind digest build request: %w", err)
	}
	return domain.DigestBuildAdmission{Job: job, Created: created}, nil
}

func retryDigestCandidate(ctx context.Context, tx pgx.Tx, request domain.DigestBuildRequest) (domain.Job, error) {
	source, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, request.WorkspaceID, request.RequestID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, domain.ErrJobNotFound
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("lock digest retry source: %w", err)
	}
	if source.Kind != "generate-digest" || (source.Status != domain.JobStatusDead && source.Status != domain.JobStatusCancelled) {
		return domain.Job{}, domain.ErrJobNotRetryable
	}
	var arguments struct {
		DigestID uuid.UUID `json:"digestId"`
	}
	if err := json.Unmarshal(source.Arguments, &arguments); err != nil || arguments.DigestID != request.DigestID || source.ArgumentsVersion != request.Job.ArgumentsVersion {
		return domain.Job{}, domain.ErrJobNotRetryable
	}
	if err := rejectCommittedDigestRetry(ctx, tx, source); err != nil {
		return domain.Job{}, err
	}
	candidate := request.Job
	candidate.Arguments = source.Arguments
	return candidate, nil
}

func admitActiveDigestJob(ctx context.Context, tx pgx.Tx, candidate domain.Job) (domain.Job, bool, error) {
	job, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE workspace_id=$1 AND idempotency_key=$2
 AND status IN ('queued','running','retry_wait')`, candidate.WorkspaceID, candidate.IdempotencyKey))
	if err == nil {
		return job, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, false, fmt.Errorf("resolve active digest job: %w", err)
	}
	return enqueueJob(ctx, tx, candidate)
}

func rejectCommittedDigestRetry(ctx context.Context, tx pgx.Tx, source domain.Job) error {
	var saved bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM digest_issues WHERE workspace_id=$1 AND generation_job_id=$2)`, source.WorkspaceID, source.ID).Scan(&saved); err != nil {
		return fmt.Errorf("check digest retry result: %w", err)
	}
	if saved {
		return domain.ErrJobNotRetryable
	}
	return nil
}
