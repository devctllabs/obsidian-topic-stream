package embeddingmodels_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/embeddingmodels"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCreateKeepsFailedInitializationInspectable(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	modelID := uuid.MustParse("0199a6e5-4c00-7000-8000-000000000042")
	repository := newRepositoryStub()
	initializer := &initializerStub{initializeErr: errors.New("provider secret must not escape")}
	service := newService(t, serviceConfig{
		repository: repository, initializer: initializer, now: now, modelID: modelID,
	})

	model, err := service.Create(context.Background(), domain.EmbeddingModelDraft{
		Name: "Local Qwen", Endpoint: "http://localhost:11434/v1",
		ProviderModel: "qwen3-embedding:0.6b", Dimensions: 1024,
	})

	require.NoError(t, err)
	require.Equal(t, modelID, model.ID)
	require.False(t, model.Enabled)
	require.Equal(t, domain.EmbeddingModelFailed, model.InitializationStatus)
	require.NotNil(t, model.FailureReason)
	require.NotContains(t, *model.FailureReason, "secret")
	require.Equal(t, model, repository.models[modelID])
}

func TestCreateCollectsDescriptorViolations(t *testing.T) {
	t.Parallel()
	service := newService(t, serviceConfig{repository: newRepositoryStub(), initializer: &initializerStub{}, now: time.Now(), modelID: uuid.New()})
	_, err := service.Create(t.Context(), domain.EmbeddingModelDraft{Name: "", ProviderModel: "", Dimensions: 0, Endpoint: "file:///private"})
	var validation *domain.ValidationError
	require.ErrorAs(t, err, &validation)
	require.ErrorIs(t, err, embeddingmodels.ErrInvalid)
	require.Len(t, validation.Violations, 4)
}

func TestUpdateProbesChangedEndpointBeforeCommit(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	modelID := uuid.New()
	repository := newRepositoryStub()
	repository.models[modelID] = domain.EmbeddingModel{
		ID: modelID, EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{
			Name: "Local", Endpoint: "http://old.test/v1", ProviderModel: "model", Dimensions: 3,
		}, Enabled: true, InitializationStatus: domain.EmbeddingModelReady, Version: 4,
	}
	initializer := &initializerStub{probeErr: errors.New("offline")}
	service := newService(t, serviceConfig{
		repository: repository, initializer: initializer, now: now, modelID: uuid.New(),
	})

	_, err := service.Update(context.Background(), modelID, 4, domain.EmbeddingModelUpdate{
		Name: "Local", Endpoint: "http://new.test/v1", Enabled: true,
	})

	require.ErrorIs(t, err, embeddingmodels.ErrInvalid)
	require.Equal(t, "http://old.test/v1", repository.models[modelID].Endpoint)
	require.Equal(t, "http://new.test/v1", initializer.probed.Endpoint)
}

func TestOptionsIncludeOnlySelectableAndCurrentModels(t *testing.T) {
	readyID, disabledCurrentID, failedID := uuid.New(), uuid.New(), uuid.New()
	repository := newRepositoryStub()
	repository.models[readyID] = model(readyID, "ready", true, domain.EmbeddingModelReady)
	repository.models[disabledCurrentID] = model(disabledCurrentID, "current", false, domain.EmbeddingModelReady)
	repository.models[failedID] = model(failedID, "failed", true, domain.EmbeddingModelFailed)
	repository.status = domain.RetrievalStatus{DesiredModelID: disabledCurrentID, ServingModelID: &disabledCurrentID}
	service := newService(t, serviceConfig{
		repository: repository, initializer: &initializerStub{}, now: time.Now(), modelID: uuid.New(),
	})

	options, err := service.Options(context.Background())

	require.NoError(t, err)
	require.Len(t, options, 2)
	require.Equal(t, readyID, options[0].Model.ID)
	require.True(t, options[0].Selectable)
	require.Equal(t, disabledCurrentID, options[1].Model.ID)
	require.False(t, options[1].Selectable)
}

func TestResolveImportedReusesAndEnablesIdenticalModel(t *testing.T) {
	t.Parallel()

	modelID := uuid.New()
	descriptor := domain.EmbeddingModelDescriptor{
		Name: "Imported", Endpoint: "http://models.test/v1", ProviderModel: "model", Dimensions: 3,
	}
	repository := newRepositoryStub()
	repository.models[modelID] = domain.EmbeddingModel{
		ID: modelID, EmbeddingModelDescriptor: descriptor,
		InitializationStatus: domain.EmbeddingModelReady, Version: 2,
	}
	service := newService(t, serviceConfig{
		repository: repository, initializer: &initializerStub{}, now: time.Now(), modelID: uuid.New(),
	})

	resolved, err := service.ResolveImported(t.Context(), descriptor)

	require.NoError(t, err)
	require.Equal(t, modelID, resolved.ID)
	require.True(t, resolved.Enabled)
	require.Equal(t, 3, resolved.Version)
}

