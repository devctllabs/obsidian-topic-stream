package settings

//go:generate go tool mockgen -source=service.go -destination=mocks/service.gen.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

var (
	ErrConflict = errors.New("resource version conflict")
	ErrInvalid  = errors.New("invalid settings")
	ErrNotFound = errors.New("settings revision not found")
)

// Repository persists versioned Workspace Settings and Preference Profile revisions.
type Repository interface {
	// GetOrCreateWorkspaceSettings returns persisted settings, creating the supplied defaults when absent.
	GetOrCreateWorkspaceSettings(ctx context.Context, workspaceID uuid.UUID, defaults domain.WorkspaceSettings) (domain.WorkspaceSettings, error)
	// UpdateWorkspaceSettings replaces settings only if expectedVersion matches and reports a successful
	// update.
	UpdateWorkspaceSettings(ctx context.Context, workspaceID uuid.UUID, expectedVersion int, settings domain.WorkspaceSettings) (domain.WorkspaceSettings, bool, error)
	// GetOrCreatePreferenceProfile returns the current profile, creating the supplied defaults when absent.
	GetOrCreatePreferenceProfile(ctx context.Context, workspaceID uuid.UUID, defaults domain.PreferenceProfile) (domain.PreferenceProfile, error)
	// UpdatePreferenceProfile replaces the profile only if expectedVersion matches and preserves its revision
	// history.
	UpdatePreferenceProfile(ctx context.Context, workspaceID uuid.UUID, expectedVersion int, profile domain.PreferenceProfile) (domain.PreferenceProfile, bool, error)
	// GetPreferenceProfileRevision returns an immutable Preference Profile revision and whether it exists.
	GetPreferenceProfileRevision(ctx context.Context, workspaceID uuid.UUID, revision int) (domain.PreferenceProfile, bool, error)
}

// RetrievalReconciler starts model reconciliation after a desired-model change.
type RetrievalReconciler interface {
	// ReconcileModel ensures a transition Job exists when the desired Embedding Model is not serving.
	ReconcileModel(ctx context.Context, modelID uuid.UUID) (domain.Job, error)
}

// Service manages versioned Workspace Settings and Preference Profile revisions.
type Service struct {
	workspaceID uuid.UUID
	repository  Repository
	cache       *Cache
	retrieval   RetrievalReconciler
	now         func() time.Time
}

// ServiceConfig supplies settings persistence, shared cache, optional model reconciliation, and the clock.
type ServiceConfig struct {
	WorkspaceID uuid.UUID
	Repository  Repository
	Cache       *Cache
	Now         func() time.Time
	Retrieval   RetrievalReconciler
}

// NewWithCache constructs settings operations using the supplied shared cache and optional model
// reconciliation.
func NewWithCache(config ServiceConfig) *Service {
	return &Service{
		workspaceID: config.WorkspaceID, repository: config.Repository, cache: config.Cache,
		now: config.Now, retrieval: config.Retrieval,
	}
}

// GetWorkspace returns the current Workspace Settings snapshot.
func (s *Service) GetWorkspace(ctx context.Context) (domain.WorkspaceSettings, error) {
	settings, err := s.cache.Snapshot(ctx, s.workspaceID)
	if err != nil {
		return domain.WorkspaceSettings{}, fmt.Errorf("get workspace settings: %w", err)
	}
	return settings, nil
}

