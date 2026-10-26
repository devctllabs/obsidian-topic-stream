package settings

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestWorkspaceSettingsCachePublishesCommittedSnapshotsAcrossCallers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 2, 12, 0, 0, 0, time.UTC)
	workspaceID := uuid.New()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	originalValue := DefaultWorkspaceSettings(time.Now())
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), workspaceID, gomock.Any()).Return(originalValue, nil)
	repository.EXPECT().UpdateWorkspaceSettings(gomock.Any(), workspaceID, 1, gomock.Any()).DoAndReturn(func(_ context.Context, _ uuid.UUID, _ int, value domain.WorkspaceSettings) (domain.WorkspaceSettings, bool, error) {
		return value, true, nil
	})
	cache := NewCache(repository, func() time.Time { return now })
	first := NewWithCache(ServiceConfig{WorkspaceID: workspaceID, Repository: repository, Cache: cache, Now: time.Now})
	second := NewWithCache(ServiceConfig{WorkspaceID: workspaceID, Repository: repository, Cache: cache, Now: time.Now})

	original, err := first.GetWorkspace(t.Context())
	require.NoError(t, err)
	_, err = second.GetWorkspace(t.Context())
	require.NoError(t, err)

	retrieval := original.Retrieval
	retrieval.CandidateLimit = 60
	updated, err := first.UpdateWorkspace(t.Context(), original.Version, domain.WorkspaceSettingsPatch{Retrieval: &retrieval})
	require.NoError(t, err)
	observed, err := second.GetWorkspace(t.Context())
	require.NoError(t, err)
	require.Equal(t, updated, observed)

}

func TestWorkspaceSettingsCacheReloadsOnlyNewerInvalidations(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	originalValue := DefaultWorkspaceSettings(time.Now())
	updatedValue := originalValue
	updatedValue.Version++
	updatedValue.Retrieval.CandidateLimit = 75
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), workspaceID, gomock.Any()).Return(originalValue, nil)
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), workspaceID, gomock.Any()).Return(updatedValue, nil)
	cache := NewCache(repository, time.Now)
	current, err := cache.Snapshot(t.Context(), workspaceID)
	require.NoError(t, err)
	current.Version++
	current.Retrieval.CandidateLimit = 75
	stored := updatedValue
	require.NoError(t, cache.Reload(t.Context(), workspaceID, stored.Version))
	require.NoError(t, cache.Reload(t.Context(), workspaceID, stored.Version))
	observed, err := cache.Snapshot(t.Context(), workspaceID)
	require.NoError(t, err)
	require.Equal(t, 75, observed.Retrieval.CandidateLimit)

}

func TestWorkspaceSettingsCacheReloadsEveryCachedWorkspaceAfterListenerReconnect(t *testing.T) {
	t.Parallel()

	firstID, secondID := uuid.New(), uuid.New()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	firstValue := DefaultWorkspaceSettings(time.Now())
	secondValue := firstValue
	updatedFirst := firstValue
	updatedFirst.Version++
	updatedFirst.Retrieval.CandidateLimit = 25
	updatedSecond := secondValue
	updatedSecond.Version++
	updatedSecond.Retrieval.CandidateLimit = 75
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), firstID, gomock.Any()).Return(firstValue, nil)
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), firstID, gomock.Any()).Return(updatedFirst, nil)
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), secondID, gomock.Any()).Return(secondValue, nil)
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), secondID, gomock.Any()).Return(updatedSecond, nil)
	cache := NewCache(repository, time.Now)
	first, err := cache.Snapshot(t.Context(), firstID)
	require.NoError(t, err)
	second, err := cache.Snapshot(t.Context(), secondID)
	require.NoError(t, err)
	first.Version++
	first.Retrieval.CandidateLimit = 25
	second.Version++
	second.Retrieval.CandidateLimit = 75
	require.NoError(t, cache.ReloadCached(t.Context()))

	loadedFirst, err := cache.Snapshot(t.Context(), firstID)
	require.NoError(t, err)
	loadedSecond, err := cache.Snapshot(t.Context(), secondID)
	require.NoError(t, err)
	require.Equal(t, 25, loadedFirst.Retrieval.CandidateLimit)
	require.Equal(t, 75, loadedSecond.Retrieval.CandidateLimit)

}

