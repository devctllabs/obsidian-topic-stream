package postgres

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateTopic stores a Topic and its initial immutable semantic revision.
func (s *Store) CreateTopic(ctx context.Context, topic domain.Topic) error {
	transaction, err := s.beginFeedChange(ctx, topic.WorkspaceID)
	if err != nil {
		return fmt.Errorf("begin topic creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `
INSERT INTO topics (id,workspace_id,name,include_text,exclude_text,mode,enabled,revision,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, topic.ID, topic.WorkspaceID, topic.Name, topic.Include,
		topic.Exclude, topic.Mode, topic.Enabled, topic.Revision, topic.CreatedAt, topic.UpdatedAt); err != nil {
		return fmt.Errorf("insert topic: %w", err)
	}
	if err := replaceTopicScopes(ctx, transaction, topic); err != nil {
		return err
	}
	if err := insertTopicRevision(ctx, transaction, topic); err != nil {
		return err
	}
	if err := requestFeedPreparation(ctx, transaction, domain.FeedPreparationRequest{WorkspaceID: topic.WorkspaceID, TopicID: &topic.ID, Invalidate: true, At: topic.UpdatedAt}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit topic creation: %w", err)
	}
	return nil
}

// ListTopics returns the Workspace's Topics.
func (s *Store) ListTopics(ctx context.Context, workspaceID uuid.UUID) ([]domain.Topic, error) {
	rows, err := s.pool.Query(ctx, topicSelect+` WHERE t.workspace_id=$1 ORDER BY t.name,t.id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("query topics: %w", err)
	}
	defer rows.Close()
	var result []domain.Topic
	for rows.Next() {
		topic, err := scanTopic(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, topic)
	}
	return result, rows.Err()
}

// GetTopic returns the Workspace Topic and whether it exists.
func (s *Store) GetTopic(ctx context.Context, workspaceID, topicID uuid.UUID) (domain.Topic, bool, error) {
	topic, err := scanTopic(s.pool.QueryRow(ctx, topicSelect+` WHERE t.workspace_id=$1 AND t.id=$2`, workspaceID, topicID))
	ok, err := found(err)
	return topic, ok, err
}

// UpdateTopic updates a Topic, optionally appending a semantic revision or clearing membership
// corrections.
func (s *Store) UpdateTopic(ctx context.Context, command domain.TopicUpdate) (domain.TopicUpdateResult, error) {
	topic := command.Topic
	transaction, err := s.beginFeedChange(ctx, topic.WorkspaceID)
	if err != nil {
		return domain.TopicUpdateResult{}, fmt.Errorf("begin topic update: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `
UPDATE topics SET name=$3,include_text=$4,exclude_text=$5,mode=$6,enabled=$7,revision=$8,updated_at=$9
WHERE workspace_id=$1 AND id=$2`, topic.WorkspaceID, topic.ID, topic.Name, topic.Include, topic.Exclude,
		topic.Mode, topic.Enabled, topic.Revision, topic.UpdatedAt); err != nil {
		return domain.TopicUpdateResult{}, fmt.Errorf("update topic: %w", err)
	}
	if err := updateTopicDefinition(ctx, transaction, command); err != nil {
		return domain.TopicUpdateResult{}, err
	}
	result := domain.TopicUpdateResult{}
	if command.ClearCorrections {
		result, err = clearTopicCorrections(ctx, transaction, topic)
		if err != nil {
			return domain.TopicUpdateResult{}, err
		}
	}
	if _, err := transaction.Exec(ctx, `DELETE FROM topic_candidate_state WHERE workspace_id=$1 AND topic_id=$2 AND NOT $3`, topic.WorkspaceID, topic.ID, topic.Enabled); err != nil {
		return domain.TopicUpdateResult{}, fmt.Errorf("release disabled Topic candidates: %w", err)
	}
	if err := requestFeedPreparation(ctx, transaction, domain.FeedPreparationRequest{WorkspaceID: topic.WorkspaceID, TopicID: &topic.ID, Invalidate: command.SemanticChanged || command.ClearCorrections, At: topic.UpdatedAt}); err != nil {
		return domain.TopicUpdateResult{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return domain.TopicUpdateResult{}, fmt.Errorf("commit topic update: %w", err)
	}
	return result, nil
}

// DeleteTopic deletes the Workspace Topic and reports whether it existed.
func (s *Store) DeleteTopic(ctx context.Context, workspaceID, topicID uuid.UUID) (bool, error) {
	tx, err := s.beginFeedChange(ctx, workspaceID)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	itemIDs, err := deleteTopicCorrections(ctx, tx, workspaceID, topicID)
	if err != nil {
		return false, err
	}
	if err := refreshFeedDisplay(ctx, tx, workspaceID, itemIDs); err != nil {
		return false, err
	}
	result, err := tx.Exec(ctx, `DELETE FROM topics WHERE workspace_id=$1 AND id=$2`, workspaceID, topicID)
	if err != nil {
		return false, fmt.Errorf("delete topic: %w", err)
	}
	return result.RowsAffected() == 1, tx.Commit(ctx)
}

const topicSelect = `
SELECT t.id,t.workspace_id,t.name,t.include_text,t.exclude_text,t.mode,t.enabled,t.revision,
	   t.correction_epoch,t.correction_version,t.created_at,t.updated_at,
	   ARRAY(SELECT ts.source_id FROM topic_sources ts WHERE ts.topic_id=t.id ORDER BY ts.source_id),
	   ARRAY(SELECT tik.item_kind FROM topic_item_kinds tik WHERE tik.topic_id=t.id ORDER BY tik.item_kind)
FROM topics t`

// topicScanner decodes one SQL row without depending on a concrete pgx row type.
type topicScanner interface {
	// Scan decodes the current SQL row into the supplied destination pointers.
	Scan(destinations ...any) error
}

func scanTopic(scanner topicScanner) (domain.Topic, error) {
	var topic domain.Topic
	var kinds []string
	if err := scanner.Scan(&topic.ID, &topic.WorkspaceID, &topic.Name, &topic.Include, &topic.Exclude, &topic.Mode,
		&topic.Enabled, &topic.Revision, &topic.CorrectionEpoch, &topic.CorrectionVersion,
		&topic.CreatedAt, &topic.UpdatedAt, &topic.SourceIDs, &kinds); err != nil {
		return domain.Topic{}, err
	}
	topic.ItemKinds = make([]domain.ItemKind, len(kinds))
	for index, kind := range kinds {
		topic.ItemKinds[index] = domain.ItemKind(kind)
	}
	return topic, nil
}

func clearTopicCorrections(
	ctx context.Context,
	transaction pgx.Tx,
	topic domain.Topic,
) (domain.TopicUpdateResult, error) {
	itemIDs, err := deleteTopicCorrections(ctx, transaction, topic.WorkspaceID, topic.ID)
	if err != nil {
		return domain.TopicUpdateResult{}, err
	}
	if len(itemIDs) == 0 {
		return domain.TopicUpdateResult{}, nil
	}
	if _, err := transaction.Exec(ctx, `UPDATE topics
SET correction_epoch=correction_epoch+1,correction_version=correction_version+1
WHERE workspace_id=$1 AND id=$2`, topic.WorkspaceID, topic.ID); err != nil {
		return domain.TopicUpdateResult{}, fmt.Errorf("advance topic correction generation: %w", err)
	}
	commits, err := markItemProjectionsPending(ctx, transaction, itemProjectionBatchChange{workspaceID: topic.WorkspaceID, itemIDs: itemIDs, updatedAt: topic.UpdatedAt})
	if err != nil {
		return domain.TopicUpdateResult{}, err
	}
	if err := refreshFeedDisplay(ctx, transaction, topic.WorkspaceID, itemIDs); err != nil {
		return domain.TopicUpdateResult{}, err
	}
	return domain.TopicUpdateResult{ProjectionCommits: commits}, nil
}

func replaceTopicScopes(ctx context.Context, transaction pgx.Tx, topic domain.Topic) error {
	if _, err := transaction.Exec(ctx, `DELETE FROM topic_sources WHERE topic_id=$1`, topic.ID); err != nil {
		return fmt.Errorf("clear topic source scope: %w", err)
	}
	if _, err := transaction.Exec(ctx, `DELETE FROM topic_item_kinds WHERE topic_id=$1`, topic.ID); err != nil {
		return fmt.Errorf("clear topic item-kind scope: %w", err)
	}
	for _, sourceID := range topic.SourceIDs {
		result, err := transaction.Exec(ctx, `
INSERT INTO topic_sources (topic_id,source_id)
SELECT $1,id FROM sources WHERE id=$2 AND workspace_id=$3`, topic.ID, sourceID, topic.WorkspaceID)
		if err != nil {
			return fmt.Errorf("insert topic source scope: %w", err)
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("insert topic source scope: source %s is not in the topic workspace", sourceID)
		}
	}
	for _, kind := range topic.ItemKinds {
		if _, err := transaction.Exec(ctx, `INSERT INTO topic_item_kinds (topic_id,item_kind) VALUES ($1,$2)`, topic.ID, kind); err != nil {
			return fmt.Errorf("insert topic item-kind scope: %w", err)
		}
	}
	return nil
}

func insertTopicRevision(ctx context.Context, transaction pgx.Tx, topic domain.Topic) error {
	sourceIDs := append([]uuid.UUID{}, topic.SourceIDs...)
	kinds := make([]string, len(topic.ItemKinds))
	for index, kind := range topic.ItemKinds {
		kinds[index] = string(kind)
	}
	_, err := transaction.Exec(ctx, `
INSERT INTO topic_revisions (topic_id,revision,source_ids,item_kinds,include_text,exclude_text,mode,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, topic.ID, topic.Revision, sourceIDs, kinds,
		topic.Include, topic.Exclude, topic.Mode, topic.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert topic revision: %w", err)
	}
	return nil
}

func updateTopicDefinition(ctx context.Context, transaction pgx.Tx, command domain.TopicUpdate) error {
	if !command.SemanticChanged {
		return nil
	}
	if err := replaceTopicScopes(ctx, transaction, command.Topic); err != nil {
		return err
	}
	return insertTopicRevision(ctx, transaction, command.Topic)
}

func deleteTopicCorrections(ctx context.Context, transaction pgx.Tx, workspaceID, topicID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := transaction.Query(ctx, `DELETE FROM topic_corrections c USING topics t WHERE t.workspace_id=$1 AND t.id=$2 AND c.topic_id=t.id RETURNING c.item_id`, workspaceID, topicID)
	if err != nil {
		return nil, fmt.Errorf("clear topic corrections: %w", err)
	}
	itemIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read cleared topic corrections: %w", err)
	}
	return itemIDs, nil
}
