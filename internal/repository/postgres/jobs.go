package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// JobRepository persists durable Jobs with atomic claims, idempotency, and lease fencing.
type JobRepository struct {
	store *Store
}

// Jobs returns durable Job operations backed by this Store's pool.
func (s *Store) Jobs() *JobRepository {
	return &JobRepository{store: s}
}

const jobColumns = `id,workspace_id,kind,queue,args_version,payload,idempotency_key,status,attempt,max_attempts,
available_at,lease_owner,lease_expires_at,cancellation_requested,reason,created_at,started_at,finished_at,updated_at`

// jobScanner decodes one SQL row without depending on a concrete pgx row type.
type jobScanner interface {
	// Scan decodes the current SQL row into the supplied destination pointers.
	Scan(destinations ...any) error
}

func scanJob(row jobScanner) (domain.Job, error) {
	var job domain.Job
	err := row.Scan(jobScanTargets(&job)...)
	return job, err
}

func jobScanTargets(job *domain.Job) []any {
	return []any{
		&job.ID, &job.WorkspaceID, &job.Kind, &job.Queue, &job.ArgumentsVersion, &job.Arguments, &job.IdempotencyKey,
		&job.Status, &job.Attempt, &job.MaxAttempts, &job.AvailableAt, &job.LeaseOwner, &job.LeaseExpiresAt,
		&job.CancellationRequested, &job.Reason, &job.CreatedAt, &job.StartedAt, &job.FinishedAt, &job.UpdatedAt,
	}
}

func liveJobExecution(job domain.Job, owner string, attempt int, now time.Time) bool {
	return job.Status == domain.JobStatusRunning && job.Attempt == attempt && !job.CancellationRequested &&
		job.LeaseOwner != nil && *job.LeaseOwner == owner && job.LeaseExpiresAt != nil && job.LeaseExpiresAt.After(now)
}

// Enqueue atomically deduplicates active jobs by workspace and idempotency key. The boolean reports
// whether the supplied Job, rather than an existing one, was stored.
func (r *JobRepository) Enqueue(ctx context.Context, job domain.Job) (domain.Job, bool, error) {
	if input, valid := modelJobArguments(job); valid {
		return r.store.admitModelJob(ctx, job, input)
	}
	return enqueueJob(ctx, r.store.pool, job)
}

// jobQuerier allows the same insertion policy inside an owning domain transaction.
type jobQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func enqueueJob(ctx context.Context, query jobQuerier, job domain.Job) (domain.Job, bool, error) {
	row := query.QueryRow(ctx, `
INSERT INTO jobs (id,workspace_id,kind,queue,args_version,payload,idempotency_key,status,attempt,max_attempts,available_at,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (workspace_id,idempotency_key)
    WHERE idempotency_key IS NOT NULL AND status IN ('queued','running','retry_wait')
DO UPDATE SET updated_at=jobs.updated_at
RETURNING `+jobColumns,
		job.ID, job.WorkspaceID, job.Kind, job.Queue, job.ArgumentsVersion, job.Arguments, job.IdempotencyKey,
		job.Status, job.Attempt, job.MaxAttempts, job.AvailableAt, job.CreatedAt, job.UpdatedAt)
	stored, err := scanJob(row)
	if err != nil {
		return domain.Job{}, false, fmt.Errorf("insert job: %w", err)
	}
	return stored, stored.ID == job.ID, nil
}

// Get returns a Job by ID and reports whether it exists.
func (r *JobRepository) Get(ctx context.Context, id uuid.UUID) (domain.Job, bool, error) {
	job, err := scanJobProgress(r.store.pool.QueryRow(ctx, `SELECT `+jobColumns+`,`+jobProgressExpression+`,`+jobModelTransitionExpression+` FROM jobs WHERE id=$1`, id))
	if err != nil {
		ok, wrapped := found(err)
		return domain.Job{}, ok, wrapped
	}
	return job, true, nil
}