type retrievalReconcilerSpy struct {
	models []uuid.UUID
}

// ReconcileModel records the desired model and returns a synthetic transition Job.
func (s *retrievalReconcilerSpy) ReconcileModel(_ context.Context, modelID uuid.UUID) (domain.Job, error) {
	s.models = append(s.models, modelID)
	return domain.Job{ID: uuid.New()}, nil
}

func TestWorkspaceSettingsValidationRejectsEveryInvalidBoundary(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*domain.WorkspaceSettings)
	}{
		{"embedding model", func(v *domain.WorkspaceSettings) { v.EmbeddingModelID = uuid.Nil }},
		{"generation endpoint missing", func(v *domain.WorkspaceSettings) { v.Generation.Enabled = true }},
		{"generation endpoint invalid", func(v *domain.WorkspaceSettings) {
			endpoint, model := "ftp://example.com", "model"
			v.Generation = domain.GenerationSettings{Enabled: true, Endpoint: &endpoint, SummaryModel: &model, RerankModel: &model}
		}},
		{"generation summary model", func(v *domain.WorkspaceSettings) {
			endpoint, blank, model := "https://example.com/v1", " ", "model"
			v.Generation = domain.GenerationSettings{Enabled: true, Endpoint: &endpoint, SummaryModel: &blank, RerankModel: &model}
		}},
		{"summary language", func(v *domain.WorkspaceSettings) { v.Summary.Language = "" }},
		{"summary input low", func(v *domain.WorkspaceSettings) { v.Summary.MaxInputChars = 999 }},
		{"summary input high", func(v *domain.WorkspaceSettings) { v.Summary.MaxInputChars = 1000001 }},
		{"summary output low", func(v *domain.WorkspaceSettings) { v.Summary.MaxSummaryCharacters = 199 }},
		{"summary output high", func(v *domain.WorkspaceSettings) { v.Summary.MaxSummaryCharacters = 4001 }},
		{"instruction empty", func(v *domain.WorkspaceSettings) { v.Instructions.SummaryChunk = "" }},
		{"instruction long", func(v *domain.WorkspaceSettings) { v.Instructions.DigestRerank = strings.Repeat("x", 12001) }},
		{"retention", func(v *domain.WorkspaceSettings) { days := 0; v.Retention.FullContentDays = &days }},
		{"candidate limit low", func(v *domain.WorkspaceSettings) { v.Retrieval.CandidateLimit = 0 }},
		{"candidate limit high", func(v *domain.WorkspaceSettings) { v.Retrieval.CandidateLimit = 101 }},
		{"rerank limit", func(v *domain.WorkspaceSettings) { v.Retrieval.RerankLimit = 2001 }},
		{"rrf k", func(v *domain.WorkspaceSettings) { v.Retrieval.RRFK = 0 }},
		{"recency half life", func(v *domain.WorkspaceSettings) { v.Retrieval.RecencyHalfLifeDays = 0 }},
		{"recency boost", func(v *domain.WorkspaceSettings) { v.Retrieval.RecencyMaxBoost = 1.01 }},
		{"feed source cap", func(v *domain.WorkspaceSettings) { v.Retrieval.FeedSourceCap = 0 }},
		{"digest source cap", func(v *domain.WorkspaceSettings) { v.Retrieval.DigestSourceCap = 0 }},
		{"mmr lambda", func(v *domain.WorkspaceSettings) { v.Retrieval.DigestMMRLambda = -0.01 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := DefaultWorkspaceSettings(now)
			test.mutate(&value)
			require.Error(t, validateWorkspace(value))
		})
	}
}

