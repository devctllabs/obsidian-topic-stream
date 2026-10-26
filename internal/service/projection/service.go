package projection

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/embeddingmodels"
	"github.com/google/uuid"
)

// Repository loads pending Item projection work and acknowledges exact deliveries.
type Repository interface {
	embeddingmodels.CollectionRegistry
	// LoadItemProjection loads the latest canonical projection and pending models; false means no projection exists.
	LoadItemProjection(ctx context.Context, workspaceID uuid.UUID, itemID uuid.UUID) (domain.ItemProjectionWork, bool, error)
	// CompleteItemProjection acknowledges a delivery only when its revision still matches pending work.
	CompleteItemProjection(ctx context.Context, delivery domain.ItemProjectionDelivery) (bool, error)
	// CompleteItemProjections acknowledges a bounded batch with one result per delivery.
	CompleteItemProjections(ctx context.Context, deliveries []domain.ItemProjectionDelivery) ([]bool, error)
}

// Embedder converts text into dense vectors for its configured model.
type Embedder interface {
	// Embed returns the model's vector representation of text.
	Embed(ctx context.Context, text string) ([]float32, error)
	// EmbedBatch returns one validated vector per input text, preserving input order.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

// Writer writes complete Item projections to one derived model index.
type Writer interface {
	embeddingmodels.CollectionIndex
	// UpdateProjectionPayload updates unchanged-content payloads while preserving vectors; false requires a
	// full upsert.
	UpdateProjectionPayload(ctx context.Context, projection domain.ItemProjection) (bool, error)
	// UpsertProjection writes canonical projection state with the supplied dense vector.
	UpsertProjection(ctx context.Context, projection domain.ItemProjection, vector []float32) error
}

// Factory builds an embedder and projection writer for one Embedding Model.
type Factory interface {
	// Build constructs the embedding and index capabilities for the supplied model or generation.
	Build(model domain.EmbeddingModel) (Embedder, Writer, error)
}

// Service delivers canonical Item revisions to pending derived model indexes.
type Service struct {
	repository   Repository
	factory      Factory
	batchOptions BatchOptions
}

// New validates and binds pending projection storage and model-specific adapter construction.
func New(repository Repository, factory Factory) (*Service, error) {
	return NewWithOptions(repository, factory, BatchOptions{Size: 8, InputBytes: 1 << 20, Concurrency: 1})
}

// BatchOptions bounds background embedding request grouping and endpoint concurrency per process.
type BatchOptions struct {
	Size        int
	InputBytes  int
	Concurrency int
}

// Validate checks the bounded background embedding envelope before resource acquisition.
func (o BatchOptions) Validate() error {
	if o.Size < 1 || o.Size > 128 || o.InputBytes < 1024 || o.InputBytes > 4<<20 || o.Concurrency < 1 || o.Concurrency > 8 {
		return errors.New("projection batching requires size 1..128, input bytes 1024..4194304 and concurrency 1..8")
	}
	return nil
}

// NewWithOptions constructs projection delivery with explicit background batching limits.
func NewWithOptions(repository Repository, factory Factory, options BatchOptions) (*Service, error) {
	if repository == nil || factory == nil {
		return nil, errors.New("projection service dependencies are required")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	return &Service{repository: repository, factory: &limitedFactory{delegate: factory, concurrency: options.Concurrency}, batchOptions: options}, nil
}

// WriteItemProjections writes a bounded page and reports useful external progress concurrently.
// Results follow input order; durable acknowledgement remains the caller's explicit operation.
func (s *Service) WriteItemProjections(ctx context.Context, commits []domain.ItemProjectionCommit, onWritten func()) ([]domain.ItemProjectionWriteResult, error) {
	return s.writePage(ctx, commits, onWritten)
}

// WriteItemProjection writes pending destinations and leaves their durable ACK to explicit completion.
func (s *Service) WriteItemProjection(ctx context.Context, commit domain.ItemProjectionCommit) ([]domain.ItemProjectionDelivery, error) {
	work, err := s.load(ctx, commit)
	if err != nil {
		return nil, err
	}
	var written []domain.ItemProjectionDelivery
	var writeErr error
	for _, model := range work.Models {
		delivery, err := s.write(ctx, work.Projection, model, commit.Execution)
		if err != nil {
			writeErr = errors.Join(writeErr, err)
		} else {
			written = append(written, delivery)
		}
	}
	return written, writeErr
}

// CompleteItemProjections acknowledges successful model writes with one result per delivery.
func (s *Service) CompleteItemProjections(ctx context.Context, deliveries []domain.ItemProjectionDelivery) ([]bool, error) {
	return s.repository.CompleteItemProjections(ctx, deliveries)
}

// DeliverItemProjection delivers the supplied revision to pending models, reusing vectors for unchanged content.
// Superseded revisions return an error; each pending model is attempted even when another delivery fails.
func (s *Service) DeliverItemProjection(ctx context.Context, commit domain.ItemProjectionCommit) error {
	work, err := s.load(ctx, commit)
	if err != nil {
		return err
	}
	var deliveryErr error
	for _, model := range work.Models {
		if err := s.deliver(ctx, work.Projection, model, commit.Execution); err != nil {
			deliveryErr = errors.Join(deliveryErr, err)
		}
	}
	return deliveryErr
}

func (s *Service) load(ctx context.Context, commit domain.ItemProjectionCommit) (domain.ItemProjectionWork, error) {
	work, found, err := s.repository.LoadItemProjection(ctx, commit.WorkspaceID, commit.ItemID)
	if err != nil {
		return domain.ItemProjectionWork{}, fmt.Errorf("load item projection: %w", err)
	}
	if !found {
		return domain.ItemProjectionWork{}, errors.New("item projection not found")
	}
	if work.Projection.Revision != commit.ProjectionRevision {
		return domain.ItemProjectionWork{}, errors.New("item projection revision changed before delivery")
	}
	return work, nil
}

func (s *Service) deliver(ctx context.Context, value domain.ItemProjection, model domain.ItemProjectionDestination, execution domain.ProjectionExecution) error {
	delivery, err := s.write(ctx, value, model, execution)
	if err != nil {
		return err
	}
	completed, err := s.repository.CompleteItemProjection(ctx, delivery)
	if err != nil {
		return fmt.Errorf("complete item projection for model %s: %w", model.ID, err)
	}
	if !completed {
		return errors.New("item projection changed during delivery")
	}
	return nil
}

func (s *Service) write(ctx context.Context, value domain.ItemProjection, model domain.ItemProjectionDestination, execution domain.ProjectionExecution) (domain.ItemProjectionDelivery, error) {
	embedder, writer, err := s.factory.Build(model.EmbeddingModel)
	if err != nil {
		return domain.ItemProjectionDelivery{}, fmt.Errorf("build projection runtime for model %s: %w", model.ID, err)
	}
	actual, err := embeddingmodels.EnsureCollection(ctx, s.repository, domain.ModelCollectionObservation{WorkspaceID: value.Item.WorkspaceID, ModelID: model.ID, Incarnation: model.CollectionIncarnation}, writer)
	if err != nil {
		return domain.ItemProjectionDelivery{}, fmt.Errorf("ensure projection for model %s: %w", model.ID, err)
	}
	if model.CollectionIncarnation != uuid.Nil && model.CollectionIncarnation != actual {
		return domain.ItemProjectionDelivery{}, fmt.Errorf("model collection changed before delivery: %w", domain.ErrConflict)
	}
	if err := writeProjection(ctx, writer, embedder, value); err != nil {
		return domain.ItemProjectionDelivery{}, fmt.Errorf("write item projection for model %s: %w", model.ID, err)
	}
	observed, err := writer.CollectionIncarnation(ctx)
	if err != nil {
		return domain.ItemProjectionDelivery{}, err
	}
	if observed != actual {
		_, observeErr := embeddingmodels.EnsureCollection(ctx, s.repository, domain.ModelCollectionObservation{WorkspaceID: value.Item.WorkspaceID, ModelID: model.ID, Incarnation: actual}, writer)
		return domain.ItemProjectionDelivery{}, errors.Join(domain.ErrConflict, observeErr)
	}
	return domain.ItemProjectionDelivery{
		WorkspaceID: value.Item.WorkspaceID, ItemID: value.Item.ID, ModelID: model.ID, Revision: value.Revision,
		ModelGeneration: model.Generation,
		Execution:       execution,
	}, nil
}

func writeProjection(ctx context.Context, writer Writer, embedder Embedder, value domain.ItemProjection) error {
	updated, err := writer.UpdateProjectionPayload(ctx, value)
	if err != nil {
		return err
	}
	if updated {
		return nil
	}
	vector, err := embedder.Embed(ctx, value.Item.RetrievalText())
	if err != nil {
		return fmt.Errorf("embed projection: %w", err)
	}
	return writer.UpsertProjection(ctx, value, vector)
}
