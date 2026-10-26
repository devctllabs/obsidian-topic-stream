package projection_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/projection"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type repositoryStub struct {
	works        map[uuid.UUID]domain.ItemProjectionWork
	work         domain.ItemProjectionWork
	ackBatches   [][]domain.ItemProjectionDelivery
	completions  []domain.ItemProjectionDelivery
	observations []domain.ModelCollectionObservation
}

// CompleteItemProjections records a bounded explicit completion.
func (s *repositoryStub) CompleteItemProjections(ctx context.Context, deliveries []domain.ItemProjectionDelivery) ([]bool, error) {
	s.ackBatches = append(s.ackBatches, append([]domain.ItemProjectionDelivery(nil), deliveries...))
	accepted := make([]bool, len(deliveries))
	for index, delivery := range deliveries {
		accepted[index], _ = s.CompleteItemProjection(ctx, delivery)
	}
	return accepted, nil
}

// WithModelCollectionLock runs the synthetic marker initialization synchronously.
func (*repositoryStub) WithModelCollectionLock(ctx context.Context, _ uuid.UUID, action func(context.Context) error) error {
	return action(ctx)
}

// ObserveModelCollection accepts the external fixture's stable collection identity.
func (s *repositoryStub) ObserveModelCollection(_ context.Context, observation domain.ModelCollectionObservation) error {
	s.observations = append(s.observations, observation)
	return nil
}

// LoadItemProjection returns the configured canonical projection and pending models.
func (s *repositoryStub) LoadItemProjection(_ context.Context, _ uuid.UUID, itemID uuid.UUID) (domain.ItemProjectionWork, bool, error) {
	if s.works != nil {
		value, found := s.works[itemID]
		return value, found, nil
	}
	return s.work, true, nil
}

// CompleteItemProjection records acknowledgment of a delivered model revision.
func (s *repositoryStub) CompleteItemProjection(_ context.Context, delivery domain.ItemProjectionDelivery) (bool, error) {
	s.completions = append(s.completions, delivery)
	return true, nil
}

type embedderStub struct {
	mu        sync.Mutex
	batches   [][]string
	batchFunc func(context.Context, []string) ([][]float32, error)
	texts     []string
}

// EmbedBatch records the batch and runs the configured provider behavior.
func (s *embedderStub) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	s.mu.Lock()
	s.batches = append(s.batches, append([]string(nil), texts...))
	s.mu.Unlock()
	if s.batchFunc != nil {
		return s.batchFunc(ctx, texts)
	}
	result := make([][]float32, len(texts))
	for i, text := range texts {
		result[i], _ = s.Embed(ctx, text)
	}
	return result, nil
}

// Embed returns a deterministic fixture vector without contacting a provider.
func (s *embedderStub) Embed(_ context.Context, text string) ([]float32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.texts = append(s.texts, text)
	return []float32{1, 2, 3}, nil
}

type writerStub struct {
	mu           sync.Mutex
	payloadReady bool
	ensured      int
	projections  []domain.ItemProjection
	vectors      [][]float32
	upsertErr    error
	incarnation  uuid.UUID
	afterWrite   uuid.UUID
}

// CollectionIncarnation returns the fixture's stable external collection identity.
func (s *writerStub) CollectionIncarnation(context.Context) (uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.incarnation != uuid.Nil {
		return s.incarnation, nil
	}
	return uuid.NameSpaceOID, nil
}

// SetCollectionIncarnation represents coordinated marker creation.
func (*writerStub) SetCollectionIncarnation(context.Context, uuid.UUID) error { return nil }

// Ensure records index initialization and returns its configured outcome.
func (s *writerStub) Ensure(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensured++
	return nil
}

// UpsertProjection records canonical payload and vector writes and returns the configured error.
func (s *writerStub) UpsertProjection(_ context.Context, value domain.ItemProjection, vector []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projections = append(s.projections, value)
	s.vectors = append(s.vectors, vector)
	if s.afterWrite != uuid.Nil {
		s.incarnation = s.afterWrite
	}
	return s.upsertErr
}

