package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ResolveModel loads a registered Embedding Model or returns an error when it is absent.
func (s *Store) ResolveModel(ctx context.Context, modelID uuid.UUID) (domain.EmbeddingModel, error) {
	model, found, err := s.EmbeddingModels().Get(ctx, modelID)
	if err != nil {
		return domain.EmbeddingModel{}, err
	}
	if !found {
		return domain.EmbeddingModel{}, errors.New("embedding model not found")
	}
	return model, nil
}

// ResolveModelIndex captures a registered model's collection and expected incarnation before a Job starts.
func (s *Store) ResolveModelIndex(ctx context.Context, workspaceID, modelID uuid.UUID) (domain.RetrievalIndex, error) {
	model, err := s.ResolveModel(ctx, modelID)
	if err != nil {
		return domain.RetrievalIndex{}, err
	}
	index := retrievalIndex(workspaceID, model, domain.RetrievalIndexBuilding, model.UpdatedAt)
	err = s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT incarnation FROM embedding_model_collections WHERE embedding_model_id=$1),'00000000-0000-0000-0000-000000000000'::uuid)`, modelID).Scan(&index.CollectionIncarnation)
	return index, err
}

// GetActive returns the Workspace's serving retrieval generation and whether one exists.
func (s *Store) GetActive(ctx context.Context, workspaceID uuid.UUID) (domain.RetrievalIndex, bool, error) {
	index, err := s.activeRetrievalIndex(ctx, workspaceID)
	present, err := found(err)
	if err != nil {
		return domain.RetrievalIndex{}, false, fmt.Errorf("get serving retrieval model: %w", err)
	}
	return index, present, nil
}

// GetOrCreateActive returns an existing serving generation or initializes its catalog state.
func (s *Store) GetOrCreateActive(
	ctx context.Context,
	workspaceID uuid.UUID,
	modelID uuid.UUID,
	now time.Time,
) (domain.RetrievalIndex, error) {
	model, err := s.ResolveModel(ctx, modelID)
	if err != nil {
		return domain.RetrievalIndex{}, err
	}
	if err := s.EmbeddingModels().EnsureWorkspaceState(ctx, workspaceID, model.ID, now); err != nil {
		return domain.RetrievalIndex{}, err
	}
	return s.activeRetrievalIndex(ctx, workspaceID)
}

// Begin starts a model transition and returns its building generation.
func (s *Store) Begin(
	ctx context.Context,
	start domain.ModelTransitionStart,
) (domain.RetrievalIndex, error) {
	model, err := s.ResolveModel(ctx, start.ModelID)
	if err != nil {
		return domain.RetrievalIndex{}, err
	}
	tx, err := s.beginFeedChange(ctx, start.WorkspaceID)
	if err != nil {
		return domain.RetrievalIndex{}, fmt.Errorf("begin retrieval model transition transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `UPDATE workspace_retrieval_state
SET transition_model_id=$2,transition_job_id=$3,updated_at=$4,transition_lease_owner=$6,
reconcile_requested=false,maintenance_requested=true
WHERE workspace_id=$1 AND selection_generation=$5
AND (latest_transition_job_id IS NULL OR latest_transition_job_id=$3)
AND NOT EXISTS(SELECT 1 FROM workspace_settings settings WHERE settings.workspace_id=$1 AND settings.embedding_model_id<>$2)
AND ($6='' OR EXISTS(SELECT 1 FROM jobs WHERE id=$3 AND workspace_id=$1 AND status='running'
 AND lease_owner=$6 AND lease_expires_at>clock_timestamp() AND NOT cancellation_requested))
