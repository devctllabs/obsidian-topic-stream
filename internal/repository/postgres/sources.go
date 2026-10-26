package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Create stores a new Workspace Source.
func (s *Store) Create(ctx context.Context, source domain.Source) error {
	config, err := json.Marshal(source.Config)
	if err != nil {
		return fmt.Errorf("encode source config: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO sources (id, workspace_id, name, config, enabled, poll_interval_minutes, last_synced_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		source.ID, source.WorkspaceID, source.Name, config, source.Enabled, source.PollIntervalMinutes,
		source.LastSyncedAt, source.CreatedAt, source.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert source: %w", err)
	}
	return nil
}

// List returns Sources belonging to the selected Workspace.
func (s *Store) List(ctx context.Context, workspaceID uuid.UUID) ([]domain.Source, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, workspace_id, name, config, enabled, poll_interval_minutes, last_synced_at, created_at, updated_at
FROM sources WHERE workspace_id=$1 ORDER BY name,id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("query sources: %w", err)
	}
	defer rows.Close()
	var result []domain.Source
	for rows.Next() {
		var source domain.Source
		var config []byte
		if err := rows.Scan(&source.ID, &source.WorkspaceID, &source.Name, &config, &source.Enabled,
			&source.PollIntervalMinutes, &source.LastSyncedAt, &source.CreatedAt, &source.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		if err := json.Unmarshal(config, &source.Config); err != nil {
			return nil, fmt.Errorf("decode source config: %w", err)
		}
		result = append(result, source)
	}
	return result, rows.Err()
}

// Get returns a Source only within its Workspace and reports whether it exists.
func (s *Store) Get(ctx context.Context, workspaceID, sourceID uuid.UUID) (domain.Source, bool, error) {
	var source domain.Source
	var config []byte
	err := s.pool.QueryRow(ctx, `
SELECT id, workspace_id, name, config, enabled, poll_interval_minutes, last_synced_at, created_at, updated_at
FROM sources WHERE workspace_id=$1 AND id=$2`, workspaceID, sourceID).Scan(
		&source.ID, &source.WorkspaceID, &source.Name, &config, &source.Enabled,
		&source.PollIntervalMinutes, &source.LastSyncedAt, &source.CreatedAt, &source.UpdatedAt,
	)
	ok, err := found(err)
	if err == nil && ok {
		if decodeErr := json.Unmarshal(config, &source.Config); decodeErr != nil {
			return domain.Source{}, false, fmt.Errorf("decode source config: %w", decodeErr)
		}
	}
	return source, ok, err
}

// SetLastSyncedAt records the Source's successful synchronization time.
func (s *Store) SetLastSyncedAt(ctx context.Context, sourceID uuid.UUID, syncedAt time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE sources SET last_synced_at=$2, updated_at=$2 WHERE id=$1`, sourceID, syncedAt)
	if err != nil {
		return fmt.Errorf("update source sync time: %w", err)
	}
	return nil
}

// Update replaces mutable Source configuration within its Workspace.
func (s *Store) Update(ctx context.Context, source domain.Source) error {
	config, err := json.Marshal(source.Config)
	if err != nil {
		return fmt.Errorf("encode source config: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
UPDATE sources SET name=$3,config=$4,enabled=$5,poll_interval_minutes=$6,updated_at=$7
WHERE workspace_id=$1 AND id=$2`, source.WorkspaceID, source.ID, source.Name, config,
		source.Enabled, source.PollIntervalMinutes, source.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update source: %w", err)
	}
	return nil
}

// Delete removes a Workspace Source and reports whether it existed.
func (s *Store) Delete(ctx context.Context, workspaceID, sourceID uuid.UUID) (bool, error) {
	tx, err := s.beginFeedChange(ctx, workspaceID)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT DISTINCT entry.item_id FROM source_entries entry
JOIN sources source ON source.id=entry.source_id WHERE source.workspace_id=$1 AND source.id=$2`, workspaceID, sourceID)
	if err != nil {
		return false, fmt.Errorf("load deleted source provenance: %w", err)
	}
	itemIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return false, err
	}
	result, err := tx.Exec(ctx, `DELETE FROM sources WHERE workspace_id=$1 AND id=$2`, workspaceID, sourceID)
	if err != nil {
		return false, fmt.Errorf("delete source: %w", err)
	}
	if len(itemIDs) != 0 {
		if _, err := markItemProjectionsPending(ctx, tx, itemProjectionBatchChange{
			workspaceID: workspaceID, itemIDs: itemIDs, updatedAt: time.Now().UTC(),
		}); err != nil {
			return false, fmt.Errorf("record deleted source provenance: %w", err)
		}
	}
	return result.RowsAffected() == 1, tx.Commit(ctx)
}