func TestCollectionReplacementBeforeOrDuringDeliveryCannotAcknowledge(t *testing.T) {
	for _, before := range []bool{false, true} {
		model := domain.EmbeddingModel{ID: uuid.New()}
		value := domain.ItemProjection{Revision: uuid.New(), Item: domain.Item{ID: uuid.New(), WorkspaceID: uuid.New()}}
		replaced := uuid.New()
		writer := &writerStub{afterWrite: replaced}
		if before {
			writer.incarnation = replaced
		}
		repository := &repositoryStub{work: domain.ItemProjectionWork{Projection: value, Models: []domain.ItemProjectionDestination{{EmbeddingModel: model, CollectionIncarnation: uuid.NameSpaceOID}}}}
		service, err := projection.New(repository, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{model.ID: {embedder: &embedderStub{}, writer: writer}}})
		require.NoError(t, err)
		err = service.DeliverItemProjection(t.Context(), domain.ItemProjectionCommit{WorkspaceID: value.Item.WorkspaceID, ItemID: value.Item.ID, ProjectionRevision: value.Revision})
		require.ErrorIs(t, err, domain.ErrConflict)
		require.Empty(t, repository.completions, "a replaced instance cannot inherit a completed delivery")
		require.Equal(t, replaced, repository.observations[len(repository.observations)-1].Incarnation)
	}
}

func TestDeliverItemProjectionCompletesHealthyModelAndLeavesFailedModelPending(t *testing.T) {
	t.Parallel()

	workspaceID, itemID, revision := uuid.New(), uuid.New(), uuid.New()
	failedModel, healthyModel := domain.EmbeddingModel{ID: uuid.New()}, domain.EmbeddingModel{ID: uuid.New()}
	writeFailure := errors.New("qdrant unavailable")
	repository := &repositoryStub{work: domain.ItemProjectionWork{
		Projection: domain.ItemProjection{
			Revision: revision, Item: domain.Item{ID: itemID, WorkspaceID: workspaceID, Title: "Item"},
		},
		Models: projectionDestinations(failedModel, healthyModel),
	}}
	failedRuntime := &runtimeStub{embedder: &embedderStub{}, writer: &writerStub{upsertErr: writeFailure}}
	healthyRuntime := &runtimeStub{embedder: &embedderStub{}, writer: &writerStub{}}
	service, err := projection.New(repository, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{
		failedModel.ID: failedRuntime, healthyModel.ID: healthyRuntime,
	}})
	require.NoError(t, err)

	err = service.DeliverItemProjection(t.Context(), domain.ItemProjectionCommit{
		WorkspaceID: workspaceID, ItemID: itemID, ProjectionRevision: revision,
	})

	require.ErrorIs(t, err, writeFailure)
	require.Len(t, failedRuntime.writer.projections, 1)
	require.Len(t, healthyRuntime.writer.projections, 1)
	require.Equal(t, []domain.ItemProjectionDelivery{{
		WorkspaceID: workspaceID, ItemID: itemID, ModelID: healthyModel.ID, Revision: revision,
	}}, repository.completions)
}

func TestDeliverItemProjectionRejectsSupersededRevisionBeforeWriting(t *testing.T) {
	t.Parallel()

	workspaceID, itemID := uuid.New(), uuid.New()
	requestedRevision, currentRevision := uuid.New(), uuid.New()
	model := domain.EmbeddingModel{ID: uuid.New()}
	runtime := &runtimeStub{embedder: &embedderStub{}, writer: &writerStub{}}
	service, err := projection.New(&repositoryStub{work: domain.ItemProjectionWork{
		Projection: domain.ItemProjection{
			Revision: currentRevision, Item: domain.Item{ID: itemID, WorkspaceID: workspaceID},
		},
		Models: projectionDestinations(model),
	}}, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{model.ID: runtime}})
	require.NoError(t, err)

	err = service.DeliverItemProjection(t.Context(), domain.ItemProjectionCommit{
		WorkspaceID: workspaceID, ItemID: itemID, ProjectionRevision: requestedRevision,
	})

	require.Error(t, err)
	require.Empty(t, runtime.embedder.texts)
	require.Empty(t, runtime.writer.projections)
}

