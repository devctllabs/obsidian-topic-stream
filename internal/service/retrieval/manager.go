package retrieval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/embeddingmodels"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
)

const (
	ReindexJobKind          = "reindex-workspace"
	reindexArgumentsVersion = 3
	reindexBatch            = 100
)

var reindexJobDefinition = jobs.JobDefinition{
	Kind: ReindexJobKind, ArgumentsVersion: reindexArgumentsVersion, Queue: domain.JobQueueMaintenance,
	Retry: jobs.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Minute, MaxDelay: 30 * time.Minute},
}

// Catalog owns serving generations and durable model-transition state.
type Catalog interface {
	embeddingmodels.CollectionRegistry
	ActiveCatalog
	// RetryModelTransition atomically admits only the latest failed model-selection operation.
	RetryModelTransition(ctx context.Context, source, replacement domain.Job) (domain.Job, bool, error)
	// ResolveModelIndex captures the target collection before changing transition state.
	ResolveModelIndex(ctx context.Context, workspaceID, modelID uuid.UUID) (domain.RetrievalIndex, error)
	// ModelRequestHasJob includes terminal Jobs so startup cannot resurrect a cancelled request.
	ModelRequestHasJob(ctx context.Context, workspaceID uuid.UUID, key string) (bool, error)
	// ResolveModel loads a registered Embedding Model or returns an error when it is absent.
	ResolveModel(ctx context.Context, modelID uuid.UUID) (domain.EmbeddingModel, error)
	// GetOrCreateActive returns an existing serving generation or initializes its catalog state.
	GetOrCreateActive(ctx context.Context, workspaceID uuid.UUID, modelID uuid.UUID, at time.Time) (domain.RetrievalIndex, error)
	// Begin starts a model transition and returns its building generation.
	Begin(ctx context.Context, transition domain.ModelTransitionStart) (domain.RetrievalIndex, error)
	// SeedModelProjections schedules the next bounded batch and reports whether initialization is complete.
	SeedModelProjections(ctx context.Context, command domain.ModelProjectionSeedCommand) (bool, error)
	// PrepareActivation persists the activating phase only for the current owned transition.
	PrepareActivation(ctx context.Context, cutover domain.ModelTransitionCutover) error
	// Activate cuts over serving retrieval after the model generation is ready.
	Activate(ctx context.Context, cutover domain.ModelTransitionCutover) error
	// Fail clears the matching failed transition without discarding the serving generation.
	Fail(ctx context.Context, failure domain.RetrievalIndexFailure) error
}

// PendingProjections finds model-specific projection work needed before cutover.
type PendingProjections interface {
	// ListPendingModelProjections returns pending Item projections for one Workspace and Embedding Model.
	ListPendingModelProjections(ctx context.Context, query domain.ModelProjectionQuery) ([]domain.ItemProjectionCommit, error)
}

// ProjectionDeliverer delivers the latest Item revision to pending model indexes.
type ProjectionDeliverer interface {
	// WriteItemProjections writes a bounded page in input order. onWritten reports useful external
	// progress and may run concurrently; successful destinations remain available after partial failure.
	WriteItemProjections(ctx context.Context, commits []domain.ItemProjectionCommit, onWritten func()) ([]domain.ItemProjectionWriteResult, error)
	// CompleteItemProjections acknowledges the written revisions with one result per entry.
	CompleteItemProjections(ctx context.Context, deliveries []domain.ItemProjectionDelivery) ([]bool, error)
}

// Embedder converts text into dense vectors for its configured model.
type Embedder interface {
	// Embed returns the model's vector representation of text.
	Embed(ctx context.Context, text string) ([]float32, error)
}

