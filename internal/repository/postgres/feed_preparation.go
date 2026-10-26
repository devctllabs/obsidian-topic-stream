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

// DueFeedWorkspaces returns a bounded page of Workspaces needing candidate preparation.
func (s *Store) DueFeedWorkspaces(ctx context.Context, query domain.FeedPreparationDueQuery) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `WITH due AS (
 SELECT cs.workspace_id FROM topic_candidate_state cs
 JOIN topics t ON t.workspace_id=cs.workspace_id AND t.id=cs.topic_id
 WHERE t.enabled
 AND ((cs.requested_version>cs.ready_version AND cs.first_dirty_at<=$1::timestamptz-interval '1 minute')
 OR cs.next_eligible_at<=$1
 OR ($4<>'' AND cs.ready_version>0 AND cs.prepared_input_version=cs.input_version AND COALESCE(cs.inputs->>'GateVersion','')<>$4))
 UNION
 SELECT request.workspace_id FROM feed_preparation_requests request
 WHERE request.first_dirty_at<=$1::timestamptz-interval '1 minute'
 AND EXISTS(SELECT 1 FROM topics t WHERE t.workspace_id=request.workspace_id AND t.enabled)
 ) SELECT workspace_id FROM due
 WHERE ($2::uuid IS NULL OR workspace_id>$2)
 AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.workspace_id=due.workspace_id AND j.kind='prepare-feed' AND j.status IN ('queued','running','retry_wait'))
 ORDER BY workspace_id LIMIT $3`, query.At, query.After, query.Limit, query.GateVersion)
	if err != nil {
		return nil, fmt.Errorf("find due Feed Workspaces: %w", err)
	}
	defer rows.Close()
	result := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

// LoadFeedPreparationWork loads a bounded batch of Topic preparation inputs for the Workspace.
func (s *Store) LoadFeedPreparationWork(ctx context.Context, query domain.FeedPreparationQuery) ([]domain.FeedPreparationWork, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockFeedPreparation(ctx, tx, query.WorkspaceID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE topic_candidate_state SET requested_version=requested_version+1,input_version=input_version+1,
 first_dirty_at=COALESCE(first_dirty_at,$2)
 WHERE workspace_id=$1 AND $3<>'' AND ready_version>0 AND prepared_input_version=input_version AND COALESCE(inputs->>'GateVersion','')<>$3`, query.WorkspaceID, query.At, query.GateVersion); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE topic_candidate_state SET requested_version=requested_version+1,
 first_dirty_at=LEAST(first_dirty_at,$2::timestamptz-interval '1 minute'),next_eligible_at=NULL
 WHERE workspace_id=$1 AND next_eligible_at<=$2`, query.WorkspaceID, query.At); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, loadFeedPreparationSQL, query.WorkspaceID, query.At, query.Limit)
	if err != nil {
		return nil, err
	}
	result := make([]domain.FeedPreparationWork, 0)
	for rows.Next() {
		work := domain.FeedPreparationWork{WorkspaceID: query.WorkspaceID}
		var inputs []byte
		if err := rows.Scan(&work.TopicID, &work.RequestedVersion, &work.InputVersion, &inputs, &work.IncludeEmbedding, &work.ExcludeEmbedding, &work.NextEligibleAt); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(inputs, &work.Inputs); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, work)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, tx.Commit(ctx)
}

func lockFeedPreparation(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID) error {
	if _, err := lockFeedHead(ctx, tx, workspaceID); err != nil {
		return err
	}
	var firstDirtyAt time.Time
	err := tx.QueryRow(ctx, `DELETE FROM feed_preparation_requests WHERE workspace_id=$1 RETURNING first_dirty_at`, workspaceID).Scan(&firstDirtyAt)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return requestFeedPreparation(ctx, tx, domain.FeedPreparationRequest{WorkspaceID: workspaceID, At: firstDirtyAt})
}

const loadFeedPreparationSQL = `SELECT cs.topic_id,cs.requested_version,cs.input_version,cs.inputs,cs.include_embedding,cs.exclude_embedding,
 (SELECT COALESCE(i.published_at,i.discovered_at) FROM items i WHERE i.workspace_id=$1
 AND COALESCE(i.published_at,i.discovered_at)>$2 ORDER BY COALESCE(i.published_at,i.discovered_at) LIMIT 1)
 FROM topic_candidate_state cs JOIN topics t ON t.workspace_id=cs.workspace_id AND t.id=cs.topic_id
 WHERE cs.workspace_id=$1 AND t.enabled AND cs.requested_version>cs.ready_version
 AND cs.first_dirty_at<=$2::timestamptz-interval '1 minute'
 ORDER BY cs.prepared_at NULLS FIRST,cs.first_dirty_at,cs.topic_id LIMIT $3`