type runtimeStub struct {
	embedder *embedderStub
	writer   *writerStub
}

type factoryStub struct {
	runtimes map[uuid.UUID]*runtimeStub
}

// Build returns model-specific fixture dependencies without acquiring production resources.
func (s *factoryStub) Build(model domain.EmbeddingModel) (projection.Embedder, projection.Writer, error) {
	runtime := s.runtimes[model.ID]
	return runtime.embedder, runtime.writer, nil
}

func TestDeliverItemProjectionWritesEveryPendingModelAndCompletesExactRevision(t *testing.T) {
	t.Parallel()

	workspaceID, itemID, revision := uuid.New(), uuid.New(), uuid.New()
	firstModel, secondModel := domain.EmbeddingModel{ID: uuid.New()}, domain.EmbeddingModel{ID: uuid.New()}
	projected := domain.ItemProjection{
		Revision: revision,
		Item: domain.Item{
			ID: itemID, WorkspaceID: workspaceID, Title: "KV cache", ContentMarkdown: "Implementation details.",
		},
		Feedback: &domain.Feedback{Sentiment: "like"},
		TopicCorrections: []domain.TopicCorrectionProjection{{
			TopicID: uuid.New(), Membership: domain.TopicMembershipInclude, CorrectionEpoch: 3,
		}},
	}
	repository := &repositoryStub{work: domain.ItemProjectionWork{
		Projection: projected, Models: projectionDestinations(firstModel, secondModel),
	}}
	firstRuntime := &runtimeStub{embedder: &embedderStub{}, writer: &writerStub{}}
	secondRuntime := &runtimeStub{embedder: &embedderStub{}, writer: &writerStub{}}
	service, err := projection.New(repository, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{
		firstModel.ID: firstRuntime, secondModel.ID: secondRuntime,
	}})
	require.NoError(t, err)

	require.NoError(t, service.DeliverItemProjection(t.Context(), domain.ItemProjectionCommit{
		WorkspaceID: workspaceID, ItemID: itemID, ProjectionRevision: revision,
	}))

	for _, runtime := range []*runtimeStub{firstRuntime, secondRuntime} {
		require.Equal(t, []string{"KV cache\n\nImplementation details."}, runtime.embedder.texts)
		require.Equal(t, 1, runtime.writer.ensured)
		require.Equal(t, []domain.ItemProjection{projected}, runtime.writer.projections)
		require.Equal(t, [][]float32{{1, 2, 3}}, runtime.writer.vectors)
	}
	require.ElementsMatch(t, []domain.ItemProjectionDelivery{
		{WorkspaceID: workspaceID, ItemID: itemID, ModelID: firstModel.ID, Revision: revision},
		{WorkspaceID: workspaceID, ItemID: itemID, ModelID: secondModel.ID, Revision: revision},
	}, repository.completions)
}

// UpdateProjectionPayload records payload updates only when the fixture permits vector reuse.
func (s *writerStub) UpdateProjectionPayload(_ context.Context, value domain.ItemProjection) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.payloadReady {
		return false, nil
	}
	s.projections = append(s.projections, value)
	return true, s.upsertErr
}

func TestDeliverPayloadChangeReusesExistingVectors(t *testing.T) {
	t.Parallel()
	workspaceID, itemID, revision, modelID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	value := domain.ItemProjection{Revision: revision, Item: domain.Item{WorkspaceID: workspaceID, ID: itemID, Title: "Unchanged"}}
	repository := &repositoryStub{work: domain.ItemProjectionWork{Projection: value, Models: projectionDestinations(domain.EmbeddingModel{ID: modelID})}}
	runtime := &runtimeStub{embedder: &embedderStub{}, writer: &writerStub{payloadReady: true}}
	service, err := projection.New(repository, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{modelID: runtime}})
	require.NoError(t, err)
	require.NoError(t, service.DeliverItemProjection(t.Context(), domain.ItemProjectionCommit{WorkspaceID: workspaceID, ItemID: itemID, ProjectionRevision: revision}))
	require.Empty(t, runtime.embedder.texts)
	require.Equal(t, []domain.ItemProjection{value}, runtime.writer.projections)
	require.Len(t, repository.completions, 1)
}