// Index provides the generation-specific derived retrieval operations.
type Index interface {
	embeddingmodels.CollectionIndex
	// QueryDense retrieves bounded dense-vector matches satisfying the filter.
	QueryDense(ctx context.Context, vector []float32, filter ranking.IndexFilter, limit int) ([]ranking.IndexHit, error)
	// QuerySparse retrieves bounded lexical matches satisfying the filter.
	QuerySparse(ctx context.Context, text string, filter ranking.IndexFilter, limit int) ([]ranking.IndexHit, error)
	// QueryRecent returns recent Items satisfying the filter, bounded by limit.
	QueryRecent(ctx context.Context, filter ranking.IndexFilter, limit int) ([]ranking.IndexHit, error)
	// RetrievePersonal returns direct feedback and positive and negative similarity neighborhoods.
	RetrievePersonal(ctx context.Context, request ranking.PersonalIndexRequest) (ranking.PersonalRetrieval, error)
	// QueryTopicCorrections retrieves bounded explicit membership evidence for the current Topic correction
	// epoch.
	QueryTopicCorrections(ctx context.Context, request ranking.TopicCorrectionIndexRequest) ([]ranking.IndexHit, error)
}

// Factory builds an embedder and index for one retrieval generation.
type Factory interface {
	// Build constructs the embedding and index capabilities for the supplied model or generation.
	Build(generation domain.RetrievalIndex) (Embedder, Index, error)
}

// ModelActivationNotifier wakes dependent calibration when the serving model changes.
type ModelActivationNotifier interface {
	// NotifyModelChanged requests recalibration after the serving Embedding Model changes.
	NotifyModelChanged()
}

// generationRuntime keeps a captured generation paired with its matching embedder and index.
type generationRuntime struct {
	generation domain.RetrievalIndex
	embedder   Embedder
	index      Index
}

// Manager serves one active retrieval generation while rebuilding replacements in the background.
// Readers switch to a replacement only after its collection is complete and durably activated.
type Manager struct {
	workspaceID      uuid.UUID
	catalog          Catalog
	pending          PendingProjections
	projector        ProjectionDeliverer
	jobs             jobs.Queue
	factory          Factory
	modelNotifier    ModelActivationNotifier
	progressNotifier jobs.StateNotifier
	now              func() time.Time
	stallTimeout     time.Duration

	// mu protects replacement of current; each operation captures one generation before doing I/O.
	mu      sync.RWMutex
	current generationRuntime
}

// Config supplies generation lifecycle, projection repair, Jobs, and the desired Embedding Model.
type Config struct {
	WorkspaceID      uuid.UUID
	Catalog          Catalog
	Pending          PendingProjections
	Projector        ProjectionDeliverer
	Jobs             jobs.Queue
	Factory          Factory
	ModelNotifier    ModelActivationNotifier
	ProgressNotifier jobs.StateNotifier
	DesiredModelID   uuid.UUID
	Now              func() time.Time
	// StallTimeout defaults to five minutes without seed or delivery progress.
	StallTimeout time.Duration
}

// New initializes serving-generation access and registers durable model transition work.
func New(ctx context.Context, config Config) (*Manager, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	active, err := config.Catalog.GetOrCreateActive(ctx, config.WorkspaceID, config.DesiredModelID, config.Now())
	if err != nil {
		return nil, fmt.Errorf("load active retrieval index: %w", err)
	}
	runtime, err := buildRuntime(config.Factory, active)
	if err != nil {
		return nil, err
	}
	// Derived I/O belongs to live operations and rebuild Jobs. Database-backed API
	// construction must remain available when Qdrant is slow or unreachable.
	manager := &Manager{
		workspaceID: config.WorkspaceID, catalog: config.Catalog,
		pending: config.Pending, projector: config.Projector, jobs: config.Jobs,
		factory: config.Factory, modelNotifier: config.ModelNotifier, progressNotifier: config.ProgressNotifier, now: config.Now, current: runtime,
	}
	manager.stallTimeout = config.StallTimeout
	if manager.stallTimeout <= 0 {
		manager.stallTimeout = 5 * time.Minute
	}
	definition := reindexJobDefinition
	definition.RetryHandler = manager.retryReindex
	config.Jobs.RegisterDefinition(definition, manager.handleReindex)
	if _, err := manager.ReconcileModel(ctx, config.DesiredModelID); err != nil {
		return nil, err
	}
	return manager, nil
}

func validateConfig(config Config) error {
	if config.WorkspaceID == uuid.Nil || config.DesiredModelID == uuid.Nil {
		return errors.New("retrieval manager dependencies are required")
	}
	if config.Catalog == nil || config.Pending == nil || config.Projector == nil ||
		config.Jobs == nil || config.Factory == nil || config.Now == nil {
		return errors.New("retrieval manager dependencies are required")
	}
	return nil
}