func TestResolveImportedRejectsSameNameWithDifferentDescriptor(t *testing.T) {
	t.Parallel()

	repository := newRepositoryStub()
	existingID := uuid.New()
	repository.models[existingID] = model(existingID, "Imported", true, domain.EmbeddingModelReady)
	service := newService(t, serviceConfig{
		repository: repository, initializer: &initializerStub{}, now: time.Now(), modelID: uuid.New(),
	})

	_, err := service.ResolveImported(t.Context(), domain.EmbeddingModelDescriptor{
		Name: "Imported", Endpoint: "http://other.test/v1", ProviderModel: "model", Dimensions: 3,
	})

	require.ErrorIs(t, err, embeddingmodels.ErrConflict)
}

func TestResolveImportedCreatesInitializesAndEnablesMissingModel(t *testing.T) {
	t.Parallel()

	modelID := uuid.New()
	descriptor := domain.EmbeddingModelDescriptor{
		Name: "Imported", Endpoint: "http://models.test/v1", ProviderModel: "model", Dimensions: 3,
	}
	repository := newRepositoryStub()
	service := newService(t, serviceConfig{
		repository: repository, initializer: &initializerStub{}, now: time.Now(), modelID: modelID,
	})

	resolved, err := service.ResolveImported(t.Context(), descriptor)

	require.NoError(t, err)
	require.Equal(t, modelID, resolved.ID)
	require.True(t, resolved.Enabled)
	require.Equal(t, domain.EmbeddingModelReady, resolved.InitializationStatus)
}

type repositoryStub struct {
	models map[uuid.UUID]domain.EmbeddingModel
	status domain.RetrievalStatus
}

func newRepositoryStub() *repositoryStub {
	return &repositoryStub{models: make(map[uuid.UUID]domain.EmbeddingModel)}
}

// List returns records stored by the fixture.
func (r *repositoryStub) List(context.Context) ([]domain.EmbeddingModel, error) {
	result := make([]domain.EmbeddingModel, 0, len(r.models))
	for _, value := range r.models {
		result = append(result, value)
	}
	return result, nil
}

// Get returns the fixture record and its lookup result.
func (r *repositoryStub) Get(_ context.Context, id uuid.UUID) (domain.EmbeddingModel, bool, error) {
	value, found := r.models[id]
	return value, found, nil
}

// Create stores the supplied record in the fixture.
func (r *repositoryStub) Create(_ context.Context, value domain.EmbeddingModel) error {
	for _, current := range r.models {
		if current.Name == value.Name {
			return embeddingmodels.ErrConflict
		}
	}
	r.models[value.ID] = value
	return nil
}

// Update stores the record only when the fixture's current version matches.
func (r *repositoryStub) Update(_ context.Context, value domain.EmbeddingModel, expected int) (bool, error) {
	current, found := r.models[value.ID]
	if !found || current.Version != expected {
		return false, nil
	}
	r.models[value.ID] = value
	return true, nil
}

// RetrievalStatus returns the fixture's desired and serving model state.
func (r *repositoryStub) RetrievalStatus(context.Context, uuid.UUID) (domain.RetrievalStatus, error) {
	return r.status, nil
}

type initializerStub struct {
	initializeErr error
	probeErr      error
	probed        domain.EmbeddingModelDescriptor
}

// Initialize returns the configured initialization failure, if any.
func (s *initializerStub) Initialize(context.Context, domain.EmbeddingModel) error {
	return s.initializeErr
}

// Probe records the descriptor and returns the configured probe outcome.
func (s *initializerStub) Probe(_ context.Context, value domain.EmbeddingModelDescriptor) error {
	s.probed = value
	return s.probeErr
}

type serviceConfig struct {
	repository  embeddingmodels.Repository
	initializer embeddingmodels.Initializer
	now         time.Time
	modelID     uuid.UUID
}

func newService(t *testing.T, config serviceConfig) *embeddingmodels.Service {
	t.Helper()
	service, err := embeddingmodels.New(embeddingmodels.Config{
		WorkspaceID: uuid.New(), Repository: config.repository, Initializer: config.initializer,
		Now: func() time.Time { return config.now }, NewID: func() uuid.UUID { return config.modelID },
	})
	require.NoError(t, err)
	return service
}

func model(id uuid.UUID, name string, enabled bool, status domain.EmbeddingModelStatus) domain.EmbeddingModel {
	return domain.EmbeddingModel{
		ID: id, EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{
			Name: name, Endpoint: "http://models.test/v1", ProviderModel: "model", Dimensions: 3,
		}, Enabled: enabled, InitializationStatus: status, Version: 1,
	}
}
