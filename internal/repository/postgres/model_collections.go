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

// WithModelCollectionLock coordinates marker initialization across processes without Workspace locks.
func (s *Store) WithModelCollectionLock(ctx context.Context, modelID uuid.UUID, action func(context.Context) error) (result error) {
	connection, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer connection.Release()
	key := "topic-stream-model-collection:" + modelID.String()
	if _, err := connection.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, key); err != nil {
		return errors.Join(err, ctx.Err())
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var unlocked bool
		err := connection.QueryRow(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key).Scan(&unlocked)
		if err == nil && !unlocked {
			err = fmt.Errorf("model collection lock was not held")
		}
		if err != nil {
			// A pooled session must never retain an advisory lock after cleanup failure.
			_ = connection.Conn().Close(cleanup)
		}
		result = errors.Join(result, err)
	}()
	return action(ctx)
}

// ObserveModelCollection records a shared incarnation and refreshes the caller's Workspace checkpoint.
func (s *Store) ObserveModelCollection(ctx context.Context, observation domain.ModelCollectionObservation) error {
	if observation.Incarnation == uuid.Nil {
		return fmt.Errorf("collection incarnation is required")
	}
	if err := s.observeSharedCollection(ctx, observation); err != nil {
		return err
	}
	if observation.WorkspaceID != uuid.Nil {
		_, _, err := s.reconcileWorkspaceModel(ctx, observation.WorkspaceID)
		return err
	}
	return nil
}

func (s *Store) observeSharedCollection(ctx context.Context, observation domain.ModelCollectionObservation) error {
	var current *uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT incarnation FROM embedding_model_collections WHERE embedding_model_id=$1`, observation.ModelID).Scan(&current)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if current != nil && *current == observation.Incarnation {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := recordCollectionIncarnation(ctx, tx, observation); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func recordCollectionIncarnation(ctx context.Context, tx pgx.Tx, observation domain.ModelCollectionObservation) error {
	var current *uuid.UUID
	if _, err := tx.Exec(ctx, `INSERT INTO embedding_model_collections(embedding_model_id,schema_version,collection_name,status,updated_at)
VALUES($1,1,$2,'ready',clock_timestamp()) ON CONFLICT(embedding_model_id) DO NOTHING`, observation.ModelID, vectorindex.CollectionName(observation.ModelID)); err != nil {
		return err
	}
	err := tx.QueryRow(ctx, `SELECT incarnation FROM embedding_model_collections WHERE embedding_model_id=$1 FOR UPDATE`, observation.ModelID).Scan(&current)
	if err != nil {
		return err
	}
	if current != nil && *current == observation.Incarnation {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE embedding_model_collections SET incarnation=$2,status='ready',updated_at=clock_timestamp() WHERE embedding_model_id=$1`, observation.ModelID, observation.Incarnation); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE workspace_retrieval_state SET maintenance_requested=true WHERE workspace_id IN
(SELECT workspace_id FROM workspace_model_progress WHERE embedding_model_id=$1 AND retained
AND collection_incarnation IS DISTINCT FROM $2::uuid)`, observation.ModelID, observation.Incarnation)
	if err != nil {
		return err
	}
	return nil
}

func refreshModelIncarnations(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT progress.embedding_model_id,progress.collection_incarnation,collection.incarnation,progress.seed_complete,
progress.embedding_model_id=state.serving_model_id,
progress.embedding_model_id=COALESCE(settings.embedding_model_id,state.serving_model_id)
FROM workspace_model_progress progress JOIN embedding_model_collections collection USING(embedding_model_id)
JOIN workspace_retrieval_state state ON state.workspace_id=progress.workspace_id
LEFT JOIN workspace_settings settings ON settings.workspace_id=progress.workspace_id
WHERE progress.workspace_id=$1 AND progress.retained AND collection.incarnation IS NOT NULL
AND progress.collection_incarnation IS DISTINCT FROM collection.incarnation`, workspaceID)
	if err != nil {
		return err
	}
	bindings, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (modelCollectionBinding, error) {
		var b modelCollectionBinding
		err := row.Scan(&b.model, &b.old, &b.incarnation, &b.complete, &b.serving, &b.desired)
		return b, err
	})
	if err != nil {
		return err
	}
	for _, b := range bindings {
		if err := bindModelCollection(ctx, tx, workspaceID, b); err != nil {
			return err
		}
	}
	return nil
}

type modelCollectionBinding struct {
	model                      uuid.UUID
	old                        *uuid.UUID
	incarnation                uuid.UUID
	complete, serving, desired bool
}

func bindModelCollection(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, b modelCollectionBinding) error {
	if b.old == nil {
		if _, err := tx.Exec(ctx, `UPDATE workspace_model_progress SET collection_incarnation=$3 WHERE workspace_id=$1 AND embedding_model_id=$2`, workspaceID, b.model, b.incarnation); err != nil {
			return err
		}
		if b.complete && b.serving {
			if _, err := tx.Exec(ctx, `UPDATE workspace_retrieval_state SET serving_incarnation=$2 WHERE workspace_id=$1 AND serving_incarnation IS NULL`, workspaceID, b.incarnation); err != nil {
				return err
			}
		}
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE workspace_model_progress SET collection_incarnation=$3,generation=gen_random_uuid(),seed_after=NULL,seed_complete=false WHERE workspace_id=$1 AND embedding_model_id=$2`, workspaceID, b.model, b.incarnation); err != nil {
		return err
	}
	if b.desired {
		if err := replaceModelSelection(ctx, tx, modelSelection{workspaceID: workspaceID, modelID: b.model, at: time.Now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}