// ReconcileModel ensures a transition Job exists when the desired Embedding Model is not serving.
func (m *Manager) ReconcileModel(ctx context.Context, modelID uuid.UUID) (domain.Job, error) {
	if modelID == uuid.Nil {
		return domain.Job{}, errors.New("embedding model ID is required")
	}
	active, err := m.catalog.GetOrCreateActive(ctx, m.workspaceID, modelID, m.now())
	if err != nil {
		return domain.Job{}, fmt.Errorf("load durable serving model: %w", err)
	}
	if active.DesiredModelID != uuid.Nil && active.DesiredModelID != modelID {
		return domain.Job{}, nil
	}
	if active.ModelID == modelID && active.Initialized {
		return domain.Job{}, nil
	}
	if err := m.validateRequestedModel(ctx, active, modelID); err != nil {
		return domain.Job{}, err
	}
	exists, err := m.catalog.ModelRequestHasJob(ctx, m.workspaceID, reindexKey(modelID, active.SelectionGeneration))
	if err != nil {
		return domain.Job{}, err
	}
	if exists {
		return domain.Job{}, nil
	}
	return enqueueRebuild(ctx, m.jobs, m.workspaceID, reindexArguments{ModelID: modelID, SelectionGeneration: active.SelectionGeneration})
}

func (m *Manager) validateRequestedModel(ctx context.Context, active domain.RetrievalIndex, modelID uuid.UUID) error {
	model, err := m.catalog.ResolveModel(ctx, modelID)
	if err != nil {
		return fmt.Errorf("resolve embedding model: %w", err)
	}
	if (!model.Enabled || model.InitializationStatus != domain.EmbeddingModelReady) && active.DesiredModelID != modelID {
		return errors.New("embedding model is not selectable")
	}
	return nil
}

// Embed returns the model's vector representation of text.
func (m *Manager) Embed(ctx context.Context, text string) ([]float32, error) {
	runtime, err := m.captureRuntime(ctx)
	if err != nil {
		return nil, err
	}
	return readGeneration(ctx, runtime, m.catalog, func() ([]float32, error) { return runtime.embedder.Embed(ctx, text) })
}