func TestWorkspaceSettingsValidationAcceptsDocumentedBoundaries(t *testing.T) {
	t.Parallel()
	value := DefaultWorkspaceSettings(time.Now())
	endpoint, summaryModel, rerankModel := "https://models.example.test/v1", "summary", "rerank"
	value.Generation = domain.GenerationSettings{Enabled: true, Endpoint: &endpoint, SummaryModel: &summaryModel, RerankModel: &rerankModel}
	value.Summary.MaxInputChars = 1000
	value.Summary.MaxSummaryCharacters = 4000
	days := 1
	value.Retention.FullContentDays = &days
	value.Retrieval = domain.RetrievalSettings{
		CandidateLimit: 100, RerankLimit: 2000, RRFK: 1,
		RecencyHalfLifeDays: 0.1, RecencyMaxBoost: 1, FeedSourceCap: 1,
		DigestSourceCap: 1, DigestMMRLambda: 1,
	}
	require.NoError(t, validateWorkspace(value))
}

func TestDefaultWorkspaceSettingsMatchRankingCalibration(t *testing.T) {
	t.Parallel()

	defaults := DefaultWorkspaceSettings(time.Now())

	require.Equal(t, 50, defaults.Retrieval.CandidateLimit)
	require.Equal(t, 7.0, defaults.Retrieval.RecencyHalfLifeDays)
	require.Equal(t, 0.10, defaults.Retrieval.RecencyMaxBoost)
	require.Equal(t, 3, defaults.Retrieval.FeedSourceCap)
	require.Equal(t, 2, defaults.Retrieval.DigestSourceCap)
	require.Equal(t, 0.75, defaults.Retrieval.DigestMMRLambda)
	require.True(t, defaults.Retrieval.DigestExplorationEnabled)
}

func TestWorkspaceValidationCollectsIndependentFields(t *testing.T) {
	t.Parallel()
	value := DefaultWorkspaceSettings(time.Now())
	value.EmbeddingModelID = uuid.Nil
	value.Generation.Enabled = true
	value.Summary.Language = " "
	value.Summary.MaxInputChars = 0
	value.Retrieval.CandidateLimit = 0
	err := validateWorkspace(value)
	var validation *domain.ValidationError
	require.ErrorAs(t, err, &validation)
	fields := make([]string, 0, len(validation.Violations))
	for _, issue := range validation.Violations {
		fields = append(fields, issue.Field)
	}
	require.ElementsMatch(t, []string{"embeddingModelId", "generation.endpoint", "generation.summaryModel", "generation.rerankModel", "summary.language", "summary.maxInputChars", "retrieval.candidateLimit"}, fields)
}