AND (
    (transition_model_id IS NULL AND transition_job_id IS NULL)
    OR (transition_model_id=$2 AND transition_job_id=$3)
)`, start.WorkspaceID, model.ID, start.JobID, start.StartedAt, start.SelectionGeneration, start.LeaseOwner)
	if err != nil {
		return domain.RetrievalIndex{}, fmt.Errorf("begin retrieval model transition: %w", err)
	}
	if command.RowsAffected() != 1 {
		return domain.RetrievalIndex{}, fmt.Errorf("model transition is obsolete or not owned: %w", domain.ErrConflict)
	}
	if err := retainModelProgress(ctx, tx, start); err != nil {
		return domain.RetrievalIndex{}, err
	}
	if err := cleanupModelPending(ctx, tx, start.WorkspaceID); err != nil {
		return domain.RetrievalIndex{}, err
	}
	if err := beginRebuildProgress(ctx, tx, start); err != nil {
		return domain.RetrievalIndex{}, err
	}
	if _, err := seedModelProjections(ctx, tx, domain.ModelProjectionSeedCommand{WorkspaceID: start.WorkspaceID, ModelID: model.ID, JobID: start.JobID, Limit: 1000, LeaseOwner: start.LeaseOwner}); err != nil {
		return domain.RetrievalIndex{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.RetrievalIndex{}, fmt.Errorf("commit retrieval model transition: %w", err)
	}
	index := retrievalIndex(start.WorkspaceID, model, domain.RetrievalIndexBuilding, start.StartedAt)
	index.SelectionGeneration = start.SelectionGeneration
	return index, nil
}

func beginRebuildProgress(ctx context.Context, tx pgx.Tx, start domain.ModelTransitionStart) error {
	if err := beginRebuildExecution(ctx, tx, start); err != nil {
		return err
	}
	return prepareRebuildProgress(ctx, tx, start)
}

func beginRebuildExecution(ctx context.Context, tx pgx.Tx, start domain.ModelTransitionStart) error {
	var superseded bool
	err := tx.QueryRow(ctx, `SELECT superseded FROM retrieval_rebuilds WHERE job_id=$1 AND workspace_id=$2 AND embedding_model_id=$3`, start.JobID, start.WorkspaceID, start.ModelID).Scan(&superseded)
	if err == nil {
		if superseded {
			return domain.ErrConflict
		}
		_, err = tx.Exec(ctx, `UPDATE retrieval_rebuilds SET active=true WHERE job_id=$1`, start.JobID)
		return err
	}
	if err != pgx.ErrNoRows {
		return err
	}
	if start.Force {
		if _, err := tx.Exec(ctx, `UPDATE workspace_model_progress SET generation=gen_random_uuid(),seed_after=NULL,seed_complete=false
WHERE workspace_id=$1 AND embedding_model_id=$2`, start.WorkspaceID, start.ModelID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE retrieval_rebuilds SET superseded=true,active=false WHERE workspace_id=$1 AND NOT superseded`, start.WorkspaceID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO retrieval_rebuilds(job_id,workspace_id,embedding_model_id,model_generation)
SELECT $1,$2,$3,generation FROM workspace_model_progress WHERE workspace_id=$2 AND embedding_model_id=$3 AND retained`, start.JobID, start.WorkspaceID, start.ModelID)
	return err
}

func retainModelProgress(ctx context.Context, tx pgx.Tx, start domain.ModelTransitionStart) error {
	// The serving destination is protected even after many interrupted selections.
	if _, err := tx.Exec(ctx, `INSERT INTO workspace_model_progress(workspace_id,embedding_model_id,selected_at)
