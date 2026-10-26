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

// GetWorkspaceSettings returns stored settings and whether the Workspace has initialized them.
func (s *Store) GetWorkspaceSettings(
	ctx context.Context,
	workspaceID uuid.UUID,
) (domain.WorkspaceSettings, bool, error) {
	var document []byte
	var version int
	var updatedAt time.Time
	var embeddingModelID uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT embedding_model_id,document,version,updated_at FROM workspace_settings WHERE workspace_id=$1`, workspaceID).
		Scan(&embeddingModelID, &document, &version, &updatedAt)
	present, err := found(err)
	if err != nil {
		return domain.WorkspaceSettings{}, false, fmt.Errorf("get workspace settings: %w", err)
	}
	if !present {
		return domain.WorkspaceSettings{}, false, nil
	}
	var settings domain.WorkspaceSettings
	if err := json.Unmarshal(document, &settings); err != nil {
		return domain.WorkspaceSettings{}, false, fmt.Errorf("decode workspace settings: %w", err)
	}
	settings.Version = version
	settings.EmbeddingModelID = embeddingModelID
	settings.UpdatedAt = updatedAt
	return settings, true, nil
}

// GetOrCreateWorkspaceSettings returns persisted settings, creating the supplied defaults when absent.
func (s *Store) GetOrCreateWorkspaceSettings(ctx context.Context, workspaceID uuid.UUID, defaults domain.WorkspaceSettings) (domain.WorkspaceSettings, error) {
	document, err := json.Marshal(defaults)
	if err != nil {
		return domain.WorkspaceSettings{}, fmt.Errorf("encode default workspace settings: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
INSERT INTO workspace_settings (workspace_id,embedding_model_id,document,version,updated_at) VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (workspace_id) DO NOTHING`, workspaceID, defaults.EmbeddingModelID, document, defaults.Version, defaults.UpdatedAt); err != nil {
		return domain.WorkspaceSettings{}, fmt.Errorf("create default workspace settings: %w", err)
	}
	var stored []byte
	var embeddingModelID uuid.UUID
	var version int
	var updatedAt = defaults.UpdatedAt
	err = s.pool.QueryRow(ctx, `SELECT embedding_model_id,document,version,updated_at FROM workspace_settings WHERE workspace_id=$1`, workspaceID).Scan(&embeddingModelID, &stored, &version, &updatedAt)
	if err != nil {
		return domain.WorkspaceSettings{}, fmt.Errorf("get or create workspace settings: %w", err)
	}
	var settings domain.WorkspaceSettings
	if err := json.Unmarshal(stored, &settings); err != nil {
		return domain.WorkspaceSettings{}, fmt.Errorf("decode workspace settings: %w", err)
	}
	settings.Version = version
	settings.EmbeddingModelID = embeddingModelID
	settings.UpdatedAt = updatedAt
	return settings, nil
}

