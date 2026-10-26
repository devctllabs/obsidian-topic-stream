package portability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/google/uuid"
)

const importJobKind = "import-workspace"
const importArgumentsVersion = 1

var importWorkspaceJob = jobs.JobDefinition{
	Kind: importJobKind, ArgumentsVersion: importArgumentsVersion, Queue: domain.JobQueueMaintenance,
	Retry: jobs.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Minute, MaxDelay: 30 * time.Minute},
}

// Repository loads and atomically imports portable Workspace state.
type Repository interface {
	// ExportWorkspace loads the Workspace's portable configuration and current feedback.
	ExportWorkspace(ctx context.Context, workspaceID uuid.UUID) (domain.WorkspaceArchiveData, error)
	// ImportWorkspace imports portable configuration into an empty Workspace, accepting exact replays.
	ImportWorkspace(ctx context.Context, workspaceID uuid.UUID, archive domain.WorkspaceArchive) error
}

// Settings provides settings needed by the consuming operation.
type Settings interface {
	// GetWorkspace returns the current Workspace Settings snapshot.
	GetWorkspace(ctx context.Context) (domain.WorkspaceSettings, error)
	// GetPreferenceProfile returns the current Preference Profile.
	GetPreferenceProfile(ctx context.Context) (domain.PreferenceProfile, error)
}

// EmbeddingModels resolves local Embedding Models for portable archives.
type EmbeddingModels interface {
	// Get returns a local Embedding Model or an error when absent.
	Get(ctx context.Context, modelID uuid.UUID) (domain.EmbeddingModel, error)
	// ResolveImported resolves a portable Embedding Model descriptor to a local selectable model.
	ResolveImported(ctx context.Context, descriptor domain.EmbeddingModelDescriptor) (domain.EmbeddingModel, error)
}

// Retrieval reconciles imported Embedding Model selection with serving retrieval.
type Retrieval interface {
	// ReconcileModel ensures a transition Job exists when the desired Embedding Model is not serving.
	ReconcileModel(ctx context.Context, modelID uuid.UUID) (domain.Job, error)
}

// Config supplies dependencies and operating settings for the portability service.
type Config struct {
	WorkspaceID     uuid.UUID
	Repository      Repository
	Settings        Settings
	EmbeddingModels EmbeddingModels
	Retrieval       Retrieval
	Jobs            jobs.Queue
	Now             func() time.Time
}

// Service exports portable Workspace state and imports it through durable Jobs.
type Service struct {
	workspaceID     uuid.UUID
	repository      Repository
	settings        Settings
	embeddingModels EmbeddingModels
	retrieval       Retrieval
	jobs            jobs.Queue
	now             func() time.Time
}

// New validates dependencies and registers the durable Workspace import handler.
func New(config Config) (*Service, error) {
	if config.WorkspaceID == uuid.Nil || config.Repository == nil || config.Settings == nil ||
		config.EmbeddingModels == nil || config.Retrieval == nil ||
		config.Jobs == nil || config.Now == nil {
		return nil, errors.New("portability service dependencies are required")
	}
	service := &Service{
		workspaceID: config.WorkspaceID, repository: config.Repository, settings: config.Settings,
		embeddingModels: config.EmbeddingModels, retrieval: config.Retrieval,
		jobs: config.Jobs, now: config.Now,
	}
	config.Jobs.RegisterDefinition(importWorkspaceJob, service.handleImport)
	return service, nil
}

// Export assembles portable configuration and current feedback without ingested content, derived indexes,
// or secrets.
func (s *Service) Export(ctx context.Context) (domain.WorkspaceArchive, error) {
	workspaceSettings, err := s.settings.GetWorkspace(ctx)
	if err != nil {
		return domain.WorkspaceArchive{}, fmt.Errorf("export workspace settings: %w", err)
	}
	profile, err := s.settings.GetPreferenceProfile(ctx)
	if err != nil {
		return domain.WorkspaceArchive{}, fmt.Errorf("export preference profile: %w", err)
	}
	model, err := s.embeddingModels.Get(ctx, workspaceSettings.EmbeddingModelID)
	if err != nil {
		return domain.WorkspaceArchive{}, fmt.Errorf("export embedding model: %w", err)
	}
	data, err := s.repository.ExportWorkspace(ctx, s.workspaceID)
	if err != nil {
		return domain.WorkspaceArchive{}, fmt.Errorf("export workspace: %w", err)
	}
	data.WorkspaceID = s.workspaceID
	data.Settings = workspaceSettings
	data.EmbeddingModel = model.EmbeddingModelDescriptor
	data.Profile = profile
	return domain.WorkspaceArchive{
		FormatVersion: domain.WorkspaceArchiveFormatV2, ExportedAt: s.now(), Data: data,
	}, nil
}

// Import validates the archive envelope synchronously and applies its contents through a durable
// Job. Imported Sources and Digest Definitions remain disabled so automation never starts implicitly.
func (s *Service) Import(ctx context.Context, archive domain.WorkspaceArchive) (domain.Job, error) {
	if err := validateArchive(archive); err != nil {
		return domain.Job{}, err
	}
	return s.jobs.Enqueue(ctx, jobs.EnqueueRequest{
		WorkspaceID: s.workspaceID, Definition: importWorkspaceJob, Arguments: archive,
		IdempotencyKey: "import-workspace:" + archive.ExportedAt.UTC().Format(time.RFC3339Nano),
	})
}

func (s *Service) handleImport(ctx context.Context, payload json.RawMessage) error {
	var archive domain.WorkspaceArchive
	if err := json.Unmarshal(payload, &archive); err != nil {
		return jobs.Permanent(fmt.Errorf("decode workspace archive: %w", err))
	}
	if err := validateArchive(archive); err != nil {
		return jobs.Permanent(err)
	}
	model, err := s.embeddingModels.ResolveImported(ctx, archive.Data.EmbeddingModel)
	if err != nil {
		return jobs.Permanent(fmt.Errorf("resolve imported embedding model: %w", err))
	}
	archive.Data.Settings.EmbeddingModelID = model.ID
	archive.Data = prepareImport(s.workspaceID, archive.Data)
	if err := s.repository.ImportWorkspace(ctx, s.workspaceID, archive); err != nil {
		if errors.Is(err, domain.ErrWorkspaceNotEmpty) {
			return jobs.Permanent(err)
		}
		return fmt.Errorf("import workspace: %w", err)
	}
	if _, err := s.retrieval.ReconcileModel(ctx, model.ID); err != nil {
		return fmt.Errorf("schedule imported retrieval model: %w", err)
	}
	return nil
}

func validateArchive(archive domain.WorkspaceArchive) error {
	return domain.JoinValidation(
		domain.CheckChoice("formatVersion", archive.FormatVersion, domain.WorkspaceArchiveFormatV2),
		domain.CheckRequired("exportedAt", !archive.ExportedAt.IsZero()),
		domain.CheckRequired("workspaceId", archive.Data.WorkspaceID != uuid.Nil),
	)
}

func prepareImport(workspaceID uuid.UUID, data domain.WorkspaceArchiveData) domain.WorkspaceArchiveData {
	data.WorkspaceID = workspaceID
	for index := range data.Sources {
		data.Sources[index].WorkspaceID = workspaceID
		data.Sources[index].Enabled = false
		data.Sources[index].LastSyncedAt = nil
	}
	for index := range data.Topics {
		data.Topics[index].WorkspaceID = workspaceID
	}
	for index := range data.Digests {
		data.Digests[index].WorkspaceID = workspaceID
		data.Digests[index].Enabled = false
		data.Digests[index].NextRunAt = nil
	}
	return data
}