SELECT workspace_id,serving_model_id,$2 FROM workspace_retrieval_state WHERE workspace_id=$1 AND serving_model_id IS NOT NULL
ON CONFLICT(workspace_id,embedding_model_id) DO NOTHING`, start.WorkspaceID, start.StartedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workspace_model_progress(workspace_id,embedding_model_id,selected_at,selection_order,collection_incarnation)
SELECT $1,$2,$3,$4,(SELECT incarnation FROM embedding_model_collections WHERE embedding_model_id=$2) ON CONFLICT(workspace_id,embedding_model_id) DO UPDATE SET
 generation=CASE WHEN workspace_model_progress.retained THEN workspace_model_progress.generation ELSE gen_random_uuid() END,
 seed_after=CASE WHEN workspace_model_progress.retained THEN workspace_model_progress.seed_after ELSE NULL END,
 seed_complete=workspace_model_progress.retained AND workspace_model_progress.seed_complete,
 collection_incarnation=CASE WHEN workspace_model_progress.retained THEN workspace_model_progress.collection_incarnation ELSE EXCLUDED.collection_incarnation END,
 retained=true,selected_at=EXCLUDED.selected_at,selection_order=EXCLUDED.selection_order`, start.WorkspaceID, start.ModelID, start.StartedAt, start.SelectionGeneration); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE workspace_model_progress SET retained=false
WHERE workspace_id=$1 AND embedding_model_id IN (
 SELECT p.embedding_model_id FROM workspace_model_progress p
 JOIN workspace_retrieval_state s USING(workspace_id)
 WHERE p.workspace_id=$1 AND p.retained AND p.embedding_model_id IS DISTINCT FROM s.serving_model_id
 ORDER BY p.selection_order DESC,p.selected_at DESC,p.embedding_model_id DESC OFFSET 2
)`, start.WorkspaceID)
	return err
}

func cleanupModelPending(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID) error {
	_, err := tx.Exec(ctx, `WITH discarded AS MATERIALIZED (
 SELECT pending.ctid FROM workspace_model_progress progress
 JOIN LATERAL (SELECT p.ctid FROM retrieval_projection_pending p
  WHERE p.workspace_id=progress.workspace_id AND p.embedding_model_id=progress.embedding_model_id
  LIMIT 1000) pending ON true
 WHERE progress.workspace_id=$1 AND NOT progress.retained LIMIT 1000
) DELETE FROM retrieval_projection_pending pending USING discarded WHERE pending.ctid=discarded.ctid`, workspaceID)
	return err
}

// SeedModelProjections resumes a bounded initialization batch under the retrieval-state lock.
func (s *Store) SeedModelProjections(ctx context.Context, command domain.ModelProjectionSeedCommand) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockRetrievalState(ctx, tx, command.WorkspaceID); err != nil {
		return false, err
	}
	if err := cleanupModelPending(ctx, tx, command.WorkspaceID); err != nil {
		return false, err
	}
	done, err := seedModelProjections(ctx, tx, command)
	if err != nil {
		return false, err
	}
	return done, tx.Commit(ctx)
}

func seedModelProjections(ctx context.Context, tx pgx.Tx, command domain.ModelProjectionSeedCommand) (bool, error) {
	if command.Limit < 1 || command.Limit > 10000 {
		return false, errors.New("projection seed batch must be between 1 and 10000")
	}
	var after *uuid.UUID
	var done bool
	var generation uuid.UUID
	err := tx.QueryRow(ctx, `SELECT progress.seed_after,progress.seed_complete,progress.generation FROM retrieval_rebuilds rebuild
JOIN workspace_retrieval_state state ON state.workspace_id=rebuild.workspace_id
AND state.transition_job_id=rebuild.job_id AND state.transition_model_id=rebuild.embedding_model_id
JOIN workspace_model_progress progress ON progress.workspace_id=rebuild.workspace_id
AND progress.embedding_model_id=rebuild.embedding_model_id AND progress.generation=rebuild.model_generation AND progress.retained
WHERE rebuild.workspace_id=$1 AND rebuild.job_id=$2 AND rebuild.embedding_model_id=$3 AND NOT rebuild.superseded
AND state.transition_lease_owner=$4
AND ($4='' OR EXISTS(SELECT 1 FROM jobs WHERE id=$2 AND lease_owner=$4 AND status='running'
 AND lease_expires_at>clock_timestamp() AND NOT cancellation_requested))