// UpdateWorkspaceSettings replaces settings only if expectedVersion matches and reports a successful
// update.
func (s *Store) UpdateWorkspaceSettings(ctx context.Context, workspaceID uuid.UUID, expectedVersion int, updated domain.WorkspaceSettings) (domain.WorkspaceSettings, bool, error) {
	tx, err := s.beginFeedChange(ctx, workspaceID)
	if err != nil {
		return domain.WorkspaceSettings{}, false, fmt.Errorf("begin workspace settings update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, changed, err := saveWorkspaceSettings(ctx, tx, workspaceSettingsUpdate{workspaceID: workspaceID, expectedVersion: expectedVersion, settings: updated})
	if err != nil || !changed {
		return stored, changed, err
	}
	if err := notifySettingsChanged(ctx, tx, workspaceID, stored.Version); err != nil {
		return domain.WorkspaceSettings{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.WorkspaceSettings{}, false, fmt.Errorf("commit workspace settings update: %w", err)
	}
	return stored, true, nil
}

type workspaceSettingsUpdate struct {
	workspaceID     uuid.UUID
	expectedVersion int
	settings        domain.WorkspaceSettings
}

func saveWorkspaceSettings(ctx context.Context, tx pgx.Tx, change workspaceSettingsUpdate) (domain.WorkspaceSettings, bool, error) {
	workspaceID, expectedVersion, updated := change.workspaceID, change.expectedVersion, change.settings
	document, err := json.Marshal(updated)
	if err != nil {
		return domain.WorkspaceSettings{}, false, fmt.Errorf("encode workspace settings: %w", err)
	}
	var version int
	var updatedAt = updated.UpdatedAt
	var previousModelID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT embedding_model_id FROM workspace_settings WHERE workspace_id=$1 AND version=$2`, workspaceID, expectedVersion).Scan(&previousModelID); err == pgx.ErrNoRows {
		return domain.WorkspaceSettings{}, false, nil
	} else if err != nil {
		return domain.WorkspaceSettings{}, false, err
	}
	if err := validateModelSelectionChange(ctx, tx, previousModelID, updated.EmbeddingModelID); err != nil {
		return domain.WorkspaceSettings{}, false, err
	}
	if err := prepareFeedSettingsUpdate(ctx, tx, workspaceID, updated); err != nil {
		return domain.WorkspaceSettings{}, false, err
	}
	err = tx.QueryRow(ctx, `
	UPDATE workspace_settings AS settings
	SET embedding_model_id=$3,document=$4,version=$5,updated_at=$6
	WHERE settings.workspace_id=$1 AND settings.version=$2
	RETURNING embedding_model_id,version,updated_at`,
		workspaceID, expectedVersion, updated.EmbeddingModelID, document, updated.Version, updated.UpdatedAt).
		Scan(&updated.EmbeddingModelID, &version, &updatedAt)
	if err == pgx.ErrNoRows {
		return domain.WorkspaceSettings{}, false, nil
	}
	if err != nil {
		return domain.WorkspaceSettings{}, false, fmt.Errorf("update workspace settings: %w", err)
	}
	updated.Version = version
	updated.UpdatedAt = updatedAt
	if previousModelID != updated.EmbeddingModelID {
		if err := replaceModelSelection(ctx, tx, modelSelection{workspaceID: workspaceID, modelID: updated.EmbeddingModelID, at: updatedAt}); err != nil {
			return domain.WorkspaceSettings{}, false, err
		}
	}
	return updated, true, nil
}

func notifySettingsChanged(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, version int) error {
	payload, err := encodeAdvisory(domain.Advisory{
		Kind: domain.AdvisorySettingsChanged, WorkspaceID: workspaceID, Version: version,
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_notify('topic_stream_advisories',$1)`, payload); err != nil {
		return fmt.Errorf("notify workspace settings change: %w", err)
	}
	return nil
}

// GetOrCreatePreferenceProfile returns the current profile, creating the supplied defaults when absent.
func (s *Store) GetOrCreatePreferenceProfile(ctx context.Context, workspaceID uuid.UUID, defaults domain.PreferenceProfile) (domain.PreferenceProfile, error) {
	if _, err := s.pool.Exec(ctx, `
INSERT INTO preference_profiles (workspace_id,prefer,avoid,revision,version,updated_at) VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (workspace_id) DO NOTHING`, workspaceID, defaults.Prefer, defaults.Avoid,
		defaults.Revision, defaults.Version, defaults.UpdatedAt); err != nil {
		return domain.PreferenceProfile{}, fmt.Errorf("create default preference profile: %w", err)
	}
	var profile domain.PreferenceProfile
	err := s.pool.QueryRow(ctx, `SELECT prefer,avoid,revision,version,updated_at FROM preference_profiles WHERE workspace_id=$1`, workspaceID).Scan(
		&profile.Prefer, &profile.Avoid, &profile.Revision, &profile.Version, &profile.UpdatedAt,
	)
	if err != nil {
		return domain.PreferenceProfile{}, fmt.Errorf("get or create preference profile: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
INSERT INTO preference_profile_revisions (workspace_id,revision,prefer,avoid,version,updated_at)
VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (workspace_id,revision) DO NOTHING`,
		workspaceID, profile.Revision, profile.Prefer, profile.Avoid, profile.Version, profile.UpdatedAt); err != nil {
		return domain.PreferenceProfile{}, fmt.Errorf("store initial preference profile revision: %w", err)
	}
	return profile, nil
}

// UpdatePreferenceProfile replaces the profile only if expectedVersion matches and preserves its revision
// history.
func (s *Store) UpdatePreferenceProfile(ctx context.Context, workspaceID uuid.UUID, expectedVersion int, updated domain.PreferenceProfile) (domain.PreferenceProfile, bool, error) {
	var stored domain.PreferenceProfile
	err := s.pool.QueryRow(ctx, `
WITH updated AS (
    UPDATE preference_profiles SET prefer=$3,avoid=$4,revision=$5,version=$6,updated_at=$7
    WHERE workspace_id=$1 AND version=$2
    RETURNING workspace_id,prefer,avoid,revision,version,updated_at
), revision AS (
    INSERT INTO preference_profile_revisions (workspace_id,revision,prefer,avoid,version,updated_at)
    SELECT workspace_id,revision,prefer,avoid,version,updated_at FROM updated
    RETURNING prefer,avoid,revision,version,updated_at
)
SELECT prefer,avoid,revision,version,updated_at FROM revision`,
		workspaceID, expectedVersion, updated.Prefer, updated.Avoid, updated.Revision, updated.Version, updated.UpdatedAt).Scan(
		&stored.Prefer, &stored.Avoid, &stored.Revision, &stored.Version, &stored.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return domain.PreferenceProfile{}, false, nil
	}
	if err != nil {
		return domain.PreferenceProfile{}, false, fmt.Errorf("update preference profile: %w", err)
	}
	return stored, true, nil
}

// GetPreferenceProfileRevision returns an immutable Preference Profile revision and whether it exists.
func (s *Store) GetPreferenceProfileRevision(
	ctx context.Context,
	workspaceID uuid.UUID,
	revision int,
) (domain.PreferenceProfile, bool, error) {
	var profile domain.PreferenceProfile
	err := s.pool.QueryRow(ctx, `
SELECT prefer,avoid,revision,version,updated_at
FROM preference_profile_revisions WHERE workspace_id=$1 AND revision=$2`, workspaceID, revision).Scan(
		&profile.Prefer, &profile.Avoid, &profile.Revision, &profile.Version, &profile.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return domain.PreferenceProfile{}, false, nil
	}
	if err != nil {
		return domain.PreferenceProfile{}, false, fmt.Errorf("get preference profile revision: %w", err)
	}
	return profile, true, nil
}

func prepareFeedSettingsUpdate(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, updated domain.WorkspaceSettings) error {
	var previous domain.RetrievalSettings
	err := tx.QueryRow(ctx, `SELECT document->'retrieval' FROM workspace_settings WHERE workspace_id=$1`, workspaceID).Scan(&previous)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if previous.CandidateLimit == updated.Retrieval.CandidateLimit && previous.RRFK == updated.Retrieval.RRFK {
		return nil
	}
	return requestFeedPreparation(ctx, tx, domain.FeedPreparationRequest{WorkspaceID: workspaceID, Invalidate: true, At: updated.UpdatedAt})
}
