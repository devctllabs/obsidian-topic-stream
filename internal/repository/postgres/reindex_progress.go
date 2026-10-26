package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// prepareRebuildProgress captures remaining required membership once, before the first seed batch.
// Seeded-and-acknowledged Items from an earlier operation are not recounted on a resumed model.
func prepareRebuildProgress(ctx context.Context, tx pgx.Tx, start domain.ModelTransitionStart) error {
	var seeded bool
	var after *uuid.UUID
	var generation uuid.UUID
	err := tx.QueryRow(ctx, `SELECT seed_complete,seed_after,generation FROM workspace_model_progress
WHERE workspace_id=$1 AND embedding_model_id=$2`, start.WorkspaceID, start.ModelID).Scan(&seeded, &after, &generation)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO retrieval_rebuild_progress(job_id,phase,updated_at)
VALUES($1,'preparing',clock_timestamp()) ON CONFLICT(job_id) DO NOTHING`, start.JobID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		_, err = tx.Exec(ctx, `UPDATE retrieval_rebuild_progress SET
phase=CASE WHEN NOT $2 THEN 'preparing' WHEN processed_items=total_items THEN 'catching_up' ELSE 'indexing' END,
updated_at=clock_timestamp() WHERE job_id=$1`, start.JobID, seeded)
		return err
	}
	statement := `INSERT INTO retrieval_rebuild_cohort(job_id,item_id)
SELECT $1::uuid,item_id FROM retrieval_projection_pending WHERE workspace_id=$2 AND embedding_model_id=$3 AND model_generation=$4`
	args := []any{start.JobID, start.WorkspaceID, start.ModelID, generation}
	if !seeded {
		statement = `INSERT INTO retrieval_rebuild_cohort(job_id,item_id)
SELECT $1::uuid,item_id FROM item_projection_state WHERE workspace_id=$2 AND ($5::uuid IS NULL OR item_id>$5)
UNION ALL
SELECT $1::uuid,item_id FROM retrieval_projection_pending WHERE workspace_id=$2 AND embedding_model_id=$3
 AND model_generation=$4 AND $5::uuid IS NOT NULL AND item_id<=$5`
		args = append(args, after)
	}
	tag, err = tx.Exec(ctx, statement, args...)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE retrieval_rebuild_progress SET total_items=$2::bigint,
phase=CASE WHEN NOT $3 THEN 'preparing' WHEN $2::bigint=0 THEN 'catching_up' ELSE 'indexing' END,updated_at=clock_timestamp()
WHERE job_id=$1`, start.JobID, tag.RowsAffected(), seeded)
	return err
}

func finishRebuildSeed(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE retrieval_rebuild_progress SET
phase=CASE WHEN processed_items=total_items THEN 'catching_up' ELSE 'indexing' END,updated_at=clock_timestamp()
WHERE job_id=$1 AND phase='preparing'`, jobID)
	return err
}

// cleanupRetiredRebuildCohorts preserves small historical counters and removes at most
// 1000 no-longer-countable members. It also runs when no Workspace requests reconciliation.
func (s *Store) cleanupRetiredRebuildCohorts(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `WITH retired AS (
 SELECT rebuild.job_id FROM retrieval_rebuilds rebuild JOIN jobs job ON job.id=rebuild.job_id
 WHERE rebuild.superseded OR job.status IN ('succeeded','dead','cancelled')
), obsolete AS (
 SELECT member.ctid FROM retired JOIN LATERAL (
  SELECT ctid FROM retrieval_rebuild_cohort WHERE job_id=retired.job_id LIMIT 1000
 ) member ON true LIMIT 1000
)
DELETE FROM retrieval_rebuild_cohort WHERE ctid IN(SELECT ctid FROM obsolete)`)
	return err
}

const jobProgressExpression = `(SELECT jsonb_build_object('phase',progress.phase,
'processedItems',progress.processed_items,'totalItems',progress.total_items,'updatedAt',progress.updated_at)
FROM retrieval_rebuild_progress progress WHERE progress.job_id=jobs.id)`

type storedJobProgress struct {
	Phase          string    `json:"phase"`
	ProcessedItems int64     `json:"processedItems"`
	TotalItems     *int64    `json:"totalItems"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// PrepareActivation persists the activating phase for the current owned model transition.
func (s *Store) PrepareActivation(ctx context.Context, cutover domain.ModelTransitionCutover) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockRetrievalState(ctx, tx, cutover.WorkspaceID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE retrieval_rebuild_progress progress SET phase='activating',updated_at=clock_timestamp()
FROM workspace_retrieval_state state JOIN retrieval_rebuilds rebuild ON rebuild.job_id=state.transition_job_id
JOIN workspace_model_progress model ON model.workspace_id=state.workspace_id AND model.embedding_model_id=state.transition_model_id
WHERE state.workspace_id=$1 AND state.transition_model_id=$2 AND state.transition_job_id=$3
 AND state.selection_generation=$4 AND state.transition_lease_owner=$5
 AND progress.job_id=rebuild.job_id AND NOT rebuild.superseded AND model.retained AND model.seed_complete
 AND model.generation=rebuild.model_generation AND progress.processed_items=progress.total_items
 AND ($5='' OR EXISTS(SELECT 1 FROM jobs WHERE id=$3 AND status='running' AND lease_owner=$5
  AND lease_expires_at>clock_timestamp() AND NOT cancellation_requested))`,
		cutover.WorkspaceID, cutover.ModelID, cutover.JobID, cutover.SelectionGeneration, cutover.LeaseOwner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return tx.Commit(ctx)
}

func scanJobProgress(row jobScanner) (domain.Job, error) {
	var job domain.Job
	var raw, transition json.RawMessage
	if err := row.Scan(append(jobScanTargets(&job), &raw, &transition)...); err != nil {
		return domain.Job{}, err
	}
	if len(raw) > 0 {
		var progress storedJobProgress
		if err := json.Unmarshal(raw, &progress); err != nil {
			return domain.Job{}, err
		}
		job.Progress = &domain.JobProgress{Phase: progress.Phase, ProcessedItems: progress.ProcessedItems, TotalItems: progress.TotalItems, UpdatedAt: progress.UpdatedAt}
	} else if job.Kind == "reindex-workspace" && !job.Status.Terminal() {
		job.Progress = &domain.JobProgress{Phase: "preparing", UpdatedAt: job.UpdatedAt}
	}
	if len(transition) > 0 {
		if err := json.Unmarshal(transition, &job.ModelTransition); err != nil {
			return domain.Job{}, err
		}
	}
	return job, nil
}