func TestEmptyPatchesAndOversizedPreferenceAreRejected(t *testing.T) {
	t.Parallel()
	service := newSettingsService(uuid.New(), preferenceRepository(t), time.Now)
	_, err := service.UpdateWorkspace(t.Context(), 1, domain.WorkspaceSettingsPatch{})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = service.UpdatePreferenceProfile(t.Context(), 1, domain.PreferenceProfilePatch{})
	require.ErrorIs(t, err, ErrInvalid)
	tooLong := strings.Repeat("x", 12001)
	_, err = service.UpdatePreferenceProfile(t.Context(), 1, domain.PreferenceProfilePatch{Prefer: &tooLong})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestPreferenceProfileKeepsHistoricalSemanticRevisions(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	originalValue := domain.PreferenceProfile{Version: 1, Revision: 1}
	updatedValue := domain.PreferenceProfile{Version: 2, Revision: 2, Prefer: "distributed systems"}
	repository.EXPECT().GetOrCreatePreferenceProfile(gomock.Any(), workspaceID, gomock.Any()).Return(originalValue, nil).Times(2)
	repository.EXPECT().UpdatePreferenceProfile(gomock.Any(), workspaceID, 1, gomock.Any()).DoAndReturn(func(_ context.Context, _ uuid.UUID, _ int, value domain.PreferenceProfile) (domain.PreferenceProfile, bool, error) {
		require.Equal(t, updatedValue.Prefer, value.Prefer)
		require.Equal(t, 2, value.Revision)
		return value, true, nil
	})
	repository.EXPECT().GetPreferenceProfileRevision(gomock.Any(), workspaceID, 1).Return(originalValue, true, nil)
	repository.EXPECT().GetPreferenceProfileRevision(gomock.Any(), workspaceID, 2).Return(updatedValue, true, nil)
	service := newSettingsService(workspaceID, repository, time.Now)
	original, err := service.GetPreferenceProfile(t.Context())
	require.NoError(t, err)
	prefer := "distributed systems"
	updated, err := service.UpdatePreferenceProfile(t.Context(), original.Version, domain.PreferenceProfilePatch{Prefer: &prefer})
	require.NoError(t, err)

	revisionOne, err := service.GetPreferenceProfileRevision(t.Context(), original.Revision)
	require.NoError(t, err)
	revisionTwo, err := service.GetPreferenceProfileRevision(t.Context(), updated.Revision)
	require.NoError(t, err)

	require.Empty(t, revisionOne.Prefer)
	require.Equal(t, prefer, revisionTwo.Prefer)
	require.Equal(t, 1, revisionOne.Version)
}

func TestEmbeddingModelChangeSchedulesRetrievalTransition(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	reconciler := &retrievalReconcilerSpy{}
	service := newSettingsService(uuid.New(), workspaceRepository(t), func() time.Time { return now }, reconciler)
	current, err := service.GetWorkspace(t.Context())
	require.NoError(t, err)
	changed := uuid.New()

	stored, err := service.UpdateWorkspace(t.Context(), current.Version, domain.WorkspaceSettingsPatch{EmbeddingModelID: &changed})

	require.NoError(t, err)
	require.Equal(t, changed, stored.EmbeddingModelID)
	require.Equal(t, []uuid.UUID{changed}, reconciler.models)
}

func TestUnrelatedSettingsChangeDoesNotScheduleRetrievalReindex(t *testing.T) {
	t.Parallel()

	reconciler := &retrievalReconcilerSpy{}
	service := newSettingsService(uuid.New(), workspaceRepository(t), time.Now, reconciler)
	current, err := service.GetWorkspace(t.Context())
	require.NoError(t, err)
	retrieval := current.Retrieval
	retrieval.CandidateLimit++

	_, err = service.UpdateWorkspace(t.Context(), current.Version, domain.WorkspaceSettingsPatch{Retrieval: &retrieval})

	require.NoError(t, err)
	require.Empty(t, reconciler.models)
}

func workspaceRepository(t *testing.T) *mocks.MockRepository {
	t.Helper()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	original := DefaultWorkspaceSettings(time.Now())
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), gomock.Any(), gomock.Any()).Return(original, nil)
	repository.EXPECT().UpdateWorkspaceSettings(gomock.Any(), gomock.Any(), 1, gomock.Any()).DoAndReturn(func(_ context.Context, _ uuid.UUID, _ int, value domain.WorkspaceSettings) (domain.WorkspaceSettings, bool, error) {
		require.Equal(t, 2, value.Version)
		return value, true, nil
	})
	return repository
}
func preferenceRepository(t *testing.T) *mocks.MockRepository {
	t.Helper()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	repository.EXPECT().GetOrCreatePreferenceProfile(gomock.Any(), gomock.Any(), gomock.Any()).Return(domain.PreferenceProfile{Version: 1, Revision: 1}, nil)
	return repository
}

// newSettingsService constructs the cache fixture used by settings scenarios.
func newSettingsService(workspaceID uuid.UUID, repository Repository, now func() time.Time, retrieval ...RetrievalReconciler) *Service {
	config := ServiceConfig{
		WorkspaceID: workspaceID, Repository: repository, Cache: NewCache(repository, now), Now: now,
	}
	if len(retrieval) > 0 {
		config.Retrieval = retrieval[0]
	}
	return NewWithCache(config)
}
