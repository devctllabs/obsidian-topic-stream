package embeddingmodels

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

var (
	ErrConflict = errors.New("embedding model conflict")
	ErrInvalid  = errors.New("invalid embedding model")
	ErrNotFound = errors.New("embedding model not found")
)

// Repository persists the Embedding Model registry and serving-model status.
type Repository interface {
	// List returns the matching Embedding Model records.
	List(ctx context.Context) ([]domain.EmbeddingModel, error)
	// Get returns the Embedding Model and whether it exists.
	Get(ctx context.Context, modelID uuid.UUID) (domain.EmbeddingModel, bool, error)
	// Create stores a new Embedding Model.
	Create(ctx context.Context, model domain.EmbeddingModel) error
	// Update updates the Embedding Model only when its expected version matches.
	Update(ctx context.Context, model domain.EmbeddingModel, expectedVersion int) (bool, error)
	// RetrievalStatus returns the desired and serving Embedding Models and any transition Job.
	RetrievalStatus(ctx context.Context, workspaceID uuid.UUID) (domain.RetrievalStatus, error)
}

// Initializer probes Embedding Models and prepares their derived collections.
type Initializer interface {
	// Initialize prepares the Embedding Model's derived collection.
	Initialize(ctx context.Context, model domain.EmbeddingModel) error
	// Probe checks that the embedding endpoint produces the descriptor's vector dimensions.
	Probe(ctx context.Context, descriptor domain.EmbeddingModelDescriptor) error
}

// Config supplies dependencies and operating settings for the embeddingmodels service.
type Config struct {
	WorkspaceID uuid.UUID
	Repository  Repository
	Initializer Initializer
	Now         func() time.Time
	NewID       func() uuid.UUID
}

// Service manages the versioned Embedding Model registry and initialization.
type Service struct {
	workspaceID uuid.UUID
	repository  Repository
	initializer Initializer
	now         func() time.Time
	newID       func() uuid.UUID
}

// New validates and binds registry, initialization, identity, and clock dependencies.
func New(config Config) (*Service, error) {
	if config.WorkspaceID == uuid.Nil || config.Repository == nil || config.Initializer == nil || config.Now == nil || config.NewID == nil {
		return nil, errors.New("embedding model service dependencies are required")
	}
	return &Service{
		workspaceID: config.WorkspaceID, repository: config.Repository, initializer: config.Initializer,
		now: config.Now, newID: config.NewID,
	}, nil
}