FOR UPDATE OF rebuild`, command.WorkspaceID, command.JobID, command.ModelID, command.LeaseOwner).Scan(&after, &done, &generation)
	if err == pgx.ErrNoRows {
		return false, domain.ErrConflict
	}
	if err != nil || done {
		return done, err
	}
	err = tx.QueryRow(ctx, `WITH batch AS MATERIALIZED (
 SELECT item_id,revision FROM item_projection_state WHERE workspace_id=$1
 AND item_id>=COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000'::uuid)
 AND ($3::uuid IS NULL OR item_id<>$3)
 ORDER BY item_id LIMIT $4
), scheduled AS (
 INSERT INTO retrieval_projection_pending(workspace_id,item_id,embedding_model_id,revision,updated_at,model_generation)
 SELECT $1,item_id,$2,revision,clock_timestamp(),$5 FROM batch
 ON CONFLICT(workspace_id,item_id,embedding_model_id) DO UPDATE SET revision=EXCLUDED.revision,updated_at=EXCLUDED.updated_at,model_generation=EXCLUDED.model_generation
) UPDATE workspace_model_progress SET seed_after=COALESCE((SELECT item_id FROM batch ORDER BY item_id DESC LIMIT 1),seed_after),
seed_complete=(SELECT count(*) FROM batch)<$4 WHERE workspace_id=$1 AND embedding_model_id=$2 RETURNING seed_complete`, command.WorkspaceID, command.ModelID, after, command.Limit, generation).Scan(&done)
	if err == nil && done {
		err = finishRebuildSeed(ctx, tx, command.JobID)
	}
	return done, err
}

// Activate cuts over serving retrieval after the model generation is ready.
func (s *Store) Activate(ctx context.Context, cutover domain.ModelTransitionCutover) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := lockFeedHead(ctx, tx, cutover.WorkspaceID); err != nil {
		return err
	}
	if err := lockRetrievalState(ctx, tx, cutover.WorkspaceID); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `UPDATE workspace_retrieval_state