// QueryDenseText embeds the text and retrieves bounded dense matches satisfying the filter.
func (m *Manager) QueryDenseText(ctx context.Context, text string, filter ranking.IndexFilter, limit int) ([]ranking.IndexHit, error) {
	runtime, err := m.captureRuntime(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.ensureRuntime(ctx, runtime); err != nil {
		return nil, fmt.Errorf("ensure retrieval index generation %d: %w", runtime.generation.Generation, err)
	}
	vector, err := runtime.embedder.Embed(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("embed query for retrieval generation %d: %w", runtime.generation.Generation, err)
	}
	return readGeneration(ctx, runtime, m.catalog, func() ([]ranking.IndexHit, error) { return runtime.index.QueryDense(ctx, vector, filter, limit) })
}

// Ensure creates missing derived index structures and validates existing configuration.
func (m *Manager) Ensure(ctx context.Context) error {
	runtime, err := m.captureRuntime(ctx)
	if err != nil {
		return err
	}
	return m.ensureRuntime(ctx, runtime)
}

// QueryDense retrieves bounded dense-vector matches satisfying the filter.
func (m *Manager) QueryDense(ctx context.Context, vector []float32, filter ranking.IndexFilter, limit int) ([]ranking.IndexHit, error) {
	runtime, err := m.captureRuntime(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.ensureRuntime(ctx, runtime); err != nil {
		return nil, err
	}
	return readGeneration(ctx, runtime, m.catalog, func() ([]ranking.IndexHit, error) { return runtime.index.QueryDense(ctx, vector, filter, limit) })
}

// QuerySparse retrieves bounded lexical matches satisfying the filter.
func (m *Manager) QuerySparse(ctx context.Context, text string, filter ranking.IndexFilter, limit int) ([]ranking.IndexHit, error) {
	runtime, err := m.captureRuntime(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.ensureRuntime(ctx, runtime); err != nil {
		return nil, fmt.Errorf("ensure retrieval index generation %d: %w", runtime.generation.Generation, err)
	}
	return readGeneration(ctx, runtime, m.catalog, func() ([]ranking.IndexHit, error) { return runtime.index.QuerySparse(ctx, text, filter, limit) })
}

// QueryRecent returns recent Items satisfying the filter, bounded by limit.
func (m *Manager) QueryRecent(ctx context.Context, filter ranking.IndexFilter, limit int) ([]ranking.IndexHit, error) {
	runtime, err := m.captureRuntime(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.ensureRuntime(ctx, runtime); err != nil {
		return nil, fmt.Errorf("ensure retrieval index generation %d: %w", runtime.generation.Generation, err)
	}
	return readGeneration(ctx, runtime, m.catalog, func() ([]ranking.IndexHit, error) { return runtime.index.QueryRecent(ctx, filter, limit) })
}

// RetrieveTopic retrieves dense, sparse, and correction evidence using one Topic index request.
func (m *Manager) RetrieveTopic(
	ctx context.Context,
	request ranking.TopicIndexRequest,
) (ranking.TopicRetrieval, error) {
	runtime, err := m.captureRuntime(ctx)
	if err != nil {
		return ranking.TopicRetrieval{}, err
	}
	if err := m.ensureRuntime(ctx, runtime); err != nil {
		return ranking.TopicRetrieval{}, fmt.Errorf(
			"ensure retrieval index generation %d: %w", runtime.generation.Generation, err,
		)
	}
	return readGeneration(ctx, runtime, m.catalog, func() (ranking.TopicRetrieval, error) { return retrieveTopic(ctx, runtime, request) })
}

func retrieveTopic(ctx context.Context, runtime generationRuntime, request ranking.TopicIndexRequest) (ranking.TopicRetrieval, error) {
	var result ranking.TopicRetrieval
	if err := retrieveTopicCandidates(ctx, runtime, request, &result); err != nil {
		return ranking.TopicRetrieval{}, err
	}
	if err := embedTopicExclude(ctx, runtime, request, &result); err != nil {
		return ranking.TopicRetrieval{}, err
	}
	if request.TopicID != uuid.Nil && request.CorrectionEpoch > 0 {
		limit := manualIncludeLimit(request.Limit, result)
		corrections, err := runtime.index.QueryTopicCorrections(ctx, ranking.TopicCorrectionIndexRequest{
			TopicID: request.TopicID, CorrectionEpoch: request.CorrectionEpoch,
			Membership: domain.TopicMembershipInclude, QueryVector: result.IncludeEmbedding,
			Filter: request.Filter, Limit: limit,
		})
		if err != nil {
			return ranking.TopicRetrieval{}, fmt.Errorf("query current topic corrections: %w", err)
		}
		result.Corrections = corrections
	}
	return result, nil
}

func manualIncludeLimit(candidateLimit int, retrieved ranking.TopicRetrieval) int {
	unique := make(map[uuid.UUID]struct{}, len(retrieved.Dense)+len(retrieved.Sparse)+len(retrieved.Recent))
	for _, hits := range [][]ranking.IndexHit{retrieved.Dense, retrieved.Sparse, retrieved.Recent} {
		for _, hit := range hits {
			unique[hit.ItemID] = struct{}{}
		}
	}
	freeSlots := max(0, candidateLimit-len(unique))
	return min(candidateLimit, max(10, freeSlots))
}

// RetrievePersonal returns direct feedback and positive and negative similarity neighborhoods.
func (m *Manager) RetrievePersonal(
	ctx context.Context,
	request ranking.PersonalIndexRequest,
) (ranking.PersonalRetrieval, error) {
	runtime, err := m.captureRuntime(ctx)
	if err != nil {
		return ranking.PersonalRetrieval{}, err
	}
	if err := m.ensureRuntime(ctx, runtime); err != nil {
		return ranking.PersonalRetrieval{}, fmt.Errorf(
			"ensure retrieval index generation %d: %w", runtime.generation.Generation, err,
		)
	}
	return readGeneration(ctx, runtime, m.catalog, func() (ranking.PersonalRetrieval, error) { return runtime.index.RetrievePersonal(ctx, request) })
}

func retrieveTopicCandidates(
	ctx context.Context,
	runtime generationRuntime,
	request ranking.TopicIndexRequest,
	result *ranking.TopicRetrieval,
) error {
	if request.Include != "" {
		return retrieveIncludedTopic(ctx, runtime, request, result)
	}
	return retrieveRecentTopic(ctx, runtime, request, result)
}

func retrieveIncludedTopic(
	ctx context.Context,
	runtime generationRuntime,
	request ranking.TopicIndexRequest,
	result *ranking.TopicRetrieval,
) error {
	vector := request.IncludeEmbedding
	var err error
	if len(vector) == 0 {
		vector, err = runtime.embedder.Embed(ctx, request.Include)
	}
	if err != nil {
		return fmt.Errorf("embed topic include for retrieval generation %d: %w", runtime.generation.Generation, err)
	}
	result.IncludeEmbedding = vector
	result.Dense, err = runtime.index.QueryDense(ctx, vector, request.Filter, request.Limit)
	if err != nil {
		return fmt.Errorf("query topic dense candidates: %w", err)
	}
	result.Sparse, err = runtime.index.QuerySparse(ctx, request.Include, request.Filter, request.Limit)
	if err != nil {
		return fmt.Errorf("query topic sparse candidates: %w", err)
	}
	return nil
}

func retrieveRecentTopic(
	ctx context.Context,
	runtime generationRuntime,
	request ranking.TopicIndexRequest,
	result *ranking.TopicRetrieval,
) error {
	var err error
	result.Recent, err = runtime.index.QueryRecent(ctx, request.Filter, request.Limit)
	if err != nil {
		return fmt.Errorf("query recent topic candidates: %w", err)
	}
	return nil
}

func embedTopicExclude(
	ctx context.Context,
	runtime generationRuntime,
	request ranking.TopicIndexRequest,
	result *ranking.TopicRetrieval,
) error {
	if request.Exclude == "" {
		return nil
	}
	vector := request.ExcludeEmbedding
	var err error
	if len(vector) == 0 {
		vector, err = runtime.embedder.Embed(ctx, request.Exclude)
	}
	if err != nil {
		return fmt.Errorf("embed topic exclude for retrieval generation %d: %w", runtime.generation.Generation, err)
	}
	result.ExcludeEmbedding = vector
	return nil
}

func (m *Manager) handleReindex(ctx context.Context, payload json.RawMessage) (result error) {
	arguments, execution, err := decodeReindexExecution(ctx, payload)
	if err != nil {
		return err
	}
	progress := newSyncProgress(m.stallTimeout)
	progress.changed = m.jobProgressNotifier(execution.JobID)
	stepCtx, cancel := progress.context(ctx)
	generation, err := m.beginReindex(stepCtx, domain.ModelTransitionStart{
		WorkspaceID: m.workspaceID, ModelID: arguments.ModelID, JobID: execution.JobID, StartedAt: m.now(),
		SelectionGeneration: arguments.SelectionGeneration, LeaseOwner: execution.LeaseOwner, Force: arguments.Force,
	})
	cancel()
	if err != nil {
		return fmt.Errorf("begin retrieval index generation: %w", progress.failure(ctx, err))
	}
	progress.advanced()
	progress.announce(true)
	defer func() {
		if result == nil {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cleanupCancel()
		failure := m.catalog.Fail(cleanupCtx, domain.RetrievalIndexFailure{
			WorkspaceID: m.workspaceID, ModelID: generation.ModelID, JobID: execution.JobID,
			Generation: generation.Generation, Reason: result.Error(), FailedAt: m.now(),
			SelectionGeneration: arguments.SelectionGeneration, LeaseOwner: execution.LeaseOwner,
		})
		result = errors.Join(result, failure)
	}()

	if err := m.seedRebuild(ctx, domain.ModelProjectionSeedCommand{WorkspaceID: m.workspaceID, ModelID: generation.ModelID, JobID: execution.JobID, LeaseOwner: execution.LeaseOwner, Limit: 1000}, progress); err != nil {
		return err
	}
	if err := m.rebuild(ctx, generation.ModelID, progress); err != nil {
		return err
	}
	runtime, err := buildRuntime(m.factory, generation)
	if err != nil {
		return err
	}
	stepCtx, cancel = progress.context(ctx)
	incarnation, ensureErr := embeddingmodels.EnsureCollection(stepCtx, m.catalog, domain.ModelCollectionObservation{WorkspaceID: m.workspaceID, ModelID: generation.ModelID}, runtime.index)
	err = ensureErr
	cancel()
	if err != nil {
		return fmt.Errorf("ensure retrieval index generation %d: %w", generation.Generation, progress.failure(ctx, err))
	}
	progress.advanced()
	// Persist activation before publishing the runtime so a restart observes the same generation.
	activatedAt := m.now()
	cutover := domain.ModelTransitionCutover{
		WorkspaceID: m.workspaceID, ModelID: generation.ModelID,
		JobID: execution.JobID, ActivatedAt: activatedAt,
		SelectionGeneration: arguments.SelectionGeneration, LeaseOwner: execution.LeaseOwner,
	}
	if err := m.activateRebuild(ctx, cutover, progress); err != nil {
		return err
	}
	generation.Status = domain.RetrievalIndexActive
	generation.CollectionIncarnation = incarnation
	generation.Initialized = true
	generation.ActivatedAt = &activatedAt
	runtime.generation = generation
	m.mu.Lock()
	m.current = runtime
	m.mu.Unlock()
	if m.modelNotifier != nil {
		m.modelNotifier.NotifyModelChanged()
	}
	return nil
}

func (m *Manager) beginReindex(ctx context.Context, start domain.ModelTransitionStart) (domain.RetrievalIndex, error) {
	index, err := m.catalog.ResolveModelIndex(ctx, start.WorkspaceID, start.ModelID)
	if err != nil {
		return domain.RetrievalIndex{}, err
	}
	runtime, err := buildRuntime(m.factory, index)
	if err != nil {
		return domain.RetrievalIndex{}, err
	}
	_, err = embeddingmodels.EnsureCollection(ctx, m.catalog, domain.ModelCollectionObservation{WorkspaceID: start.WorkspaceID, ModelID: start.ModelID}, runtime.index)
	if err != nil {
		return domain.RetrievalIndex{}, err
	}
	generation, err := m.catalog.Begin(ctx, start)
	if errors.Is(err, domain.ErrConflict) {
		return domain.RetrievalIndex{}, jobs.Permanent(err)
	}
	return generation, err
}

func decodeReindexExecution(ctx context.Context, payload json.RawMessage) (reindexArguments, jobs.Execution, error) {
	var arguments reindexArguments
	if err := json.Unmarshal(payload, &arguments); err != nil || arguments.ModelID == uuid.Nil {
		return reindexArguments{}, jobs.Execution{}, jobs.Permanent(errors.New("invalid reindex-workspace arguments"))
	}
	execution, found := jobs.CurrentExecution(ctx)
	if !found || execution.JobID == uuid.Nil {
		return reindexArguments{}, jobs.Execution{}, jobs.Permanent(
			errors.New("reindex-workspace requires a durable job execution"),
		)
	}
	return arguments, execution, nil
}

func (m *Manager) rebuild(ctx context.Context, modelID uuid.UUID, progress *syncProgress) error {
	var after *uuid.UUID
	var failures error
	for {
		stepCtx, cancel := progress.context(ctx)
		commits, err := m.pending.ListPendingModelProjections(stepCtx, domain.ModelProjectionQuery{WorkspaceID: m.workspaceID, ModelID: modelID, AfterItemID: after, Limit: reindexBatch})
		cancel()
		if err != nil {
			return errors.Join(failures, fmt.Errorf("list pending projections for model %s: %w", modelID, progress.failure(ctx, err)))
		}
		if len(commits) == 0 {
			return failures
		}
		failed, deliveryErr := m.deliverRebuildBatch(ctx, modelID, commits, progress)
		if errors.Is(deliveryErr, domain.ErrModelSyncStalled) {
			return errors.Join(failures, deliveryErr)
		}
		if failures == nil {
			failures = deliveryErr
		}
		// A total provider outage must not turn into a million failing network requests.
		if ctx.Err() != nil || failed == len(commits) {
			return errors.Join(failures, ctx.Err())
		}
		last := commits[len(commits)-1].ItemID
		after = &last
	}
}

func (m *Manager) deliverRebuildBatch(ctx context.Context, modelID uuid.UUID, commits []domain.ItemProjectionCommit, progress *syncProgress) (int, error) {
	batch := rebuildAcknowledgements{modelID: modelID, failures: make(map[uuid.UUID]bool)}
	execution, _ := jobs.CurrentExecution(ctx)
	prepared := append([]domain.ItemProjectionCommit(nil), commits...)
	for i := range prepared {
		prepared[i].Execution = domain.ProjectionExecution{JobID: execution.JobID, LeaseOwner: execution.LeaseOwner, Attempt: execution.Attempt}
	}
	results, err := m.writeRebuildPage(ctx, prepared, progress)
	if err != nil {
		return len(commits), err
	}
	if len(results) != len(commits) {
		return len(commits), errors.New("projection writer returned an incomplete page")
	}
	for i, result := range results {
		batch.pending = append(batch.pending, result.Deliveries...)
		if result.Err != nil {
			batch.fail(commits[i].ItemID, result.Err)
		}
	}
	return m.completeRebuildBatch(ctx, batch, progress)
}

func (m *Manager) seedRebuild(ctx context.Context, command domain.ModelProjectionSeedCommand, progress *syncProgress) error {
	for {
		stepCtx, cancel := progress.context(ctx)
		done, err := m.catalog.SeedModelProjections(stepCtx, command)
		cancel()
		if err != nil {
			return fmt.Errorf("seed model projections: %w", progress.failure(ctx, err))
		}
		progress.advanced()
		if done {
			progress.announce(true)
			return nil
		}
	}
}

func (m *Manager) ensureRuntime(ctx context.Context, runtime generationRuntime) error {
	generation := runtime.generation
	actual, err := embeddingmodels.EnsureCollection(ctx, m.catalog, domain.ModelCollectionObservation{WorkspaceID: m.workspaceID, ModelID: generation.ModelID, Incarnation: generation.CollectionIncarnation}, runtime.index)
	if err != nil {
		return err
	}
	if generation.Initialized && generation.CollectionIncarnation == actual {
		return nil
	}
	active, found, err := m.catalog.GetActive(ctx, m.workspaceID)
	if err != nil {
		return err
	}
	if !found || !active.Initialized || active.ModelID != generation.ModelID || active.CollectionIncarnation != actual {
		return domain.ErrRetrievalUnavailable
	}
	return nil
}

func (m *Manager) captureRuntime(ctx context.Context) (generationRuntime, error) {
	generation, found, err := m.catalog.GetActive(ctx, m.workspaceID)
	if err != nil {
		return generationRuntime{}, err
	}
	if !found {
		return generationRuntime{}, domain.ErrRetrievalUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.current
	if !sameRuntime(runtime.generation, generation) {
		runtime, err = buildRuntime(m.factory, generation)
		if err != nil {
			return generationRuntime{}, err
		}
	}
	runtime.generation = generation
	m.current = runtime
	return runtime, nil
}

func sameRuntime(left, right domain.RetrievalIndex) bool {
	return left.ModelID == right.ModelID && left.Generation == right.Generation && left.Collection == right.Collection &&
		left.Embedding == right.Embedding && left.CollectionIncarnation == right.CollectionIncarnation
}

func buildRuntime(factory Factory, generation domain.RetrievalIndex) (generationRuntime, error) {
	embedder, index, err := factory.Build(generation)
	if err != nil {
		return generationRuntime{}, fmt.Errorf("build retrieval generation %d: %w", generation.Generation, err)
	}
	if embedder == nil || index == nil {
		return generationRuntime{}, errors.New("retrieval factory returned incomplete generation")
	}
	return generationRuntime{generation: generation, embedder: embedder, index: index}, nil
}