func projectionDestinations(models ...domain.EmbeddingModel) []domain.ItemProjectionDestination {
	result := make([]domain.ItemProjectionDestination, 0, len(models))
	for _, model := range models {
		result = append(result, domain.ItemProjectionDestination{EmbeddingModel: model})
	}
	return result
}

func TestDeliveryPreservesDestinationGenerationAndExecutionLease(t *testing.T) {
	model := domain.EmbeddingModel{ID: uuid.New()}
	generation := uuid.New()
	execution := domain.ProjectionExecution{JobID: uuid.New(), LeaseOwner: "owned-worker", Attempt: 3}
	value := domain.ItemProjection{Revision: uuid.New(), Item: domain.Item{ID: uuid.New(), WorkspaceID: uuid.New()}}
	repository := &repositoryStub{work: domain.ItemProjectionWork{Projection: value,
		Models: []domain.ItemProjectionDestination{{EmbeddingModel: model, Generation: generation}}}}
	service, err := projection.New(repository, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{
		model.ID: {embedder: &embedderStub{}, writer: &writerStub{payloadReady: true}},
	}})
	require.NoError(t, err)
	require.NoError(t, service.DeliverItemProjection(t.Context(), domain.ItemProjectionCommit{
		WorkspaceID: value.Item.WorkspaceID, ItemID: value.Item.ID, ProjectionRevision: value.Revision, Execution: execution}))
	require.Equal(t, []domain.ItemProjectionDelivery{{WorkspaceID: value.Item.WorkspaceID,
		ItemID: value.Item.ID, ModelID: model.ID, Revision: value.Revision,
		ModelGeneration: generation, Execution: execution}}, repository.completions)
}

func TestDeferredProjectionWriteCompletesHealthyModelsExplicitly(t *testing.T) {
	bad, healthy := domain.EmbeddingModel{ID: uuid.New()}, domain.EmbeddingModel{ID: uuid.New()}
	failure := errors.New("one model write failed")
	generation := uuid.New()
	execution := domain.ProjectionExecution{JobID: uuid.New(), LeaseOwner: "owned-worker", Attempt: 2}
	value := domain.ItemProjection{Revision: uuid.New(), Item: domain.Item{ID: uuid.New(), WorkspaceID: uuid.New()}}
	repository := &repositoryStub{work: domain.ItemProjectionWork{Projection: value, Models: []domain.ItemProjectionDestination{
		{EmbeddingModel: bad}, {EmbeddingModel: healthy, Generation: generation},
	}}}
	service, err := projection.New(repository, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{
		bad.ID:     {embedder: &embedderStub{}, writer: &writerStub{upsertErr: failure}},
		healthy.ID: {embedder: &embedderStub{}, writer: &writerStub{payloadReady: true}},
	}})
	require.NoError(t, err)
	written, err := service.WriteItemProjection(t.Context(), domain.ItemProjectionCommit{WorkspaceID: value.Item.WorkspaceID, ItemID: value.Item.ID, ProjectionRevision: value.Revision, Execution: execution})
	require.ErrorIs(t, err, failure)
	require.Equal(t, []domain.ItemProjectionDelivery{{WorkspaceID: value.Item.WorkspaceID, ItemID: value.Item.ID, ModelID: healthy.ID, Revision: value.Revision, ModelGeneration: generation, Execution: execution}}, written)
	require.Empty(t, repository.completions, "index writes alone leave pending work durable")
	accepted, err := service.CompleteItemProjections(t.Context(), written)
	require.NoError(t, err)
	require.Equal(t, []bool{true}, accepted)
	require.Equal(t, written, repository.completions)
	require.Equal(t, [][]domain.ItemProjectionDelivery{written}, repository.ackBatches)
}
