package postgres

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GetTopicGateCalibration returns stored gate calibration for one Topic and Embedding Model.
func (s *Store) GetTopicGateCalibration(
	ctx context.Context,
	topicID, modelID uuid.UUID,
) (domain.TopicGateCalibration, bool, error) {
	calibration, err := scanTopicGateCalibration(s.pool.QueryRow(ctx, `
SELECT topic_id,embedding_model_id,topic_revision,correction_epoch,correction_version,
       algorithm_version,include_threshold,exclude_threshold,updated_at
FROM topic_gate_calibrations
WHERE topic_id=$1 AND embedding_model_id=$2`, topicID, modelID))
	present, err := found(err)
	if err != nil {
		return domain.TopicGateCalibration{}, false, fmt.Errorf("get topic gate calibration: %w", err)
	}
	return calibration, present, nil
}

// SaveTopicGateCalibration saves thresholds only if their Topic and correction inputs remain current.
func (s *Store) SaveTopicGateCalibration(
	ctx context.Context,
	calibration domain.TopicGateCalibration,
) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var workspaceID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT workspace_id FROM topics WHERE id=$1`, calibration.TopicID).Scan(&workspaceID)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := lockFeedHead(ctx, tx, workspaceID); err != nil {
		return false, err
	}
	command, err := tx.Exec(ctx, `
INSERT INTO topic_gate_calibrations
(topic_id,embedding_model_id,topic_revision,correction_epoch,correction_version,
 algorithm_version,include_threshold,exclude_threshold,updated_at)
SELECT t.id,m.id,$3,$4,$5,$6,$7,$8,$9
FROM topics t
JOIN embedding_models m ON m.id=$2
WHERE t.id=$1 AND t.revision=$3 AND t.correction_epoch=$4
ON CONFLICT (topic_id,embedding_model_id) DO UPDATE SET
topic_revision=EXCLUDED.topic_revision,
correction_epoch=EXCLUDED.correction_epoch,
correction_version=EXCLUDED.correction_version,
algorithm_version=EXCLUDED.algorithm_version,
include_threshold=EXCLUDED.include_threshold,
exclude_threshold=EXCLUDED.exclude_threshold,
updated_at=EXCLUDED.updated_at
WHERE topic_gate_calibrations.correction_version <= EXCLUDED.correction_version`,
		calibration.TopicID, calibration.EmbeddingModelID, calibration.TopicRevision,
		calibration.CorrectionEpoch, calibration.CorrectionVersion, calibration.AlgorithmVersion,
		calibration.IncludeThreshold, calibration.ExcludeThreshold, calibration.UpdatedAt)
	if err != nil {
		return false, fmt.Errorf("save topic gate calibration: %w", err)
	}
	if command.RowsAffected() == 0 {
		return false, tx.Commit(ctx)
	}
	if err := requestFeedPreparation(ctx, tx, domain.FeedPreparationRequest{WorkspaceID: workspaceID, TopicID: &calibration.TopicID, Invalidate: true, At: calibration.UpdatedAt}); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// ListTopicsNeedingCalibration returns enabled Topics whose calibration inputs or algorithm have changed.
func (s *Store) ListTopicsNeedingCalibration(
	ctx context.Context,
	workspaceID, modelID uuid.UUID,
	algorithmVersion string,
) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
SELECT t.id
FROM topics t
LEFT JOIN topic_gate_calibrations c
  ON c.topic_id=t.id AND c.embedding_model_id=$2
WHERE t.workspace_id=$1 AND t.enabled
  AND (c.topic_id IS NULL
       OR c.topic_revision<>t.revision
       OR c.correction_epoch<>t.correction_epoch
       OR c.correction_version<>t.correction_version
       OR c.algorithm_version<>$3)
ORDER BY t.id`, workspaceID, modelID, algorithmVersion)
	if err != nil {
		return nil, fmt.Errorf("list topics needing calibration: %w", err)
	}
	defer rows.Close()
	topicIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var topicID uuid.UUID
		if err := rows.Scan(&topicID); err != nil {
			return nil, fmt.Errorf("scan topic needing calibration: %w", err)
		}
		topicIDs = append(topicIDs, topicID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topics needing calibration: %w", err)
	}
	return topicIDs, nil
}