// List returns a bounded page of Jobs matching Workspace, status, and cursor filters.
func (r *JobRepository) List(ctx context.Context, query domain.JobList) ([]domain.Job, error) {
	statement := `SELECT ` + jobColumns + `,` + jobProgressExpression + `,` + jobModelTransitionExpression + ` FROM jobs WHERE workspace_id=$1`
	args := []any{query.WorkspaceID}
	if query.Status != nil {
		args = append(args, *query.Status)
		statement += fmt.Sprintf(" AND status=$%d", len(args))
	}
	if query.Cursor != nil {
		args = append(args, query.Cursor.CreatedAt, query.Cursor.ID)
		statement += fmt.Sprintf(" AND (created_at,id)<($%d,$%d)", len(args)-1, len(args))
	}
	args = append(args, query.Limit)
	statement += fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d", len(args))
	rows, err := r.store.pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query jobs: %w", err)
	}
	defer rows.Close()
	result := make([]domain.Job, 0, query.Limit)
	for rows.Next() {
		job, err := scanJobProgress(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		result = append(result, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}
	return result, nil
}

// Claim recovers up to 100 exhausted expired leases without waiting for locked Jobs, then uses
// SKIP LOCKED so competing workers cannot claim the same eligible Job.
func (r *JobRepository) Claim(
	ctx context.Context,
	request domain.JobClaimRequest,
) (domain.JobClaim, error) {
	tx, err := r.store.pool.Begin(ctx)
	if err != nil {
		return domain.JobClaim{}, fmt.Errorf("begin job claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	expiredRows, err := tx.Query(ctx, `
WITH exhausted AS (
 SELECT id FROM jobs
 WHERE queue=$2 AND status='running' AND lease_expires_at < $1 AND attempt >= max_attempts
 ORDER BY lease_expires_at,id
 FOR UPDATE SKIP LOCKED LIMIT 100
)
UPDATE jobs AS job SET status='dead',reason='lease expired after final attempt',finished_at=$1,
lease_owner=NULL,lease_expires_at=NULL,updated_at=$1
FROM exhausted WHERE job.id=exhausted.id
RETURNING job.id,job.workspace_id`, request.Now, request.Queue)
	if err != nil {
		return domain.JobClaim{}, fmt.Errorf("recover exhausted jobs: %w", err)
	}
	changedJobs := make([]domain.JobIdentity, 0, 2)
	for expiredRows.Next() {
		var identity domain.JobIdentity
		if err := expiredRows.Scan(&identity.ID, &identity.WorkspaceID); err != nil {
			expiredRows.Close()
			return domain.JobClaim{}, fmt.Errorf("scan exhausted job: %w", err)
		}
		changedJobs = append(changedJobs, identity)
	}
	if err := expiredRows.Err(); err != nil {
		expiredRows.Close()
		return domain.JobClaim{}, fmt.Errorf("iterate exhausted jobs: %w", err)
	}
	expiredRows.Close()
	job, err := scanJob(tx.QueryRow(ctx, `
WITH candidate AS (
    SELECT id FROM jobs
    WHERE queue=$4 AND ((status IN ('queued','retry_wait') AND available_at <= $2)
		OR (status='running' AND lease_expires_at < $2 AND attempt < max_attempts))
    ORDER BY available_at,created_at,id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE jobs AS job SET status='running',attempt=job.attempt+1,lease_owner=$1,lease_expires_at=$3,
cancellation_requested=false,started_at=COALESCE(job.started_at,$2),finished_at=NULL,updated_at=$2
FROM candidate WHERE job.id=candidate.id
	RETURNING `+prefixedJobColumns("job"), request.Owner, request.Now, request.LeaseExpiresAt, request.Queue))
	if err != nil {
		if err == pgx.ErrNoRows {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return domain.JobClaim{}, fmt.Errorf("commit empty job claim: %w", commitErr)
			}
			return domain.JobClaim{ChangedJobs: changedJobs}, nil
		}
		return domain.JobClaim{}, fmt.Errorf("claim job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.JobClaim{}, fmt.Errorf("commit job claim: %w", err)
	}
	changedJobs = append(changedJobs, domain.JobIdentity{ID: job.ID, WorkspaceID: job.WorkspaceID})
	return domain.JobClaim{Job: job, ChangedJobs: changedJobs}, nil
}

// Heartbeat extends an owned lease and reports cancellation and continued lease ownership.
func (r *JobRepository) Heartbeat(ctx context.Context, heartbeat domain.JobHeartbeat) (bool, bool, error) {
	var requested bool
	err := r.store.pool.QueryRow(ctx, `
UPDATE jobs SET lease_expires_at=$4,updated_at=$3
WHERE id=$1 AND status='running' AND lease_owner=$2
RETURNING cancellation_requested`, heartbeat.ID, heartbeat.Owner, heartbeat.Now, heartbeat.LeaseExpiresAt).Scan(&requested)
	if err == pgx.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("heartbeat job: %w", err)
	}
	return requested, true, nil
}

// Finish succeeds only for the current lease owner. A pending cooperative cancellation overrides
// the worker's proposed terminal or retry status.
func (r *JobRepository) Finish(ctx context.Context, finish domain.JobFinish) (bool, error) {
	command, err := r.store.pool.Exec(ctx, `
UPDATE jobs SET
status=CASE WHEN cancellation_requested THEN 'cancelled' ELSE $3 END,
reason=CASE WHEN cancellation_requested THEN 'cancelled by user' ELSE $4 END,
available_at=$5,
finished_at=CASE WHEN cancellation_requested THEN $7::timestamptz ELSE $6::timestamptz END,
lease_owner=NULL,
lease_expires_at=NULL,cancellation_requested=false,updated_at=$7
WHERE id=$1 AND status='running' AND lease_owner=$2`, finish.ID, finish.Owner, finish.Status, finish.Reason,
		finish.AvailableAt, finish.FinishedAt, finish.UpdatedAt)
	if err != nil {
		return false, fmt.Errorf("finish job: %w", err)
	}
	return command.RowsAffected() == 1, nil
}

// RequestCancel immediately terminates queued work but only raises a cooperative flag for a running
// Job, whose owner must observe it through Heartbeat.
func (r *JobRepository) RequestCancel(ctx context.Context, id uuid.UUID, now time.Time) (domain.Job, bool, error) {
	job, err := scanJob(r.store.pool.QueryRow(ctx, `
UPDATE jobs SET
    cancellation_requested=CASE WHEN status='running' THEN true ELSE cancellation_requested END,
    status=CASE WHEN status IN ('queued','retry_wait') THEN 'cancelled' ELSE status END,
    reason=CASE WHEN status IN ('queued','retry_wait') THEN 'cancelled by user' ELSE reason END,
    finished_at=CASE WHEN status IN ('queued','retry_wait') THEN $2 ELSE finished_at END,
    updated_at=CASE WHEN status IN ('queued','retry_wait','running') THEN $2 ELSE updated_at END
WHERE id=$1 RETURNING `+jobColumns, id, now))
	if err != nil {
		ok, wrapped := found(err)
		return domain.Job{}, ok, wrapped
	}
	return job, true, nil
}

func prefixedJobColumns(prefix string) string {
	return prefix + `.id,` + prefix + `.workspace_id,` + prefix + `.kind,` + prefix + `.queue,` + prefix + `.args_version,` +
		prefix + `.payload,` + prefix + `.idempotency_key,` + prefix + `.status,` + prefix + `.attempt,` +
		prefix + `.max_attempts,` + prefix + `.available_at,` + prefix + `.lease_owner,` +
		prefix + `.lease_expires_at,` + prefix + `.cancellation_requested,` + prefix + `.reason,` +
		prefix + `.created_at,` + prefix + `.started_at,` + prefix + `.finished_at,` + prefix + `.updated_at`
}