// Create validates a descriptor, stores a disabled registry entry, and attempts initialization.
func (s *Service) Create(ctx context.Context, draft domain.EmbeddingModelDraft) (domain.EmbeddingModel, error) {
	descriptor := normalizeDescriptor(domain.EmbeddingModelDescriptor{
		Name: strings.TrimSpace(draft.Name), Endpoint: strings.TrimSpace(draft.Endpoint),
		ProviderModel: strings.TrimSpace(draft.ProviderModel), Dimensions: draft.Dimensions,
	})
	if err := domain.ValidateEmbeddingModelDescriptor(descriptor); err != nil {
		return domain.EmbeddingModel{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	now := s.now()
	model := domain.EmbeddingModel{
		ID: s.newID(), EmbeddingModelDescriptor: descriptor, Enabled: false,
		InitializationStatus: domain.EmbeddingModelInitializing, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if model.ID == uuid.Nil {
		return domain.EmbeddingModel{}, errors.New("generate embedding model ID: nil UUID")
	}
	if err := s.repository.Create(ctx, model); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return domain.EmbeddingModel{}, fmt.Errorf("%w: model name already exists", ErrConflict)
		}
		return domain.EmbeddingModel{}, fmt.Errorf("create embedding model: %w", err)
	}
	return s.finishInitialization(ctx, model)
}

// ResolveImported returns the local selectable model represented by an archive descriptor. The
// archive carries no installation-specific model ID, so the registry name owns conflict detection.
func (s *Service) ResolveImported(
	ctx context.Context,
	descriptor domain.EmbeddingModelDescriptor,
) (domain.EmbeddingModel, error) {
	descriptor = normalizeDescriptor(descriptor)
	if err := domain.ValidateEmbeddingModelDescriptor(descriptor); err != nil {
		return domain.EmbeddingModel{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	models, err := s.List(ctx)
	if err != nil {
		return domain.EmbeddingModel{}, err
	}
	for _, model := range models {
		if model.Name != descriptor.Name {
			continue
		}
		if model.EmbeddingModelDescriptor != descriptor {
			return domain.EmbeddingModel{}, fmt.Errorf("%w: model name has a different descriptor", ErrConflict)
		}
		return s.prepareImported(ctx, model)
	}
	created, err := s.Create(ctx, domain.EmbeddingModelDraft(descriptor))
	if err != nil {
		return domain.EmbeddingModel{}, err
	}
	return s.prepareImported(ctx, created)
}

func (s *Service) prepareImported(ctx context.Context, model domain.EmbeddingModel) (domain.EmbeddingModel, error) {
	var err error
	if model.InitializationStatus != domain.EmbeddingModelReady {
		model, err = s.Initialize(ctx, model.ID)
		if err != nil {
			return domain.EmbeddingModel{}, err
		}
	}
	if model.InitializationStatus != domain.EmbeddingModelReady {
		return domain.EmbeddingModel{}, fmt.Errorf("%w: imported model initialization failed", ErrInvalid)
	}
	if model.Enabled {
		return model, nil
	}
	return s.Update(ctx, model.ID, model.Version, domain.EmbeddingModelUpdate{
		Name: model.Name, Endpoint: model.Endpoint, Enabled: true,
	})
}

func normalizeDescriptor(value domain.EmbeddingModelDescriptor) domain.EmbeddingModelDescriptor {
	value.Name = strings.TrimSpace(value.Name)
	value.Endpoint = strings.TrimSpace(value.Endpoint)
	value.ProviderModel = strings.TrimSpace(value.ProviderModel)
	return value
}

// List returns the local Embedding Model registry.
func (s *Service) List(ctx context.Context) ([]domain.EmbeddingModel, error) {
	models, err := s.repository.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list embedding models: %w", err)
	}
	slices.SortFunc(models, compareModels)
	return models, nil
}

// Get returns a registry entry or ErrNotFound.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.EmbeddingModel, error) {
	if id == uuid.Nil {
		return domain.EmbeddingModel{}, ErrNotFound
	}
	model, found, err := s.repository.Get(ctx, id)
	if err != nil {
		return domain.EmbeddingModel{}, fmt.Errorf("get embedding model: %w", err)
	}
	if !found {
		return domain.EmbeddingModel{}, ErrNotFound
	}
	return model, nil
}

// Update validates mutable fields and expectedVersion before saving the model.
func (s *Service) Update(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int,
	update domain.EmbeddingModelUpdate,
) (domain.EmbeddingModel, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return domain.EmbeddingModel{}, err
	}
	if current.Version != expectedVersion {
		return domain.EmbeddingModel{}, ErrConflict
	}
	descriptor, err := s.validateUpdate(ctx, current, update)
	if err != nil {
		return domain.EmbeddingModel{}, err
	}
	if descriptor == current.EmbeddingModelDescriptor && update.Enabled == current.Enabled {
		return current, nil
	}
	current.EmbeddingModelDescriptor = descriptor
	current.Enabled = update.Enabled
	current.Version++
	current.UpdatedAt = s.now()
	return s.storeUpdate(ctx, current, expectedVersion)
}