SET serving_model_id=transition_model_id,
serving_incarnation=(SELECT incarnation FROM embedding_model_collections WHERE embedding_model_id=$3),
transition_model_id=NULL,transition_job_id=NULL,transition_lease_owner='',updated_at=$2
WHERE workspace_id=$1 AND transition_model_id=$3 AND transition_job_id=$4
  AND selection_generation=$5 AND transition_lease_owner=$6
  AND NOT EXISTS(SELECT 1 FROM workspace_settings settings WHERE settings.workspace_id=$1 AND settings.embedding_model_id<>$3)
  AND ($6='' OR EXISTS(SELECT 1 FROM jobs WHERE id=$4 AND lease_owner=$6 AND status='running'
      AND lease_expires_at>clock_timestamp() AND NOT cancellation_requested))
  AND EXISTS(SELECT 1 FROM retrieval_rebuilds rebuild JOIN workspace_model_progress progress
      ON progress.workspace_id=rebuild.workspace_id AND progress.embedding_model_id=rebuild.embedding_model_id
      AND progress.generation=rebuild.model_generation AND progress.retained
      WHERE rebuild.job_id=$4 AND progress.seed_complete AND NOT rebuild.superseded
      AND progress.collection_incarnation IS NOT DISTINCT FROM
       (SELECT incarnation FROM embedding_model_collections WHERE embedding_model_id=progress.embedding_model_id))
  AND NOT EXISTS (
      SELECT 1 FROM retrieval_projection_pending pending
      WHERE pending.workspace_id=workspace_retrieval_state.workspace_id
        AND pending.embedding_model_id=workspace_retrieval_state.transition_model_id
  )`, cutover.WorkspaceID, cutover.ActivatedAt, cutover.ModelID, cutover.JobID, cutover.SelectionGeneration, cutover.LeaseOwner)
	if err != nil {
		return fmt.Errorf("activate retrieval model: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("retrieval model transition is not current, owned or complete: %w", domain.ErrConflict)
	}
	if _, err := tx.Exec(ctx, `UPDATE retrieval_rebuilds SET active=false WHERE job_id=$1`, cutover.JobID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE retrieval_rebuild_progress SET phase='completed',updated_at=clock_timestamp() WHERE job_id=$1`, cutover.JobID); err != nil {
		return err
	}
	if err := requestFeedPreparation(ctx, tx, domain.FeedPreparationRequest{WorkspaceID: cutover.WorkspaceID, Invalidate: true, At: cutover.ActivatedAt}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Fail clears the matching failed transition without discarding the serving generation.
func (s *Store) Fail(ctx context.Context, failure domain.RetrievalIndexFailure) error {
	_, err := s.pool.Exec(ctx, `UPDATE workspace_retrieval_state
SET transition_model_id=NULL,transition_job_id=NULL,transition_lease_owner='',updated_at=$4
WHERE workspace_id=$1 AND transition_model_id=$2 AND transition_job_id=$3
AND selection_generation=$5 AND transition_lease_owner=$6`,
		failure.WorkspaceID, failure.ModelID, failure.JobID, failure.FailedAt, failure.SelectionGeneration, failure.LeaseOwner)
	if err != nil {
		return fmt.Errorf("clear failed retrieval model transition: %w", err)
	}
	return nil
}

func (s *Store) activeRetrievalIndex(ctx context.Context, workspaceID uuid.UUID) (domain.RetrievalIndex, error) {
	var model domain.EmbeddingModel
	var activatedAt time.Time
	var selectionGeneration int64
	var desiredModelID uuid.UUID
	var incarnation uuid.UUID
	var initialized bool
	err := s.pool.QueryRow(ctx, `SELECT model.id,model.name,model.endpoint,model.provider_model,
model.dimensions,model.enabled,model.initialization_status,model.failure_reason,model.version,
model.created_at,model.updated_at,state.updated_at,state.selection_generation,COALESCE(settings.embedding_model_id,state.serving_model_id),
COALESCE(collection.incarnation,'00000000-0000-0000-0000-000000000000'::uuid),
COALESCE(state.serving_incarnation=collection.incarnation,false)
FROM workspace_retrieval_state AS state
JOIN embedding_models AS model ON model.id=state.serving_model_id
LEFT JOIN workspace_settings settings ON settings.workspace_id=state.workspace_id
LEFT JOIN embedding_model_collections collection ON collection.embedding_model_id=state.serving_model_id
WHERE state.workspace_id=$1`, workspaceID).Scan(
		&model.ID, &model.Name, &model.Endpoint, &model.ProviderModel, &model.Dimensions,
		&model.Enabled, &model.InitializationStatus, &model.FailureReason, &model.Version,
		&model.CreatedAt, &model.UpdatedAt, &activatedAt, &selectionGeneration, &desiredModelID, &incarnation, &initialized,
	)
	if err != nil {
		return domain.RetrievalIndex{}, err
	}
	index := retrievalIndex(workspaceID, model, domain.RetrievalIndexActive, activatedAt)
	index.ActivatedAt = &activatedAt
	index.SelectionGeneration = selectionGeneration
	index.DesiredModelID = desiredModelID
	index.CollectionIncarnation = incarnation
	index.Initialized = initialized
	return index, nil
}

func retrievalIndex(
	workspaceID uuid.UUID,
	model domain.EmbeddingModel,
	status domain.RetrievalIndexStatus,
	now time.Time,
) domain.RetrievalIndex {
	return domain.RetrievalIndex{
		WorkspaceID: workspaceID, ModelID: model.ID, Generation: model.Version,
		Collection: vectorindex.CollectionName(model.ID),
		Embedding: domain.EmbeddingSettings{
			Endpoint: model.Endpoint, Model: model.ProviderModel, Dimensions: model.Dimensions,
		},
		Status: status, CreatedAt: now,
	}
}
