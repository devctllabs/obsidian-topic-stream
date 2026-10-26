package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ExportWorkspace loads the Workspace's portable configuration and current feedback.
func (s *Store) ExportWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
) (domain.WorkspaceArchiveData, error) {
	sources, err := s.List(ctx, workspaceID)
	if err != nil {
		return domain.WorkspaceArchiveData{}, err
	}
	topics, err := s.ListTopics(ctx, workspaceID)
	if err != nil {
		return domain.WorkspaceArchiveData{}, err
	}
	digests, err := s.ListDigests(ctx, workspaceID)
	if err != nil {
		return domain.WorkspaceArchiveData{}, err
	}
	feedback, err := s.exportPortableFeedback(ctx, workspaceID)
	if err != nil {
		return domain.WorkspaceArchiveData{}, err
	}
	return domain.WorkspaceArchiveData{
		WorkspaceID: workspaceID, Sources: sources, Topics: topics, Feedback: feedback, Digests: digests,
	}, nil
}

func (s *Store) exportPortableFeedback(
	ctx context.Context,
	workspaceID uuid.UUID,
) ([]domain.PortableFeedback, error) {
	rows, err := s.pool.Query(ctx, `
SELECT i.canonical_url,f.sentiment,f.comment,f.created_at,f.updated_at
FROM item_feedback f JOIN items i ON i.workspace_id=f.workspace_id AND i.id=f.item_id
WHERE f.workspace_id=$1
UNION ALL
SELECT canonical_url,sentiment,comment,created_at,updated_at
FROM pending_item_feedback WHERE workspace_id=$1
ORDER BY 1`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("query portable feedback: %w", err)
	}
	defer rows.Close()
	result := make([]domain.PortableFeedback, 0)
	for rows.Next() {
		var value domain.PortableFeedback
		if err := rows.Scan(
			&value.CanonicalURL, &value.Feedback.Sentiment, &value.Feedback.Comment,
			&value.Feedback.CreatedAt, &value.Feedback.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan portable feedback: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate portable feedback: %w", err)
	}
	return result, nil
}

// ImportWorkspace atomically accepts only an empty portable workspace. Replaying the exact archive
// is idempotent even after the first import has populated that workspace.
func (s *Store) ImportWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	archive domain.WorkspaceArchive,
) error {
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin workspace import: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	imported, err := sameWorkspaceImport(ctx, transaction, workspaceID, archive)
	if err != nil {
		return err
	}
	if imported {
		return nil
	}
	nonempty, err := workspaceHasPortableState(ctx, transaction, workspaceID)
	if err != nil {
		return err
	}
	if nonempty {
		return domain.ErrWorkspaceNotEmpty
	}
	if err := importWorkspaceData(ctx, transaction, workspaceID, archive.Data); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO workspace_imports (workspace_id,format_version,exported_at) VALUES ($1,$2,$3)`,
		workspaceID, archive.FormatVersion, archive.ExportedAt); err != nil {
		return fmt.Errorf("record workspace import: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit workspace import: %w", err)
	}
	return nil
}

func sameWorkspaceImport(
	ctx context.Context,
	transaction pgx.Tx,
	workspaceID uuid.UUID,
	archive domain.WorkspaceArchive,
) (bool, error) {
	var exists bool
	err := transaction.QueryRow(ctx, `SELECT EXISTS (
    SELECT 1 FROM workspace_imports
    WHERE workspace_id=$1 AND format_version=$2 AND exported_at=$3
)`, workspaceID, archive.FormatVersion, archive.ExportedAt).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check prior workspace import: %w", err)
	}
	return exists, nil
}

func workspaceHasPortableState(ctx context.Context, transaction pgx.Tx, workspaceID uuid.UUID) (bool, error) {
	var nonempty bool
	err := transaction.QueryRow(ctx, `SELECT EXISTS (
    SELECT 1 FROM sources WHERE workspace_id=$1
    UNION ALL SELECT 1 FROM topics WHERE workspace_id=$1
    UNION ALL SELECT 1 FROM items WHERE workspace_id=$1
    UNION ALL SELECT 1 FROM digest_definitions WHERE workspace_id=$1
    UNION ALL SELECT 1 FROM pending_item_feedback WHERE workspace_id=$1
)`, workspaceID).Scan(&nonempty)
	if err != nil {
		return false, fmt.Errorf("check empty workspace: %w", err)
	}
	return nonempty, nil
}

func importWorkspaceData(
	ctx context.Context,
	transaction pgx.Tx,
	workspaceID uuid.UUID,
	data domain.WorkspaceArchiveData,
) error {
	if err := importSettings(ctx, transaction, workspaceID, data); err != nil {
		return err
	}
	for _, source := range data.Sources {
		source.WorkspaceID = workspaceID
		if err := importSource(ctx, transaction, source); err != nil {
			return err
		}
	}
	for _, topic := range data.Topics {
		topic.WorkspaceID = workspaceID
		if err := importTopic(ctx, transaction, topic); err != nil {
			return err
		}
	}
	for _, digest := range data.Digests {
		digest.WorkspaceID = workspaceID
		if err := importDigest(ctx, transaction, digest); err != nil {
			return err
		}
	}
	return importFeedback(ctx, transaction, workspaceID, data.Feedback)
}

func importSettings(
	ctx context.Context,
	transaction pgx.Tx,
	workspaceID uuid.UUID,
	data domain.WorkspaceArchiveData,
) error {
	document, err := json.Marshal(data.Settings)
	if err != nil {
		return fmt.Errorf("encode imported workspace settings: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO workspace_settings (workspace_id,embedding_model_id,document,version,updated_at) VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (workspace_id) DO UPDATE SET
embedding_model_id=EXCLUDED.embedding_model_id,document=EXCLUDED.document,
version=EXCLUDED.version,updated_at=EXCLUDED.updated_at`,
		workspaceID, data.Settings.EmbeddingModelID, document, data.Settings.Version, data.Settings.UpdatedAt); err != nil {
		return fmt.Errorf("import workspace settings: %w", err)
	}
	if _, err := transaction.Exec(ctx, `DELETE FROM preference_profile_revisions WHERE workspace_id=$1`, workspaceID); err != nil {
		return fmt.Errorf("clear imported preference revisions: %w", err)
	}
	if _, err := transaction.Exec(ctx, `DELETE FROM preference_profiles WHERE workspace_id=$1`, workspaceID); err != nil {
		return fmt.Errorf("clear imported preference profile: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO preference_profiles (workspace_id,prefer,avoid,revision,version,updated_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		workspaceID, data.Profile.Prefer, data.Profile.Avoid, data.Profile.Revision,
		data.Profile.Version, data.Profile.UpdatedAt); err != nil {
		return fmt.Errorf("import preference profile: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO preference_profile_revisions (workspace_id,revision,prefer,avoid,version,updated_at)
VALUES ($1,$2,$3,$4,$5,$6)`, workspaceID, data.Profile.Revision, data.Profile.Prefer,
		data.Profile.Avoid, data.Profile.Version, data.Profile.UpdatedAt); err != nil {
		return fmt.Errorf("import preference profile revision: %w", err)
	}
	return nil
}

func importSource(ctx context.Context, transaction pgx.Tx, source domain.Source) error {
	config, err := json.Marshal(source.Config)
	if err != nil {
		return fmt.Errorf("encode imported source config: %w", err)
	}
	_, err = transaction.Exec(ctx, `
INSERT INTO sources (id,workspace_id,name,config,enabled,poll_interval_minutes,last_synced_at,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, source.ID, source.WorkspaceID, source.Name,
		config, source.Enabled, source.PollIntervalMinutes, source.LastSyncedAt, source.CreatedAt, source.UpdatedAt)
	if err != nil {
		return fmt.Errorf("import source: %w", err)
	}
	return nil
}

func importTopic(ctx context.Context, transaction pgx.Tx, topic domain.Topic) error {
	_, err := transaction.Exec(ctx, `
INSERT INTO topics (id,workspace_id,name,include_text,exclude_text,mode,enabled,revision,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, topic.ID, topic.WorkspaceID, topic.Name, topic.Include,
		topic.Exclude, topic.Mode, topic.Enabled, topic.Revision, topic.CreatedAt, topic.UpdatedAt)
	if err != nil {
		return fmt.Errorf("import topic: %w", err)
	}
	if err := replaceTopicScopes(ctx, transaction, topic); err != nil {
		return err
	}
	return insertTopicRevision(ctx, transaction, topic)
}

func importDigest(ctx context.Context, transaction pgx.Tx, definition domain.DigestDefinition) error {
	topics, recurrence, err := encodeDigestConfiguration(definition)
	if err != nil {
		return err
	}
	_, err = transaction.Exec(ctx, `
INSERT INTO digest_definitions (
    id,workspace_id,name,topics,timezone,recurrence,enabled,version,next_run_at,last_successful_at,created_at,updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, definition.ID, definition.WorkspaceID,
		definition.Name, topics, definition.Timezone, recurrence, definition.Enabled, definition.Version,
		definition.NextRunAt, definition.LastSuccessfulAt, definition.CreatedAt, definition.UpdatedAt)
	if err != nil {
		return fmt.Errorf("import digest definition: %w", err)
	}
	return nil
}

func importFeedback(
	ctx context.Context,
	transaction pgx.Tx,
	workspaceID uuid.UUID,
	feedback []domain.PortableFeedback,
) error {
	for _, value := range feedback {
		_, err := transaction.Exec(ctx, `
INSERT INTO pending_item_feedback (workspace_id,canonical_url,sentiment,comment,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6)`, workspaceID, value.CanonicalURL, value.Feedback.Sentiment,
			value.Feedback.Comment, value.Feedback.CreatedAt, value.Feedback.UpdatedAt)
		if err != nil {
			return fmt.Errorf("import pending feedback: %w", err)
		}
	}
	return nil
}
