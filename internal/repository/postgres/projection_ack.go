package postgres

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CompleteItemProjections acknowledges bounded model deliveries with one accepted flag per input.
// Entries share a Workspace and have unique Item/model keys. Any SQL error rolls back the batch.
func (s *Store) CompleteItemProjections(ctx context.Context, deliveries []domain.ItemProjectionDelivery) ([]bool, error) {
	if len(deliveries) == 0 {
		return nil, nil
	}
	args, err := projectionAckArguments(deliveries)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := lockFeedHead(ctx, tx, deliveries[0].WorkspaceID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, completeItemProjectionsSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("complete item projection batch: %w", err)
	}
	accepted, serving, err := scanProjectionAcknowledgements(rows, len(deliveries))
	if err != nil {
		return nil, err
	}
	if serving {
		if _, err := tx.Exec(ctx, `INSERT INTO feed_preparation_requests(workspace_id) VALUES ($1)
 ON CONFLICT(workspace_id) DO NOTHING`, deliveries[0].WorkspaceID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return accepted, nil
}

func scanProjectionAcknowledgements(rows pgx.Rows, count int) ([]bool, bool, error) {
	defer rows.Close()
	accepted := make([]bool, count)
	serving := false
	for rows.Next() {
		var position int
		var isServing bool
		if err := rows.Scan(&position, &isServing); err != nil {
			return nil, false, err
		}
		accepted[position-1] = true
		serving = serving || isServing
	}
	return accepted, serving, rows.Err()
}

func projectionAckArguments(deliveries []domain.ItemProjectionDelivery) ([]any, error) {
	if len(deliveries) > 1000 {
		return nil, fmt.Errorf("projection acknowledgement batch exceeds 1000 entries")
	}
	workspaceID := deliveries[0].WorkspaceID
	var items, models, revisions, generations, jobs []uuid.UUID
	var owners []string
	var attempts []int64
	keys := make(map[[2]uuid.UUID]bool, len(deliveries))
	for _, delivery := range deliveries {
		if delivery.WorkspaceID != workspaceID {
			return nil, fmt.Errorf("projection acknowledgement batch spans Workspaces")
		}
		key := [2]uuid.UUID{delivery.ItemID, delivery.ModelID}
		if keys[key] {
			return nil, fmt.Errorf("projection acknowledgement batch repeats an Item/model key")
		}
		keys[key] = true
		items = append(items, delivery.ItemID)
		models = append(models, delivery.ModelID)
		revisions = append(revisions, delivery.Revision)
		generations = append(generations, delivery.ModelGeneration)
		jobs = append(jobs, delivery.Execution.JobID)
		owners = append(owners, delivery.Execution.LeaseOwner)
		attempts = append(attempts, int64(delivery.Execution.Attempt))
	}
	return []any{workspaceID, items, models, revisions, generations, jobs, owners, attempts}, nil
}

const completeItemProjectionsSQL = `WITH input AS MATERIALIZED (
 SELECT item_id,model_id,revision,generation,job_id,owner,attempt,position
 FROM unnest($2::uuid[],$3::uuid[],$4::uuid[],$5::uuid[],$6::uuid[],$7::text[],$8::bigint[])
 WITH ORDINALITY AS batch(item_id,model_id,revision,generation,job_id,owner,attempt,position)
)
, completed AS (
DELETE FROM retrieval_projection_pending pending USING input
JOIN workspace_retrieval_state retrieval ON retrieval.workspace_id=$1
JOIN item_projection_state state ON state.workspace_id=retrieval.workspace_id
 AND state.item_id=input.item_id AND state.revision=input.revision
LEFT JOIN workspace_model_progress progress ON progress.workspace_id=retrieval.workspace_id AND progress.embedding_model_id=input.model_id
LEFT JOIN embedding_model_collections collection ON collection.embedding_model_id=input.model_id
WHERE pending.workspace_id=$1 AND pending.item_id=input.item_id AND pending.embedding_model_id=input.model_id
 AND pending.revision=input.revision AND pending.model_generation=input.generation
 AND COALESCE(progress.retained,true)
 AND COALESCE(progress.generation,'00000000-0000-0000-0000-000000000000'::uuid)=input.generation
 AND progress.collection_incarnation IS NOT DISTINCT FROM collection.incarnation
 AND (input.model_id=retrieval.serving_model_id OR
  (input.model_id=retrieval.transition_model_id AND EXISTS(SELECT 1 FROM jobs job
   WHERE job.id=retrieval.transition_job_id AND job.status IN ('queued','running','retry_wait') AND NOT job.cancellation_requested)))
 AND (input.job_id='00000000-0000-0000-0000-000000000000'::uuid OR EXISTS(SELECT 1 FROM jobs job
  WHERE job.id=input.job_id AND job.workspace_id=$1 AND job.status='running' AND job.lease_owner=input.owner
   AND job.attempt=input.attempt AND job.lease_expires_at>clock_timestamp() AND NOT job.cancellation_requested))
RETURNING input.position,pending.item_id,pending.embedding_model_id AS model_id,pending.model_generation,
 pending.embedding_model_id=retrieval.serving_model_id AS is_serving
), cohort_done AS (
 DELETE FROM retrieval_rebuild_cohort member USING completed,retrieval_rebuilds rebuild
 WHERE member.job_id=rebuild.job_id AND member.item_id=completed.item_id
 AND rebuild.workspace_id=$1 AND rebuild.embedding_model_id=completed.model_id
 AND rebuild.model_generation=completed.model_generation AND NOT rebuild.superseded
 AND EXISTS(SELECT 1 FROM jobs job WHERE job.id=rebuild.job_id
  AND job.status IN ('queued','running','retry_wait') AND NOT job.cancellation_requested)
 RETURNING member.job_id
), counted AS (
 SELECT job_id,count(*) AS amount FROM cohort_done GROUP BY job_id
), progress_updated AS (
 UPDATE retrieval_rebuild_progress progress SET processed_items=progress.processed_items+counted.amount,
 phase=CASE WHEN progress.processed_items+counted.amount=progress.total_items THEN 'catching_up' ELSE progress.phase END,
 updated_at=clock_timestamp() FROM counted WHERE progress.job_id=counted.job_id
 RETURNING progress.job_id
)
SELECT position,is_serving FROM completed`