// LoadTopicCalibrationSnapshot loads the Topic, model, and current corrections used by one calibration
// attempt.
func (s *Store) LoadTopicCalibrationSnapshot(
	ctx context.Context,
	workspaceID, topicID, modelID uuid.UUID,
) (domain.TopicCalibrationSnapshot, bool, error) {
	transaction, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return domain.TopicCalibrationSnapshot{}, false, fmt.Errorf("begin topic calibration snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	topic, err := scanTopic(transaction.QueryRow(ctx,
		topicSelect+` WHERE t.workspace_id=$1 AND t.id=$2`, workspaceID, topicID))
	present, err := found(err)
	if err != nil || !present {
		return domain.TopicCalibrationSnapshot{}, present, err
	}
	model, err := scanEmbeddingModel(transaction.QueryRow(ctx,
		`SELECT `+embeddingModelColumns+` FROM embedding_models WHERE id=$1`, modelID))
	if err == pgx.ErrNoRows {
		return domain.TopicCalibrationSnapshot{}, false, nil
	}
	if err != nil {
		return domain.TopicCalibrationSnapshot{}, false, fmt.Errorf("get calibration embedding model: %w", err)
	}
	corrections, err := loadScopedTopicCorrections(ctx, transaction, topic)
	if err != nil {
		return domain.TopicCalibrationSnapshot{}, false, err
	}
	return domain.TopicCalibrationSnapshot{Topic: topic, Model: model, Corrections: corrections}, true, nil
}

func loadScopedTopicCorrections(
	ctx context.Context,
	transaction pgx.Tx,
	topic domain.Topic,
) ([]domain.TopicCorrection, error) {
	rows, err := transaction.Query(ctx, `
SELECT i.workspace_id,c.item_id,c.topic_id,c.membership,c.updated_at
FROM topic_corrections c
JOIN items i ON i.id=c.item_id
WHERE c.topic_id=$1 AND i.workspace_id=$2
  AND (NOT EXISTS (SELECT 1 FROM topic_item_kinds WHERE topic_id=$1)
       OR EXISTS (SELECT 1 FROM topic_item_kinds k WHERE k.topic_id=$1 AND k.item_kind=i.kind))
  AND (NOT EXISTS (SELECT 1 FROM topic_sources WHERE topic_id=$1)
       OR EXISTS (
           SELECT 1
           FROM source_entries entry
           JOIN topic_sources scope ON scope.source_id=entry.source_id AND scope.topic_id=$1
           WHERE entry.item_id=i.id
       ))
ORDER BY c.item_id`, topic.ID, topic.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("query scoped topic corrections: %w", err)
	}
	defer rows.Close()
	corrections := make([]domain.TopicCorrection, 0)
	for rows.Next() {
		var correction domain.TopicCorrection
		if err := rows.Scan(
			&correction.WorkspaceID, &correction.ItemID, &correction.TopicID,
			&correction.Membership, &correction.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan scoped topic correction: %w", err)
		}
		corrections = append(corrections, correction)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scoped topic corrections: %w", err)
	}
	return corrections, nil
}

// topicGateCalibrationScanner decodes one SQL row without depending on a concrete pgx row type.
type topicGateCalibrationScanner interface {
	// Scan decodes the current SQL row into the supplied destination pointers.
	Scan(destinations ...any) error
}

func scanTopicGateCalibration(scanner topicGateCalibrationScanner) (domain.TopicGateCalibration, error) {
	var calibration domain.TopicGateCalibration
	err := scanner.Scan(
		&calibration.TopicID, &calibration.EmbeddingModelID, &calibration.TopicRevision,
		&calibration.CorrectionEpoch, &calibration.CorrectionVersion, &calibration.AlgorithmVersion,
		&calibration.IncludeThreshold, &calibration.ExcludeThreshold, &calibration.UpdatedAt,
	)
	return calibration, err
}
