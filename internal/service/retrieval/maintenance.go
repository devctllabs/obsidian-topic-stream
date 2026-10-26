package retrieval

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/google/uuid"
)

// ActiveCatalog reads the currently serving retrieval generation.
type ActiveCatalog interface {
	// GetActive returns the Workspace's serving retrieval generation and whether one exists.
	GetActive(ctx context.Context, workspaceID uuid.UUID) (domain.RetrievalIndex, bool, error)
}

// MaintenanceConfig supplies the Workspace catalog and durable rebuild queue.
type MaintenanceConfig struct {
	WorkspaceID uuid.UUID
	Catalog     ActiveCatalog
	Queue       ReindexQueue
}

// Maintenance enqueues a complete rebuild of the Workspace's current serving model.
type Maintenance struct {
	workspaceID uuid.UUID
	catalog     ActiveCatalog
	queue       ReindexQueue
}

// ReindexQueue submits durable model-preserving rebuild requests.
type ReindexQueue interface {
	// Enqueue submits a durable Job and reuses an equivalent active idempotency key.
	Enqueue(ctx context.Context, request jobs.EnqueueRequest) (domain.Job, error)
}

type reindexArguments struct {
	ModelID             uuid.UUID `json:"modelId"`
	SelectionGeneration int64     `json:"selectionGeneration"`
	Force               bool      `json:"force"`
}

// NewMaintenance validates the Workspace catalog and rebuild queue.
func NewMaintenance(config MaintenanceConfig) (*Maintenance, error) {
	if config.WorkspaceID == uuid.Nil || config.Catalog == nil || config.Queue == nil {
		return nil, errors.New("retrieval maintenance dependencies are required")
	}
	return &Maintenance{workspaceID: config.WorkspaceID, catalog: config.Catalog, queue: config.Queue}, nil
}

// Rebuild preserves the active generation's Embedding Model and deduplicates equivalent active
// rebuild requests through the Job queue.
func (m *Maintenance) Rebuild(ctx context.Context) (domain.Job, error) {
	generation, found, err := m.catalog.GetActive(ctx, m.workspaceID)
	if err != nil {
		return domain.Job{}, fmt.Errorf("get active retrieval generation: %w", err)
	}
	if !found {
		return domain.Job{}, errors.New("active retrieval generation does not exist; index content before rebuilding")
	}
	if generation.DesiredModelID != uuid.Nil && generation.DesiredModelID != generation.ModelID {
		return domain.Job{}, fmt.Errorf("model selection is still transitioning: %w", domain.ErrConflict)
	}
	return enqueueRebuild(ctx, m.queue, m.workspaceID, reindexArguments{ModelID: generation.ModelID, SelectionGeneration: generation.SelectionGeneration, Force: true})
}

func enqueueRebuild(
	ctx context.Context,
	queue ReindexQueue,
	workspaceID uuid.UUID,
	arguments reindexArguments,
) (domain.Job, error) {
	if workspaceID == uuid.Nil || arguments.ModelID == uuid.Nil || queue == nil {
		return domain.Job{}, errors.New("workspace, embedding model, and reindex queue are required")
	}
	job, err := queue.Enqueue(ctx, jobs.EnqueueRequest{
		WorkspaceID: workspaceID, Definition: reindexJobDefinition,
		Arguments:      arguments,
		IdempotencyKey: reindexKey(arguments.ModelID, arguments.SelectionGeneration),
	})
	if err != nil {
		return domain.Job{}, fmt.Errorf("enqueue workspace reindex: %w", err)
	}
	return job, nil
}

func reindexKey(modelID uuid.UUID, generation int64) string {
	return ReindexJobKind + ":" + modelID.String() + ":" + strconv.FormatInt(generation, 10)
}