// UpdateWorkspace commits settings before reconciling an embedding change. Startup reconciliation
// repairs an interruption between that commit and durable reindex enqueue.
func (s *Service) UpdateWorkspace(ctx context.Context, expectedVersion int, patch domain.WorkspaceSettingsPatch) (domain.WorkspaceSettings, error) {
	if emptyWorkspacePatch(patch) {
		return domain.WorkspaceSettings{}, fmt.Errorf("%w: %w", ErrInvalid, domain.NewValidationError(domain.Violation{Field: "", Rule: domain.ValidationNotEmpty}))
	}
	current, err := s.GetWorkspace(ctx)
	if err != nil {
		return domain.WorkspaceSettings{}, err
	}
	if current.Version != expectedVersion {
		return domain.WorkspaceSettings{}, ErrConflict
	}
	updated := current
	applyWorkspacePatch(&updated, patch)
	if err := validateWorkspace(updated); err != nil {
		return domain.WorkspaceSettings{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	updated.Version++
	updated.UpdatedAt = s.now()
	stored, changed, err := s.repository.UpdateWorkspaceSettings(ctx, s.workspaceID, expectedVersion, updated)
	if err != nil {
		return domain.WorkspaceSettings{}, fmt.Errorf("update workspace settings: %w", err)
	}
	if !changed {
		return domain.WorkspaceSettings{}, ErrConflict
	}
	s.cache.Publish(s.workspaceID, stored)
	if s.retrieval != nil && current.EmbeddingModelID != stored.EmbeddingModelID {
		if _, err := s.retrieval.ReconcileModel(ctx, stored.EmbeddingModelID); err != nil {
			return domain.WorkspaceSettings{}, fmt.Errorf("schedule retrieval reindex: %w", err)
		}
	}
	return stored, nil
}

func emptyWorkspacePatch(patch domain.WorkspaceSettingsPatch) bool {
	return patch.EmbeddingModelID == nil && patch.Generation == nil && patch.Summary == nil && patch.Instructions == nil &&
		patch.Retention == nil && patch.Retrieval == nil
}

func applyWorkspacePatch(settings *domain.WorkspaceSettings, patch domain.WorkspaceSettingsPatch) {
	if patch.EmbeddingModelID != nil {
		settings.EmbeddingModelID = *patch.EmbeddingModelID
	}
	if patch.Generation != nil {
		settings.Generation = *patch.Generation
	}
	if patch.Summary != nil {
		settings.Summary = *patch.Summary
	}
	if patch.Instructions != nil {
		settings.Instructions = *patch.Instructions
	}
	if patch.Retention != nil {
		settings.Retention = *patch.Retention
	}
	if patch.Retrieval != nil {
		settings.Retrieval = *patch.Retrieval
	}
}

// GetPreferenceProfile returns the current Preference Profile.
func (s *Service) GetPreferenceProfile(ctx context.Context) (domain.PreferenceProfile, error) {
	profile, err := s.repository.GetOrCreatePreferenceProfile(ctx, s.workspaceID, domain.PreferenceProfile{
		Revision: 1, Version: 1, UpdatedAt: s.now(),
	})
	if err != nil {
		return domain.PreferenceProfile{}, fmt.Errorf("get preference profile: %w", err)
	}
	return profile, nil
}

// UpdatePreferenceProfile saves changed guidance as a new revision using expectedVersion; unchanged text
// preserves the current revision.
func (s *Service) UpdatePreferenceProfile(ctx context.Context, expectedVersion int, patch domain.PreferenceProfilePatch) (domain.PreferenceProfile, error) {
	if patch.Prefer == nil && patch.Avoid == nil {
		return domain.PreferenceProfile{}, fmt.Errorf("%w: %w", ErrInvalid, domain.NewValidationError(domain.Violation{Field: "", Rule: domain.ValidationNotEmpty}))
	}
	current, err := s.GetPreferenceProfile(ctx)
	if err != nil {
		return domain.PreferenceProfile{}, err
	}
	if current.Version != expectedVersion {
		return domain.PreferenceProfile{}, ErrConflict
	}
	updated := current
	applyPreferencePatch(&updated, patch)
	if err := validatePreference(updated); err != nil {
		return domain.PreferenceProfile{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if updated.Prefer == current.Prefer && updated.Avoid == current.Avoid {
		return current, nil
	}
	updated.Revision++
	updated.Version++
	updated.UpdatedAt = s.now()
	stored, changed, err := s.repository.UpdatePreferenceProfile(ctx, s.workspaceID, expectedVersion, updated)
	if err != nil {
		return domain.PreferenceProfile{}, fmt.Errorf("update preference profile: %w", err)
	}
	if !changed {
		return domain.PreferenceProfile{}, ErrConflict
	}
	return stored, nil
}

// GetPreferenceProfileRevision returns an immutable revision or ErrNotFound.
func (s *Service) GetPreferenceProfileRevision(ctx context.Context, revision int) (domain.PreferenceProfile, error) {
	if revision < 1 {
		return domain.PreferenceProfile{}, fmt.Errorf("%w: %w", ErrInvalid, domain.CheckMinimum("revision", float64(revision), 1))
	}
	profile, found, err := s.repository.GetPreferenceProfileRevision(ctx, s.workspaceID, revision)
	if err != nil {
		return domain.PreferenceProfile{}, fmt.Errorf("get preference profile revision: %w", err)
	}
	if !found {
		return domain.PreferenceProfile{}, ErrNotFound
	}
	return profile, nil
}

func validatePreference(profile domain.PreferenceProfile) error {
	return domain.JoinValidation(domain.CheckLength("prefer", len(profile.Prefer), 0, 12000),
		domain.CheckLength("avoid", len(profile.Avoid), 0, 12000))
}

func applyPreferencePatch(profile *domain.PreferenceProfile, patch domain.PreferenceProfilePatch) {
	if patch.Prefer != nil {
		profile.Prefer = strings.TrimSpace(*patch.Prefer)
	}
	if patch.Avoid != nil {
		profile.Avoid = strings.TrimSpace(*patch.Avoid)
	}
}

// DefaultWorkspaceSettings returns initial non-secret product settings at the supplied time.
func DefaultWorkspaceSettings(now time.Time) domain.WorkspaceSettings {
	return domain.WorkspaceSettings{
		Version:          1,
		EmbeddingModelID: domain.DefaultEmbeddingModelID,
		Generation:       domain.GenerationSettings{},
		Summary:          domain.SummarySettings{Language: "same-as-source", MaxInputChars: 48000, MaxSummaryCharacters: 800},
		Instructions: domain.TaskInstructions{
			SummaryChunk:  "Extract the important factual points from this content chunk.",
			SummaryReduce: "Combine the supplied facts without adding unsupported claims.",
			SummaryFinal:  "Write a concise plain-text summary for the reader.",
			DigestRerank:  "Rank candidates by the reader profile and explain the strongest signals.",
		},
		Retention: domain.RetentionSettings{},
		Retrieval: domain.RetrievalSettings{
			CandidateLimit: 50, RerankLimit: 30, RRFK: 60,
			RecencyHalfLifeDays: 7, RecencyMaxBoost: 0.1, FeedSourceCap: 3,
			DigestSourceCap: 2, DigestMMRLambda: 0.75,
			DigestExplorationEnabled: true,
		},
		UpdatedAt: now,
	}
}

func validateWorkspace(settings domain.WorkspaceSettings) error {
	validators := []func(domain.WorkspaceSettings) error{
		validateEmbeddingModel,
		validateGenerationSettings,
		validateSummarySettings,
		validateInstructionSettings,
		validateRetentionSettings,
		validateRetrievalSettings,
	}
	checks := make([]error, 0, len(validators))
	for _, validate := range validators {
		checks = append(checks, validate(settings))
	}
	return domain.JoinValidation(checks...)
}

func validateEmbeddingModel(settings domain.WorkspaceSettings) error {
	return domain.CheckRequired("embeddingModelId", settings.EmbeddingModelID != uuid.Nil)
}

func validateGenerationSettings(settings domain.WorkspaceSettings) error {
	if !settings.Generation.Enabled {
		return nil
	}
	return domain.JoinValidation(
		domain.CheckCondition("generation.endpoint", domain.ValidationHTTPURL, settings.Generation.Endpoint != nil && validateHTTPURL(*settings.Generation.Endpoint) == nil),
		domain.CheckRequired("generation.summaryModel", !blank(settings.Generation.SummaryModel)),
		domain.CheckRequired("generation.rerankModel", !blank(settings.Generation.RerankModel)),
	)
}

func validateSummarySettings(settings domain.WorkspaceSettings) error {
	return domain.JoinValidation(
		domain.CheckRequired("summary.language", strings.TrimSpace(settings.Summary.Language) != ""),
		domain.CheckRange("summary.maxInputChars", float64(settings.Summary.MaxInputChars), 1000, 1000000),
		domain.CheckRange("summary.maxSummaryCharacters", float64(settings.Summary.MaxSummaryCharacters), 200, 4000),
	)
}

func validateInstructionSettings(settings domain.WorkspaceSettings) error {
	values := []struct{ field, value string }{
		{"instructions.summaryChunk", settings.Instructions.SummaryChunk}, {"instructions.summaryReduce", settings.Instructions.SummaryReduce},
		{"instructions.summaryFinal", settings.Instructions.SummaryFinal}, {"instructions.digestRerank", settings.Instructions.DigestRerank},
	}
	checks := make([]error, 0, len(values))
	for _, value := range values {
		length := len(value.value)
		if strings.TrimSpace(value.value) == "" {
			length = 0
		}
		checks = append(checks, domain.CheckLength(value.field, length, 1, 12000))
	}
	return domain.JoinValidation(checks...)
}

func validateRetentionSettings(settings domain.WorkspaceSettings) error {
	if settings.Retention.FullContentDays != nil && *settings.Retention.FullContentDays < 1 {
		return domain.CheckMinimum("retention.fullContentDays", float64(*settings.Retention.FullContentDays), 1)
	}
	return nil
}

func validateRetrievalSettings(settings domain.WorkspaceSettings) error {
	r := settings.Retrieval
	return domain.JoinValidation(
		domain.CheckRange("retrieval.candidateLimit", float64(r.CandidateLimit), 1, 100),
		domain.CheckRange("retrieval.rerankLimit", float64(r.RerankLimit), 1, 2000),
		domain.CheckMinimum("retrieval.rrfK", float64(r.RRFK), 1),
		domain.CheckCondition("retrieval.recencyHalfLifeDays", domain.ValidationPositive, r.RecencyHalfLifeDays > 0),
		domain.CheckRange("retrieval.recencyMaxBoost", r.RecencyMaxBoost, 0, 1),
		domain.CheckMinimum("retrieval.feedSourceCap", float64(r.FeedSourceCap), 1),
		domain.CheckMinimum("retrieval.digestSourceCap", float64(r.DigestSourceCap), 1),
		domain.CheckRange("retrieval.digestMmrLambda", r.DigestMMRLambda, 0, 1),
	)
}

func validateHTTPURL(raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("must not contain credentials, query parameters, or fragments")
	}
	return nil
}

func blank(value *string) bool { return value == nil || strings.TrimSpace(*value) == "" }
