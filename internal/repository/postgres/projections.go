package postgres

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// LoadItemProjection loads the latest canonical projection and pending models in one consistent snapshot.
// False means the Item or its projection state does not exist.
func (s *Store) LoadItemProjection(
	ctx context.Context,
	workspaceID uuid.UUID,
	itemID uuid.UUID,
) (domain.ItemProjectionWork, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return domain.ItemProjectionWork{}, false, fmt.Errorf("begin projection snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	batch := &pgx.Batch{}
	batch.Queue(`SELECT `+itemColumns+` FROM items i
LEFT JOIN item_state s ON s.workspace_id=i.workspace_id AND s.item_id=i.id
LEFT JOIN item_feedback f ON f.workspace_id=i.workspace_id AND f.item_id=i.id
WHERE i.workspace_id=$1 AND i.id=$2`, workspaceID, itemID)
	batch.Queue(`SELECT revision,version,EXISTS(SELECT 1 FROM feed_publications p WHERE p.workspace_id=$1 AND p.item_id=$2)
FROM item_projection_state WHERE workspace_id=$1 AND item_id=$2`, workspaceID, itemID)
	batch.Queue(`SELECT correction.topic_id,correction.membership,t.correction_epoch
FROM topic_corrections correction JOIN topics t ON t.id=correction.topic_id AND t.workspace_id=$1
WHERE correction.item_id=$2 ORDER BY correction.topic_id`, workspaceID, itemID)
	batch.Queue(`SELECT model.id,model.name,model.endpoint,model.provider_model,
model.dimensions,model.enabled,model.initialization_status,model.failure_reason,model.version,
model.created_at,model.updated_at,pending.model_generation,COALESCE(collection.incarnation,'00000000-0000-0000-0000-000000000000'::uuid)
FROM retrieval_projection_pending pending
JOIN item_projection_state state USING(workspace_id,item_id,revision)
JOIN embedding_models model ON model.id=pending.embedding_model_id
JOIN workspace_retrieval_state retrieval ON retrieval.workspace_id=pending.workspace_id
AND (pending.embedding_model_id=retrieval.serving_model_id OR
 (pending.embedding_model_id=retrieval.transition_model_id AND EXISTS(SELECT 1 FROM jobs job
 WHERE job.id=retrieval.transition_job_id AND job.status IN ('queued','running','retry_wait') AND NOT job.cancellation_requested)))
LEFT JOIN workspace_model_progress progress ON progress.workspace_id=pending.workspace_id AND progress.embedding_model_id=pending.embedding_model_id
LEFT JOIN embedding_model_collections collection ON collection.embedding_model_id=pending.embedding_model_id
WHERE pending.workspace_id=$1 AND pending.item_id=$2 AND COALESCE(progress.retained,true)
AND pending.model_generation=COALESCE(progress.generation,'00000000-0000-0000-0000-000000000000'::uuid)
AND progress.collection_incarnation IS NOT DISTINCT FROM collection.incarnation
ORDER BY model.id`, workspaceID, itemID)
	results := tx.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()
	item, err := scanItem(results.QueryRow())
	if err == pgx.ErrNoRows {
		return domain.ItemProjectionWork{}, false, nil
	}
	if err != nil {
		return domain.ItemProjectionWork{}, false, fmt.Errorf("load projection item: %w", err)
	}
	var revision uuid.UUID
	var version int64
	var published bool
	if err := results.QueryRow().Scan(&revision, &version, &published); err == pgx.ErrNoRows {
		return domain.ItemProjectionWork{}, false, nil
	} else if err != nil {
		return domain.ItemProjectionWork{}, false, fmt.Errorf("load projection revision: %w", err)
	}
	corrections, err := scanProjectionCorrections(results.Query())
	if err != nil {
		return domain.ItemProjectionWork{}, false, err
	}
	models, err := scanProjectionModels(results.Query())
	if err != nil {
		return domain.ItemProjectionWork{}, false, err
	}
	if err := results.Close(); err != nil {
		return domain.ItemProjectionWork{}, false, fmt.Errorf("close projection snapshot batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ItemProjectionWork{}, false, fmt.Errorf("commit projection snapshot: %w", err)
	}
	return domain.ItemProjectionWork{
		Projection: domain.ItemProjection{
			Revision: revision, Version: version, Published: published, Item: item, Feedback: item.Feedback, TopicCorrections: corrections,
		},
		Models: models,
	}, true, nil
}

func scanProjectionCorrections(rows pgx.Rows, err error) ([]domain.TopicCorrectionProjection, error) {
	if err != nil {
		return nil, fmt.Errorf("query projection topic corrections: %w", err)
	}
	defer rows.Close()
	result := make([]domain.TopicCorrectionProjection, 0)
	for rows.Next() {
		var correction domain.TopicCorrectionProjection
		if err := rows.Scan(&correction.TopicID, &correction.Membership, &correction.CorrectionEpoch); err != nil {
			return nil, fmt.Errorf("scan projection topic correction: %w", err)
		}
		result = append(result, correction)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projection topic corrections: %w", err)
	}
	return result, nil
}

func scanProjectionModels(rows pgx.Rows, err error) ([]domain.ItemProjectionDestination, error) {
	if err != nil {
		return nil, fmt.Errorf("query pending projection models: %w", err)
	}
	defer rows.Close()
	models := make([]domain.ItemProjectionDestination, 0)
	for rows.Next() {
		var model domain.ItemProjectionDestination
		err := rows.Scan(&model.ID, &model.Name, &model.Endpoint, &model.ProviderModel, &model.Dimensions,
			&model.Enabled, &model.InitializationStatus, &model.FailureReason, &model.Version,
			&model.CreatedAt, &model.UpdatedAt, &model.Generation, &model.CollectionIncarnation)
		if err != nil {
			return nil, fmt.Errorf("scan pending projection model: %w", err)
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending projection models: %w", err)
	}
	return models, nil
}

// CompleteItemProjection acknowledges a delivery only when its revision still matches pending work.
func (s *Store) CompleteItemProjection(ctx context.Context, delivery domain.ItemProjectionDelivery) (bool, error) {
	accepted, err := s.CompleteItemProjections(ctx, []domain.ItemProjectionDelivery{delivery})
	if err != nil {
		return false, err
	}
	return accepted[0], nil
}

// ListPendingItemProjections returns a bounded page of pending projection commits for the Workspace.
func (s *Store) ListPendingItemProjections(
	ctx context.Context,
	workspaceID uuid.UUID,
	after *uuid.UUID,
	limit int,
) ([]domain.ItemProjectionCommit, error) {
	rows, err := s.pool.Query(ctx, `WITH destinations AS (
 SELECT serving_model_id AS model_id FROM workspace_retrieval_state WHERE workspace_id=$1
 UNION SELECT state.transition_model_id FROM workspace_retrieval_state state
 JOIN jobs job ON job.id=state.transition_job_id
 WHERE state.workspace_id=$1 AND job.status IN ('queued','running','retry_wait') AND NOT job.cancellation_requested
)
SELECT DISTINCT pending.item_id,pending.revision FROM destinations destination
LEFT JOIN workspace_model_progress progress ON progress.workspace_id=$1 AND progress.embedding_model_id=destination.model_id
LEFT JOIN embedding_model_collections collection ON collection.embedding_model_id=destination.model_id
CROSS JOIN LATERAL (
 SELECT pending.item_id,pending.revision FROM retrieval_projection_pending pending
 JOIN LATERAL (SELECT 1 FROM item_projection_state state WHERE state.workspace_id=$1
  AND state.item_id=pending.item_id AND state.revision=pending.revision LIMIT 1) current_state ON true
 WHERE pending.workspace_id=$1 AND pending.embedding_model_id=destination.model_id
 AND pending.item_id>=COALESCE($2::uuid,'00000000-0000-0000-0000-000000000000'::uuid)
 AND ($2::uuid IS NULL OR pending.item_id<>$2)
 AND pending.model_generation=COALESCE(progress.generation,'00000000-0000-0000-0000-000000000000'::uuid)
 ORDER BY pending.item_id LIMIT $3
) pending
WHERE COALESCE(progress.retained,true)
AND progress.collection_incarnation IS NOT DISTINCT FROM collection.incarnation
ORDER BY pending.item_id,pending.revision LIMIT $3`, workspaceID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending item projections: %w", err)
	}
	defer rows.Close()
	commits := make([]domain.ItemProjectionCommit, 0, limit)
	for rows.Next() {
		commit := domain.ItemProjectionCommit{WorkspaceID: workspaceID}
		if err := rows.Scan(&commit.ItemID, &commit.ProjectionRevision); err != nil {
			return nil, fmt.Errorf("scan pending item projection: %w", err)
		}
		commits = append(commits, commit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending item projections: %w", err)
	}
	return commits, nil
}

// ListPendingModelProjections returns pending Item projections for one Workspace and Embedding Model.
func (s *Store) ListPendingModelProjections(
	ctx context.Context,
	query domain.ModelProjectionQuery,
) ([]domain.ItemProjectionCommit, error) {
	rows, err := s.pool.Query(ctx, `SELECT pending.item_id,pending.revision
FROM retrieval_projection_pending pending
JOIN LATERAL (
 SELECT 1 FROM item_projection_state state
 WHERE state.workspace_id=pending.workspace_id AND state.item_id=pending.item_id
 AND state.revision=pending.revision LIMIT 1
) current_state ON true
WHERE pending.workspace_id=$1 AND pending.embedding_model_id=$2
AND pending.item_id>=COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000'::uuid)
AND ($4::uuid IS NULL OR pending.item_id<>$4)
ORDER BY pending.item_id
LIMIT $3`, query.WorkspaceID, query.ModelID, query.Limit, query.AfterItemID)
	if err != nil {
		return nil, fmt.Errorf("query pending model projections: %w", err)
	}
	defer rows.Close()
	commits := make([]domain.ItemProjectionCommit, 0, query.Limit)
	for rows.Next() {
		commit := domain.ItemProjectionCommit{WorkspaceID: query.WorkspaceID}
		if err := rows.Scan(&commit.ItemID, &commit.ProjectionRevision); err != nil {
			return nil, fmt.Errorf("scan pending model projection: %w", err)
		}
		commits = append(commits, commit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending model projections: %w", err)
	}
	return commits, nil
}

func lockRetrievalState(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT workspace_id FROM workspace_retrieval_state WHERE workspace_id=$1 FOR UPDATE`, workspaceID)
	return err
}

// ListPendingGlobalItemProjections inspects bounded pending keys before excluding active Jobs.
// Cursor progress includes active/stale Items, so a dense queued prefix cannot cause a full scan.
func (s *Store) ListPendingGlobalItemProjections(ctx context.Context, query domain.ProjectionRepairQuery) (domain.ProjectionRepairPage, error) {
	if query.Limit < 1 || query.Limit > 1000 {
		return domain.ProjectionRepairPage{}, fmt.Errorf("projection repair limit must be between 1 and 1000")
	}
	statement := `WITH destinations AS (
 SELECT workspace_id,serving_model_id AS model_id FROM workspace_retrieval_state WHERE serving_model_id IS NOT NULL
 UNION SELECT state.workspace_id,state.transition_model_id FROM workspace_retrieval_state state
 JOIN jobs job ON job.id=state.transition_job_id
 WHERE state.transition_model_id IS NOT NULL AND job.status IN ('queued','running','retry_wait') AND NOT job.cancellation_requested
), destination AS MATERIALIZED (
 SELECT workspace_id,model_id FROM destinations`
	var workspaceID, modelID, itemID *uuid.UUID
	if query.After != nil {
		workspaceID, modelID, itemID = &query.After.WorkspaceID, &query.After.ModelID, &query.After.ItemID
		if query.After.Exhausted {
			statement += ` WHERE (workspace_id,model_id)>($2::uuid,$3::uuid)`
		} else {
			statement += ` WHERE (workspace_id,model_id)>=($2::uuid,$3::uuid)`
		}
	}
	statement += ` ORDER BY workspace_id,model_id LIMIT 1
), inspected AS MATERIALIZED (
 SELECT d.workspace_id,d.model_id,p.item_id,p.revision,p.model_generation FROM destination d
 LEFT JOIN LATERAL (
  SELECT item_id,revision,model_generation FROM retrieval_projection_pending p
  WHERE p.workspace_id=d.workspace_id AND p.embedding_model_id=d.model_id
   AND (p.workspace_id,p.embedding_model_id,p.item_id)>
    (COALESCE($2::uuid,'00000000-0000-0000-0000-000000000000'::uuid),
     COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000'::uuid),
     COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000'::uuid))
  ORDER BY item_id LIMIT $1
 ) p ON true
)
SELECT k.workspace_id,k.model_id,k.item_id,state.revision,
 state.revision IS NOT NULL AND k.revision=state.revision
 AND COALESCE(progress.retained,true)
 AND k.model_generation=COALESCE(progress.generation,'00000000-0000-0000-0000-000000000000'::uuid)
 AND progress.collection_incarnation IS NOT DISTINCT FROM collection.incarnation
 AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.workspace_id=k.workspace_id
  AND j.idempotency_key='index-item:'||k.item_id::text||':'||state.revision::text
  AND j.status IN ('queued','running','retry_wait')) AS needs_enqueue
FROM inspected k LEFT JOIN item_projection_state state USING(workspace_id,item_id)
LEFT JOIN workspace_model_progress progress ON progress.workspace_id=k.workspace_id AND progress.embedding_model_id=k.model_id
LEFT JOIN embedding_model_collections collection ON collection.embedding_model_id=k.model_id
ORDER BY k.workspace_id,k.item_id`
	rows, err := s.pool.Query(ctx, statement, query.Limit, workspaceID, modelID, itemID)
	if err != nil {
		return domain.ProjectionRepairPage{}, fmt.Errorf("inspect pending projection keys: %w", err)
	}
	return scanProjectionRepairPage(rows, query.Limit)
}

func scanProjectionRepairPage(rows pgx.Rows, limit int) (domain.ProjectionRepairPage, error) {
	defer rows.Close()
	page := domain.ProjectionRepairPage{Commits: make([]domain.ItemProjectionCommit, 0, limit)}
	var last domain.ProjectionRepairCursor
	count := 0
	selected := false
	for rows.Next() {
		var inspectedID *uuid.UUID
		var revision *uuid.UUID
		var needsEnqueue bool
		if err := rows.Scan(&last.WorkspaceID, &last.ModelID, &inspectedID, &revision, &needsEnqueue); err != nil {
			return domain.ProjectionRepairPage{}, err
		}
		selected = true
		if inspectedID == nil {
			continue
		}
		last.ItemID = *inspectedID
		count++
		// needs_enqueue is true only when the joined current revision is non-NULL.
		if needsEnqueue {
			page.Commits = append(page.Commits, domain.ItemProjectionCommit{
				WorkspaceID: last.WorkspaceID, ItemID: last.ItemID, ProjectionRevision: *revision,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return domain.ProjectionRepairPage{}, err
	}
	if selected {
		last.Exhausted = count < limit
		page.NextCursor = &last
	}
	return page, nil
}
