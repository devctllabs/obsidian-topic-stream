package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const embeddingModelColumns = `id,name,endpoint,provider_model,dimensions,enabled,initialization_status,failure_reason,version,created_at,updated_at`

// EmbeddingModels persists the model registry and its derived-collection initialization status.
type EmbeddingModels struct {
	store *Store
}

// CollectionStatusUpdate records initialization progress for a shared model collection.
type CollectionStatusUpdate struct {
	ModelID       uuid.UUID
	Collection    string
	Status        domain.EmbeddingModelStatus
	FailureReason *string
	UpdatedAt     time.Time
}

// EmbeddingModels returns registry operations backed by this Store's pool.
func (s *Store) EmbeddingModels() *EmbeddingModels {
	return &EmbeddingModels{store: s}
}

// EnsureDefaultEmbeddingModel inserts the installation's bootstrap model only when absent.
func (s *Store) EnsureDefaultEmbeddingModel(ctx context.Context, descriptor domain.EmbeddingModelDescriptor, now time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO embedding_models
(id,name,endpoint,provider_model,dimensions,enabled,initialization_status,version,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,true,'initializing',1,$6,$6)
ON CONFLICT (id) DO NOTHING`, domain.DefaultEmbeddingModelID, descriptor.Name,
		descriptor.Endpoint, descriptor.ProviderModel, descriptor.Dimensions, now)
	if err != nil {
		return fmt.Errorf("ensure default embedding model: %w", err)
	}
	return nil
}

// List returns the installation's Embedding Models in name and ID order.
func (r *EmbeddingModels) List(ctx context.Context) ([]domain.EmbeddingModel, error) {
	rows, err := r.store.pool.Query(ctx, `SELECT `+embeddingModelColumns+` FROM embedding_models ORDER BY lower(name),id`)
	if err != nil {
		return nil, fmt.Errorf("list embedding models: %w", err)
	}
	defer rows.Close()
	models := make([]domain.EmbeddingModel, 0)
	for rows.Next() {
		model, scanErr := scanEmbeddingModel(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan embedding model: %w", scanErr)
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate embedding models: %w", err)
	}
	return models, nil
}

// Get returns an Embedding Model and whether the registry entry exists.
func (r *EmbeddingModels) Get(ctx context.Context, id uuid.UUID) (domain.EmbeddingModel, bool, error) {
	model, err := scanEmbeddingModel(r.store.pool.QueryRow(ctx,
		`SELECT `+embeddingModelColumns+` FROM embedding_models WHERE id=$1`, id))
	present, err := found(err)
	if err != nil {
		return domain.EmbeddingModel{}, false, fmt.Errorf("get embedding model: %w", err)
	}
	return model, present, nil
}

// Create stores a model, returning domain.ErrConflict when its registry identity conflicts.
func (r *EmbeddingModels) Create(ctx context.Context, model domain.EmbeddingModel) error {
	_, err := r.store.pool.Exec(ctx, `INSERT INTO embedding_models
(id,name,endpoint,provider_model,dimensions,enabled,initialization_status,failure_reason,version,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, model.ID, model.Name, model.Endpoint,
		model.ProviderModel, model.Dimensions, model.Enabled, model.InitializationStatus,
		model.FailureReason, model.Version, model.CreatedAt, model.UpdatedAt)
	if isUniqueViolation(err) {
		return domain.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("insert embedding model: %w", err)
	}
	return nil
}

// Update compares expectedVersion before saving mutable fields; false reports a missing or stale entry.
func (r *EmbeddingModels) Update(ctx context.Context, model domain.EmbeddingModel, expectedVersion int) (bool, error) {
	command, err := r.store.pool.Exec(ctx, `UPDATE embedding_models
SET name=$3,endpoint=$4,enabled=$5,initialization_status=$6,failure_reason=$7,version=$8,updated_at=$9
WHERE id=$1 AND version=$2`, model.ID, expectedVersion, model.Name, model.Endpoint, model.Enabled,
		model.InitializationStatus, model.FailureReason, model.Version, model.UpdatedAt)
	if isUniqueViolation(err) {
		return false, domain.ErrConflict
	}
	if err != nil {
		return false, fmt.Errorf("update embedding model: %w", err)
	}
	return command.RowsAffected() == 1, nil
}

