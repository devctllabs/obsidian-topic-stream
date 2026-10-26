package portability_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"

	"github.com/ethernity/obsidian-topic-stream/internal/service/portability"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type portabilityRepositoryStub struct {
	exported domain.WorkspaceArchiveData
	imported *domain.WorkspaceArchiveData
}

func TestImportCollectsEnvelopeViolationsBeforeQueueing(t *testing.T) {
	t.Parallel()
	queue := newJobQueue(t)
	service, err := portability.New(portability.Config{WorkspaceID: uuid.New(), Repository: &portabilityRepositoryStub{},
		Settings: portabilitySettingsStub{}, EmbeddingModels: &portabilityModelsStub{}, Retrieval: &portabilityRetrievalStub{}, Jobs: queue, Now: time.Now})
	require.NoError(t, err)
	_, err = service.Import(t.Context(), domain.WorkspaceArchive{})
	var validation *domain.ValidationError
	require.ErrorAs(t, err, &validation)
	require.Len(t, validation.Violations, 3)
	require.Empty(t, queue.requests)
}

type portabilitySettingsStub struct {
	settings domain.WorkspaceSettings
	profile  domain.PreferenceProfile
}

type portabilityModelsStub struct {
	models     map[uuid.UUID]domain.EmbeddingModel
	resolved   domain.EmbeddingModel
	descriptor domain.EmbeddingModelDescriptor
}

// Get returns the fixture record and its lookup result.
func (s *portabilityModelsStub) Get(_ context.Context, id uuid.UUID) (domain.EmbeddingModel, error) {
	return s.models[id], nil
}

// ResolveImported records the portable descriptor and returns the configured local model.
func (s *portabilityModelsStub) ResolveImported(
	_ context.Context,
	descriptor domain.EmbeddingModelDescriptor,
) (domain.EmbeddingModel, error) {
	s.descriptor = descriptor
	return s.resolved, nil
}

type portabilityRetrievalStub struct {
	modelID uuid.UUID
}

// ReconcileModel records the desired model and returns a synthetic transition Job.
func (s *portabilityRetrievalStub) ReconcileModel(_ context.Context, modelID uuid.UUID) (domain.Job, error) {
	s.modelID = modelID
	return domain.Job{ID: uuid.New()}, nil
}

// GetWorkspace returns the fixture's Workspace Settings snapshot.
func (s portabilitySettingsStub) GetWorkspace(context.Context) (domain.WorkspaceSettings, error) {
	return s.settings, nil
}

// GetPreferenceProfile returns the fixture's Preference Profile.
func (s portabilitySettingsStub) GetPreferenceProfile(context.Context) (domain.PreferenceProfile, error) {
	return s.profile, nil
}

// ExportWorkspace returns the fixture's portable Workspace data.
func (s *portabilityRepositoryStub) ExportWorkspace(context.Context, uuid.UUID) (domain.WorkspaceArchiveData, error) {
	return s.exported, nil
}

// ImportWorkspace records imported data after rebinding it to the target Workspace.
func (s *portabilityRepositoryStub) ImportWorkspace(
	_ context.Context,
	workspaceID uuid.UUID,
	archive domain.WorkspaceArchive,
) error {
	data := archive.Data
	data.WorkspaceID = workspaceID
	s.imported = &data
	return nil
}

func TestExportAndImportPreservePortableStateWithSchedulesDisabled(t *testing.T) {
	t.Parallel()

	workspaceID, sourceID, topicID, digestID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)
	repository := &portabilityRepositoryStub{exported: domain.WorkspaceArchiveData{
		WorkspaceID: workspaceID,
		Sources: []domain.Source{{
			ID: sourceID, WorkspaceID: workspaceID, Name: "Feed", Enabled: true,
			Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://example.com/feed"},
		}},
		Topics: []domain.Topic{{ID: topicID, WorkspaceID: workspaceID, Name: "Systems", Enabled: true}},
		Feedback: []domain.PortableFeedback{{
			CanonicalURL: "https://example.com/item",
			Feedback:     domain.Feedback{Sentiment: "like", CreatedAt: now, UpdatedAt: now},
		}},
		Digests: []domain.DigestDefinition{{
			ID: digestID, WorkspaceID: workspaceID, Name: "Weekly", Enabled: true,
			Topics: []domain.DigestTopic{{TopicID: topicID, ItemLimit: 5}},
		}},
	}}
	archiveModelID, localModelID := uuid.New(), uuid.New()
	descriptor := domain.EmbeddingModelDescriptor{
		Name: "Local Qwen", Endpoint: "http://models.test/v1", ProviderModel: "qwen", Dimensions: 1024,
	}
	portableSettings := portabilitySettingsStub{
		settings: domain.WorkspaceSettings{Version: 2, EmbeddingModelID: archiveModelID},
		profile:  domain.PreferenceProfile{Prefer: "systems", Revision: 2, Version: 2},
	}
	models := &portabilityModelsStub{
		models: map[uuid.UUID]domain.EmbeddingModel{archiveModelID: {
			ID: archiveModelID, EmbeddingModelDescriptor: descriptor,
		}},
		resolved: domain.EmbeddingModel{ID: localModelID, EmbeddingModelDescriptor: descriptor},
	}
	retrieval := &portabilityRetrievalStub{}
	queue := newJobQueue(t)
	service, err := portability.New(portability.Config{
		WorkspaceID: workspaceID, Repository: repository, Settings: portableSettings,
		EmbeddingModels: models, Retrieval: retrieval, Jobs: queue, Now: func() time.Time { return now },
	})
	require.NoError(t, err)

	archive, err := service.Export(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.WorkspaceArchiveFormatV2, archive.FormatVersion)
	require.Equal(t, now, archive.ExportedAt)
	require.Equal(t, portableSettings.settings, archive.Data.Settings)
	require.Equal(t, descriptor, archive.Data.EmbeddingModel)
	require.Equal(t, portableSettings.profile, archive.Data.Profile)
	require.Equal(t, repository.exported.Feedback, archive.Data.Feedback)

	_, err = service.Import(t.Context(), archive)
	require.NoError(t, err)
	require.NoError(t, queue.run(t, 0))
	require.NotNil(t, repository.imported)
	require.Equal(t, workspaceID, repository.imported.WorkspaceID)
	require.False(t, repository.imported.Sources[0].Enabled)
	require.False(t, repository.imported.Digests[0].Enabled)
	require.Nil(t, repository.imported.Digests[0].NextRunAt)
	require.Equal(t, repository.exported.Feedback, repository.imported.Feedback)
	require.Equal(t, descriptor, models.descriptor)
	require.Equal(t, localModelID, repository.imported.Settings.EmbeddingModelID)
	require.Equal(t, localModelID, retrieval.modelID)
}