func (s *Service) validateUpdate(
	ctx context.Context,
	current domain.EmbeddingModel,
	update domain.EmbeddingModelUpdate,
) (domain.EmbeddingModelDescriptor, error) {
	descriptor := current.EmbeddingModelDescriptor
	descriptor.Name = strings.TrimSpace(update.Name)
	descriptor.Endpoint = strings.TrimSpace(update.Endpoint)
	if err := domain.ValidateEmbeddingModelDescriptor(descriptor); err != nil {
		return domain.EmbeddingModelDescriptor{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if update.Enabled && current.InitializationStatus != domain.EmbeddingModelReady {
		return domain.EmbeddingModelDescriptor{}, fmt.Errorf("%w: %w", ErrInvalid, domain.NewValidationError(domain.Violation{Field: "enabled", Rule: domain.ValidationReady}))
	}
	if descriptor.Endpoint != current.Endpoint {
		if err := s.initializer.Probe(ctx, descriptor); err != nil {
			return domain.EmbeddingModelDescriptor{}, fmt.Errorf("%w: %w", ErrInvalid, domain.NewValidationError(domain.Violation{Field: "endpoint", Rule: domain.ValidationEndpoint}))
		}
	}
	return descriptor, nil
}

func (s *Service) storeUpdate(
	ctx context.Context,
	model domain.EmbeddingModel,
	expectedVersion int,
) (domain.EmbeddingModel, error) {
	changed, err := s.repository.Update(ctx, model, expectedVersion)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return domain.EmbeddingModel{}, fmt.Errorf("%w: model name already exists", ErrConflict)
		}
		return domain.EmbeddingModel{}, fmt.Errorf("update embedding model: %w", err)
	}
	if !changed {
		return domain.EmbeddingModel{}, ErrConflict
	}
	return model, nil
}

// Initialize prepares a registered model's collection and records success or failure.
func (s *Service) Initialize(ctx context.Context, id uuid.UUID) (domain.EmbeddingModel, error) {
	model, err := s.Get(ctx, id)
	if err != nil {
		return domain.EmbeddingModel{}, err
	}
	return s.finishInitialization(ctx, model)
}

// Options marks enabled, ready Embedding Models as selectable.
func (s *Service) Options(ctx context.Context) ([]domain.EmbeddingModelOption, error) {
	models, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	status, err := s.RetrievalStatus(ctx)
	if err != nil {
		return nil, err
	}
	options := make([]domain.EmbeddingModelOption, 0, len(models))
	for _, model := range models {
		selectable := model.Enabled && model.InitializationStatus == domain.EmbeddingModelReady
		current := model.ID == status.DesiredModelID || sameID(status.ServingModelID, model.ID)
		if selectable || current {
			options = append(options, domain.EmbeddingModelOption{Model: model, Selectable: selectable})
		}
	}
	slices.SortStableFunc(options, func(left, right domain.EmbeddingModelOption) int {
		if left.Selectable != right.Selectable {
			if left.Selectable {
				return -1
			}
			return 1
		}
		return compareModels(left.Model, right.Model)
	})
	return options, nil
}

// RetrievalStatus returns desired, serving, and transition state for this Workspace.
func (s *Service) RetrievalStatus(ctx context.Context) (domain.RetrievalStatus, error) {
	status, err := s.repository.RetrievalStatus(ctx, s.workspaceID)
	if err != nil {
		return domain.RetrievalStatus{}, fmt.Errorf("get retrieval status: %w", err)
	}
	return status, nil
}

func (s *Service) finishInitialization(ctx context.Context, model domain.EmbeddingModel) (domain.EmbeddingModel, error) {
	model.InitializationStatus = domain.EmbeddingModelReady
	model.FailureReason = nil
	if err := s.initializer.Initialize(ctx, model); err != nil {
		reason := "embedding model initialization failed"
		model.InitializationStatus = domain.EmbeddingModelFailed
		model.FailureReason = &reason
	}
	expectedVersion := model.Version
	model.Version++
	model.UpdatedAt = s.now()
	changed, err := s.repository.Update(ctx, model, expectedVersion)
	if err != nil {
		return domain.EmbeddingModel{}, fmt.Errorf("store embedding model initialization: %w", err)
	}
	if !changed {
		return domain.EmbeddingModel{}, ErrConflict
	}
	return model, nil
}

func compareModels(left, right domain.EmbeddingModel) int {
	if compared := strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name)); compared != 0 {
		return compared
	}
	return strings.Compare(left.ID.String(), right.ID.String())
}

func sameID(value *uuid.UUID, expected uuid.UUID) bool {
	return value != nil && *value == expected
}