// RetrievalStatus returns the desired and serving Embedding Models and any transition Job.
func (r *EmbeddingModels) RetrievalStatus(ctx context.Context, workspaceID uuid.UUID) (domain.RetrievalStatus, error) {
	var status domain.RetrievalStatus
	var latest json.RawMessage
	err := r.store.pool.QueryRow(ctx, `SELECT settings.embedding_model_id,
CASE WHEN state.serving_incarnation=collection.incarnation THEN state.serving_model_id END,state.transition_job_id,`+latestModelTransitionExpression+`
FROM workspace_settings AS settings
LEFT JOIN workspace_retrieval_state AS state ON state.workspace_id=settings.workspace_id
LEFT JOIN embedding_model_collections collection ON collection.embedding_model_id=state.serving_model_id
LEFT JOIN workspace_model_progress progress ON progress.workspace_id=state.workspace_id AND progress.embedding_model_id=settings.embedding_model_id
LEFT JOIN model_transition_jobs binding ON binding.job_id=state.latest_transition_job_id
 AND binding.workspace_id=state.workspace_id AND binding.selection_generation=state.selection_generation
 AND binding.embedding_model_id=settings.embedding_model_id
LEFT JOIN jobs job ON job.id=binding.job_id
WHERE settings.workspace_id=$1`, workspaceID).Scan(
		&status.DesiredModelID, &status.ServingModelID, &status.TransitionJobID, &latest,
	)
	if err != nil {
		return domain.RetrievalStatus{}, fmt.Errorf("query workspace retrieval status: %w", err)
	}
	if len(latest) > 0 {
		if err := json.Unmarshal(latest, &status.LatestTransition); err != nil {
			return domain.RetrievalStatus{}, err
		}
	}
	return status, nil
}

// SetCollectionStatus persists initialization progress for the shared model collection.
func (r *EmbeddingModels) SetCollectionStatus(
	ctx context.Context,
	update CollectionStatusUpdate,
) error {
	_, err := r.store.pool.Exec(ctx, `INSERT INTO embedding_model_collections
(embedding_model_id,schema_version,collection_name,status,failure_reason,updated_at)
VALUES ($1,1,$2,$3,$4,$5)
ON CONFLICT (embedding_model_id) DO UPDATE SET
collection_name=EXCLUDED.collection_name,status=EXCLUDED.status,
failure_reason=EXCLUDED.failure_reason,updated_at=EXCLUDED.updated_at`,
		update.ModelID, update.Collection, update.Status, update.FailureReason, update.UpdatedAt)
	if err != nil {
		return fmt.Errorf("set embedding model collection status: %w", err)
	}
	return nil
}

// EnsureWorkspaceState publishes initial tracking under the canonical write lock. Existing content
// is initialized by bounded worker batches, never by a startup-wide pending insert.
func (r *EmbeddingModels) EnsureWorkspaceState(
	ctx context.Context,
	workspaceID uuid.UUID,
	servingModelID uuid.UUID,
	now time.Time,
) error {
	tx, err := r.store.beginFeedChange(ctx, workspaceID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO workspace_retrieval_state
(workspace_id,serving_model_id,updated_at) VALUES ($1,$2,$3)
ON CONFLICT (workspace_id) DO UPDATE SET
serving_model_id=COALESCE(workspace_retrieval_state.serving_model_id,EXCLUDED.serving_model_id),
updated_at=EXCLUDED.updated_at WHERE workspace_retrieval_state.serving_model_id IS NULL`, workspaceID, servingModelID, now)
	if err != nil {
		return fmt.Errorf("ensure workspace retrieval state: %w", err)
	}
	if err := lockRetrievalState(ctx, tx, workspaceID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO workspace_model_progress(workspace_id,embedding_model_id,selected_at,seed_complete,collection_incarnation)
SELECT state.workspace_id,state.serving_model_id,$2,
 NOT EXISTS(SELECT 1 FROM item_projection_state WHERE workspace_id=$1),collection.incarnation
FROM workspace_retrieval_state state LEFT JOIN embedding_model_collections collection ON collection.embedding_model_id=state.serving_model_id
WHERE state.workspace_id=$1 ON CONFLICT(workspace_id,embedding_model_id) DO NOTHING`, workspaceID, now)
	if err != nil {
		return err
	}
	if err := refreshModelIncarnations(ctx, tx, workspaceID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE workspace_retrieval_state state SET
serving_incarnation=COALESCE(state.serving_incarnation,CASE WHEN progress.seed_complete THEN progress.collection_incarnation END),
reconcile_requested=state.reconcile_requested OR (NOT progress.seed_complete AND state.serving_incarnation IS NULL),
maintenance_requested=state.maintenance_requested OR (NOT progress.seed_complete AND state.serving_incarnation IS NULL)
FROM workspace_model_progress progress WHERE state.workspace_id=$1 AND progress.workspace_id=$1
AND progress.embedding_model_id=state.serving_model_id
AND (state.serving_incarnation IS NULL AND (progress.collection_incarnation IS NOT NULL OR NOT progress.seed_complete))`, workspaceID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// embeddingModelScanner decodes one SQL row without depending on a concrete pgx row type.
type embeddingModelScanner interface {
	// Scan decodes the current SQL row into the supplied destination pointers.
	Scan(destinations ...any) error
}

func scanEmbeddingModel(row embeddingModelScanner) (domain.EmbeddingModel, error) {
	var model domain.EmbeddingModel
	err := row.Scan(
		&model.ID, &model.Name, &model.Endpoint, &model.ProviderModel, &model.Dimensions,
		&model.Enabled, &model.InitializationStatus, &model.FailureReason, &model.Version,
		&model.CreatedAt, &model.UpdatedAt,
	)
	return model, err
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}
