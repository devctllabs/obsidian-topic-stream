package deps

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/devctllabs/go-libs/di"
	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/embeddings"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/generation"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/httpfetch"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/sourceadapter"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/advisories"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
	"github.com/ethernity/obsidian-topic-stream/internal/service/embeddingmodels"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feed"
	"github.com/ethernity/obsidian-topic-stream/internal/service/indexing"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ingest"
	"github.com/ethernity/obsidian-topic-stream/internal/service/items"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/portability"
	"github.com/ethernity/obsidian-topic-stream/internal/service/projection"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/ethernity/obsidian-topic-stream/internal/service/retrieval"
	"github.com/ethernity/obsidian-topic-stream/internal/service/search"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/service/sourcepoller"
	"github.com/ethernity/obsidian-topic-stream/internal/service/sources"
	"github.com/ethernity/obsidian-topic-stream/internal/service/summaries"
	"github.com/ethernity/obsidian-topic-stream/internal/service/systemstatus"
	"github.com/ethernity/obsidian-topic-stream/internal/service/topiccalibration"
	"github.com/ethernity/obsidian-topic-stream/internal/service/topics"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

func provideStore(container *di.Container) error {
	return di.ProvideResource(container, func(resolver di.Resolver) (*postgres.Store, error) {
		cfg, err := di.Resolve[generated.Config](resolver)
		if err != nil {
			return nil, err
		}
		values, err := di.Resolve[runtimeValues](resolver)
		if err != nil {
			return nil, err
		}
		store, err := postgres.Open(values.context, cfg.DBPrimary.PostgresDSN)
		if err != nil {
			return nil, err
		}
		if err := store.EnsureDefaultEmbeddingModel(values.context, bootstrapEmbeddingDescriptor(cfg.Bootstrap), time.Now()); err != nil {
			store.Close()
			return nil, fmt.Errorf("prepare bootstrap embedding model; run topic-stream db up first if the schema is absent: %w", err)
		}
		if values.workspaceID != uuid.Nil {
			if err := store.EnsureWorkspace(values.context, values.workspaceID); err != nil {
				store.Close()
				return nil, fmt.Errorf("prepare workspace; run topic-stream db up first if the schema is absent: %w", err)
			}
		}
		return store, nil
	}, func(_ context.Context, store *postgres.Store) error {
		store.Close()
		return nil
	})
}

func provideServices(container *di.Container) error {
	if err := provideRuntimeServices(container); err != nil {
		return err
	}
	return provideApplicationServices(container)
}

func provideRuntimeServices(container *di.Container) error {
	if err := provideRuntimeExecution(container); err != nil {
		return err
	}
	if err := provideRuntimeRetrieval(container); err != nil {
		return err
	}
	return provideRuntimeSettings(container)
}

func provideRuntimeExecution(container *di.Container) error {
	if err := di.Provide(container, provideModelReconciler); err != nil {
		return err
	}
	if err := di.Provide(container, provideFeedReconciler); err != nil {
		return err
	}
	if err := di.Provide(container, provideProjectionReconciler); err != nil {
		return err
	}
	if err := di.Provide(container, func(di.Resolver) (*advisories.PublishQueue, error) {
		return advisories.NewPublishQueue(), nil
	}); err != nil {
		return err
	}
	if err := di.Provide(container, func(resolver di.Resolver) (*events.Service, error) {
		values, err := di.Resolve[runtimeValues](resolver)
		if err != nil {
			return nil, err
		}
		queue, err := di.Resolve[*advisories.PublishQueue](resolver)
		if err != nil {
			return nil, err
		}
		return events.NewForWorkspace(values.workspaceID, queue), nil
	}); err != nil {
		return err
	}
	if err := di.Provide(container, provideSourceReader); err != nil {
		return err
	}
	if err := di.ProvideResource(container, provideJobs, func(ctx context.Context, service *jobs.Service) error {
		return service.Shutdown(ctx)
	}); err != nil {
		return err
	}
	return nil
}

func provideRuntimeRetrieval(container *di.Container) error {
	if err := di.ProvideResource(container, provideQdrant, func(_ context.Context, client *qdrant.Client) error {
		return client.Close()
	}); err != nil {
		return err
	}
	if err := di.Provide(container, provideEmbeddingModels); err != nil {
		return err
	}
	if err := di.Provide(container, provideProjection); err != nil {
		return err
	}
	if err := di.Provide(container, provideTopicCalibration); err != nil {
		return err
	}
	if err := di.Provide(container, provideRetrieval); err != nil {
		return err
	}
	return nil
}

func provideRuntimeSettings(container *di.Container) error {
	if err := di.Provide(container, provideSettingsCache); err != nil {
		return err
	}
	if err := di.Provide(container, provideSettings); err != nil {
		return err
	}
	if err := di.Provide(container, provideAdvisories); err != nil {
		return err
	}
	return nil
}

func provideAdvisories(resolver di.Resolver) (*advisories.Transport, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	cache, err := di.Resolve[*settings.Cache](resolver)
	if err != nil {
		return nil, err
	}
	eventService, err := di.Resolve[*events.Service](resolver)
	if err != nil {
		return nil, err
	}
	queue, err := di.Resolve[*advisories.PublishQueue](resolver)
	if err != nil {
		return nil, err
	}
	return advisories.New(advisories.Config{
		Backend: postgres.NewAdvisoryBackend(store), Settings: cache,
		Events: eventService, Queue: queue, RetryDelay: time.Second,
	})
}

func provideApplicationServices(container *di.Container) error {
	if err := provideContentServices(container); err != nil {
		return err
	}
	return provideDeliveryServices(container)
}

func provideContentServices(container *di.Container) error {
	if err := di.Provide(container, provideFeedPreparation); err != nil {
		return err
	}
	if err := di.Provide(container, provideRanking); err != nil {
		return err
	}
	if err := di.Provide(container, provideIndexing); err != nil {
		return err
	}
	if err := di.Provide(container, provideIngest); err != nil {
		return err
	}
	if err := di.Provide(container, provideSources); err != nil {
		return err
	}
	if err := di.Provide(container, func(resolver di.Resolver) (*sourcepoller.Service, error) {
		sourceService, err := di.Resolve[*sources.Service](resolver)
		if err != nil {
			return nil, err
		}
		service := sourcepoller.New(sourceService, time.Minute, time.Now)
		return &service, nil
	}); err != nil {
		return err
	}
	return nil
}

func provideDeliveryServices(container *di.Container) error {
	if err := di.Provide(container, provideSummaries); err != nil {
		return err
	}
	if err := di.Provide(container, provideFeed); err != nil {
		return err
	}
	if err := di.Provide(container, provideSearch); err != nil {
		return err
	}
	if err := di.Provide(container, provideItems); err != nil {
		return err
	}
	if err := di.Provide(container, provideTopics); err != nil {
		return err
	}
	if err := di.Provide(container, provideDigests); err != nil {
		return err
	}
	if err := di.Provide(container, providePortability); err != nil {
		return err
	}
	if err := di.Provide(container, provideStatus); err != nil {
		return err
	}
	return di.Provide[http.Handler](container, provideRouter)
}

func providePortability(resolver di.Resolver) (*portability.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	settingsService, err := di.Resolve[*settings.Service](resolver)
	if err != nil {
		return nil, err
	}
	embeddingModelService, err := di.Resolve[*embeddingmodels.Service](resolver)
	if err != nil {
		return nil, err
	}
	retrievalManager, err := di.Resolve[*retrieval.Manager](resolver)
	if err != nil {
		return nil, err
	}
	jobService, err := di.Resolve[*jobs.Service](resolver)
	if err != nil {
		return nil, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return nil, err
	}
	return portability.New(portability.Config{
		WorkspaceID: values.workspaceID, Repository: store, Settings: settingsService,
		EmbeddingModels: embeddingModelService, Retrieval: retrievalManager,
		Jobs: jobService.ForWorkspace(values.workspaceID), Now: time.Now,
	})
}

func provideJobs(resolver di.Resolver) (*jobs.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	eventService, err := di.Resolve[*events.Service](resolver)
	if err != nil {
		return nil, err
	}
	cfg, err := di.Resolve[generated.Config](resolver)
	if err != nil {
		return nil, err
	}
	config := jobConfig(cfg.Runtime, eventService)
	return jobs.New(store.Jobs(), time.Now, config), nil
}

func jobConfig(runtime generated.RuntimeConfig, notifier jobs.StateNotifier) jobs.Config {
	config := jobs.DefaultConfig()
	config.FetchConcurrency = runtime.WorkerFetchConcurrency
	config.GenerationConcurrency = runtime.WorkerGenerationConcurrency
	config.RetrievalConcurrency = runtime.WorkerRetrievalConcurrency
	config.FeedPreparationConcurrency = runtime.WorkerFeedPreparationConcurrency
	config.MaintenanceConcurrency = runtime.WorkerMaintenanceConcurrency
	config.Notifier = notifier
	return config
}

func provideSourceReader(resolver di.Resolver) (*sourceadapter.Reader, error) {
	cfg, err := di.Resolve[generated.Config](resolver)
	if err != nil {
		return nil, err
	}
	perHostConcurrency := max(cfg.Runtime.WorkerFetchPerHostConcurrency, 1)
	limiter, err := httpfetch.NewHostLimiter(perHostConcurrency)
	if err != nil {
		return nil, err
	}
	publicClient := httpfetch.NewClientWithLimiter(false, limiter)
	privateClient := httpfetch.NewClientWithLimiter(true, limiter)
	clientFactory := func(allowPrivateNetwork bool) *http.Client {
		if allowPrivateNetwork {
			return privateClient
		}
		return publicClient
	}
	return sourceadapter.NewReaderWithOptions(sourceadapter.Options{HTTPClientForNetwork: clientFactory}), nil
}

func provideIngest(resolver di.Resolver) (*ingest.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	reader, err := di.Resolve[*sourceadapter.Reader](resolver)
	if err != nil {
		return nil, err
	}
	indexer, err := di.Resolve[*indexing.Service](resolver)
	if err != nil {
		return nil, err
	}
	service := ingest.New(reader, store, indexer, time.Now)
	return &service, nil
}

func provideQdrant(resolver di.Resolver) (*qdrant.Client, error) {
	cfg, err := di.Resolve[generated.Config](resolver)
	if err != nil {
		return nil, err
	}
	return vectorindex.NewClient(cfg.Runtime.QdrantUrl)
}

func provideIndexing(resolver di.Resolver) (*indexing.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	queue, err := di.Resolve[*jobs.Service](resolver)
	if err != nil {
		return nil, err
	}
	projector, err := di.Resolve[*projection.Service](resolver)
	if err != nil {
		return nil, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return nil, err
	}
	eventService, err := di.Resolve[*events.Service](resolver)
	if err != nil {
		return nil, err
	}
	service, err := indexing.New(indexing.Config{
		WorkspaceID: values.workspaceID, Pending: store, Jobs: queue.ForWorkspace(values.workspaceID),
		Processor: projector, Events: eventService,
	})
	if err != nil {
		return nil, err
	}
	return service, nil
}

type retrievalFactory struct {
	qdrant *qdrant.Client
	http   *http.Client
	policy vectorindex.SearchPolicy
}

type projectionFactory struct {
	policy vectorindex.SearchPolicy
	qdrant *qdrant.Client
	http   *http.Client
}

type topicCalibrationFactory struct {
	policy      vectorindex.SearchPolicy
	workspaceID uuid.UUID
	qdrant      *qdrant.Client
	http        *http.Client
}

type topicCalibrationRuntime struct {
	workspaceID uuid.UUID
	embedder    *embeddings.OpenAI
	index       *vectorindex.Index
}

// Embed returns the model's vector representation of text.
func (r topicCalibrationRuntime) Embed(ctx context.Context, text string) ([]float32, error) {
	return r.embedder.Embed(ctx, text)
}

// DenseVectors returns stored dense vectors for the requested Items.
func (r topicCalibrationRuntime) DenseVectors(
	ctx context.Context,
	itemIDs []uuid.UUID,
) (map[uuid.UUID][]float32, error) {
	return r.index.DenseVectors(ctx, r.workspaceID, itemIDs)
}

// Build constructs embedding and index adapters for the supplied model or generation.
func (f topicCalibrationFactory) Build(model domain.EmbeddingModel) (topiccalibration.Runtime, error) {
	embedder, err := embeddings.NewOpenAI(model.Endpoint, model.ProviderModel, model.Dimensions, f.http)
	if err != nil {
		return nil, err
	}
	index, err := vectorindex.NewWithSearchPolicy(f.qdrant, vectorindex.CollectionName(model.ID), model.Dimensions, f.policy)
	if err != nil {
		return nil, err
	}
	return topicCalibrationRuntime{workspaceID: f.workspaceID, embedder: embedder, index: index}, nil
}

// Build constructs embedding and index adapters for the supplied model or generation.
func (f projectionFactory) Build(model domain.EmbeddingModel) (projection.Embedder, projection.Writer, error) {
	embedder, err := embeddings.NewOpenAI(model.Endpoint, model.ProviderModel, model.Dimensions, f.http)
	if err != nil {
		return nil, nil, err
	}
	index, err := vectorindex.NewWithSearchPolicy(f.qdrant, vectorindex.CollectionName(model.ID), model.Dimensions, f.policy)
	if err != nil {
		return nil, nil, err
	}
	return embedder, index, nil
}

func provideProjection(resolver di.Resolver) (*projection.Service, error) {
	cfg, err := di.Resolve[generated.Config](resolver)
	if err != nil {
		return nil, err
	}
	policy, err := qdrantSearchPolicy(cfg.Runtime)
	if err != nil {
		return nil, err
	}
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	client, err := di.Resolve[*qdrant.Client](resolver)
	if err != nil {
		return nil, err
	}
	return projection.NewWithOptions(store, projectionFactory{
		qdrant: client, http: &http.Client{Timeout: time.Minute}, policy: policy,
	}, projectionBatchOptions(cfg.Runtime))
}

func projectionBatchOptions(runtime generated.RuntimeConfig) projection.BatchOptions {
	return projection.BatchOptions{Size: runtime.EmbeddingBatchSize, InputBytes: runtime.EmbeddingBatchInputBytes, Concurrency: runtime.EmbeddingConcurrency}
}

// Build constructs embedding and index adapters for the supplied model or generation.
func (f retrievalFactory) Build(generation domain.RetrievalIndex) (retrieval.Embedder, retrieval.Index, error) {
	embedder, err := embeddings.NewOpenAI(
		generation.Embedding.Endpoint,
		generation.Embedding.Model,
		generation.Embedding.Dimensions,
		f.http,
	)
	if err != nil {
		return nil, nil, err
	}
	index, err := vectorindex.NewWithSearchPolicy(f.qdrant, generation.Collection, generation.Embedding.Dimensions, f.policy)
	if err != nil {
		return nil, nil, err
	}
	return embedder, index, nil
}

func provideRetrieval(resolver di.Resolver) (*retrieval.Manager, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	queue, err := di.Resolve[*jobs.Service](resolver)
	if err != nil {
		return nil, err
	}
	projector, err := di.Resolve[*projection.Service](resolver)
	if err != nil {
		return nil, err
	}
	calibration, err := di.Resolve[*topiccalibration.Service](resolver)
	if err != nil {
		return nil, err
	}
	client, err := di.Resolve[*qdrant.Client](resolver)
	if err != nil {
		return nil, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return nil, err
	}
	eventService, err := di.Resolve[*events.Service](resolver)
	if err != nil {
		return nil, err
	}
	workspaceSettings, err := store.GetOrCreateWorkspaceSettings(
		values.context, values.workspaceID, settings.DefaultWorkspaceSettings(time.Now()),
	)
	if err != nil {
		return nil, err
	}
	return retrieval.New(values.context, retrieval.Config{
		WorkspaceID: values.workspaceID, Catalog: store, Pending: store, Projector: projector,
		Jobs:             queue.ForWorkspace(values.workspaceID),
		ModelNotifier:    calibration,
		ProgressNotifier: eventService,
		Factory:          retrievalFactory{qdrant: client, http: &http.Client{Timeout: time.Minute}, policy: values.searchPolicy},
		DesiredModelID:   workspaceSettings.EmbeddingModelID, Now: time.Now,
	})
}

func provideTopicCalibration(resolver di.Resolver) (*topiccalibration.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	queue, err := di.Resolve[*jobs.Service](resolver)
	if err != nil {
		return nil, err
	}
	client, err := di.Resolve[*qdrant.Client](resolver)
	if err != nil {
		return nil, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return nil, err
	}
	return topiccalibration.New(topiccalibration.Config{
		WorkspaceID: values.workspaceID, Repository: store, Jobs: queue.ForWorkspace(values.workspaceID),
		Factory: topicCalibrationFactory{
			workspaceID: values.workspaceID, qdrant: client, http: &http.Client{Timeout: time.Minute}, policy: values.searchPolicy,
		},
		Defaults: ranking.DefaultTopicThresholdDefaults("qwen3-embedding:0.6b"), Now: time.Now,
	})
}

func provideSources(resolver di.Resolver) (*sources.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	ingester, err := di.Resolve[*ingest.Service](resolver)
	if err != nil {
		return nil, err
	}
	jobService, err := di.Resolve[*jobs.Service](resolver)
	if err != nil {
		return nil, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return nil, err
	}
	service := sources.New(sources.Config{
		WorkspaceID: values.workspaceID, Repository: store, Ingester: ingester,
		Jobs: jobService.ForWorkspace(values.workspaceID), Now: time.Now,
	})
	return &service, nil
}

func provideFeed(resolver di.Resolver) (*feed.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return nil, err
	}
	settingsService, err := di.Resolve[*settings.Service](resolver)
	if err != nil {
		return nil, err
	}
	client, err := di.Resolve[*qdrant.Client](resolver)
	if err != nil {
		return nil, err
	}
	eventService, err := di.Resolve[*events.Service](resolver)
	if err != nil {
		return nil, err
	}
	factory := feedIndexFactory{factory: retrievalFactory{qdrant: client, http: &http.Client{Timeout: time.Minute}, policy: values.searchPolicy}}
	service := feed.New(feed.Config{
		History: store, Topics: store, Admission: store, Settings: settingsService, Ranker: ranking.NewFeedRanker(factory), Events: eventService,
		GateVersion: ranking.DefaultTopicThresholdDefaults("qwen3-embedding:0.6b").Version, WorkspaceID: values.workspaceID, Now: time.Now,
	})
	return &service, nil
}

func provideRanking(resolver di.Resolver) (*ranking.Engine, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	retrievalManager, err := di.Resolve[*retrieval.Manager](resolver)
	if err != nil {
		return nil, err
	}
	settingsService, err := di.Resolve[*settings.Service](resolver)
	if err != nil {
		return nil, err
	}
	calibration, err := di.Resolve[*topiccalibration.Service](resolver)
	if err != nil {
		return nil, err
	}
	return ranking.NewEngine(ranking.EngineConfig{
		Index: retrievalManager, Profiles: settingsService, Thresholds: calibration,
		WorkspaceSettings: settingsService, Items: store,
		LLMFactory:        rerankClientFactory{client: &http.Client{Timeout: 35 * time.Second}},
		ThresholdDefaults: ranking.DefaultTopicThresholdDefaults("qwen3-embedding:0.6b"),
		Now:               time.Now,
	})
}

type rerankClientFactory struct {
	client *http.Client
}

// NewRerankClient constructs a reranking client from non-secret generation settings.
func (f rerankClientFactory) NewRerankClient(settings domain.GenerationSettings) (ranking.LLMRerankClient, error) {
	if settings.Endpoint == nil || settings.RerankModel == nil {
		return nil, fmt.Errorf("enabled LLM reranking requires generation endpoint and rerank model")
	}
	return generation.NewOpenAI(
		*settings.Endpoint,
		*settings.RerankModel,
		f.client,
	)
}

func provideSearch(resolver di.Resolver) (*search.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	retrievalManager, err := di.Resolve[*retrieval.Manager](resolver)
	if err != nil {
		return nil, err
	}
	settingsService, err := di.Resolve[*settings.Service](resolver)
	if err != nil {
		return nil, err
	}
	ranker, err := di.Resolve[*ranking.Engine](resolver)
	if err != nil {
		return nil, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return nil, err
	}
	return search.New(search.Config{
		WorkspaceID: values.workspaceID, Repository: store, Index: retrievalManager,
		Settings: settingsService, Now: time.Now, Topics: store, Ranker: ranker,
	})
}

func provideItems(resolver di.Resolver) (*items.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return nil, err
	}
	projector, err := di.Resolve[*projection.Service](resolver)
	if err != nil {
		return nil, err
	}
	calibration, err := di.Resolve[*topiccalibration.Service](resolver)
	if err != nil {
		return nil, err
	}
	return items.New(items.Config{
		WorkspaceID: values.workspaceID, Repository: store,
		Projector: projector, Calibration: calibration, ImageClient: httpfetch.NewClient(false), Now: time.Now,
	})
}

func provideDigests(resolver di.Resolver) (*digests.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return nil, err
	}
	ranker, err := di.Resolve[*ranking.Engine](resolver)
	if err != nil {
		return nil, err
	}
	jobService, err := di.Resolve[*jobs.Service](resolver)
	if err != nil {
		return nil, err
	}
	eventService, err := di.Resolve[*events.Service](resolver)
	if err != nil {
		return nil, err
	}
	settingsService, err := di.Resolve[*settings.Service](resolver)
	if err != nil {
		return nil, err
	}
	summaryService, err := di.Resolve[*summaries.Service](resolver)
	if err != nil {
		return nil, err
	}
	return digests.New(digests.Config{
		WorkspaceID: values.workspaceID, Repository: store, Topics: store, Ranker: ranker,
		Settings: settingsService, Items: store, Jobs: jobService.ForWorkspace(values.workspaceID),
		Events: eventService, Summaries: summaryService, Now: time.Now,
	})
}

func provideTopics(resolver di.Resolver) (*topics.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	indexer, err := di.Resolve[*indexing.Service](resolver)
	if err != nil {
		return nil, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return nil, err
	}
	calibration, err := di.Resolve[*topiccalibration.Service](resolver)
	if err != nil {
		return nil, err
	}
	service := topics.New(topics.Config{
		WorkspaceID: values.workspaceID, Repository: store, Projections: indexer,
		Calibration: calibration, Now: time.Now,
	})
	return &service, nil
}

func provideStatus(resolver di.Resolver) (*systemstatus.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	qdrantClient, err := di.Resolve[*qdrant.Client](resolver)
	if err != nil {
		return nil, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return nil, err
	}
	settingsService, err := di.Resolve[*settings.Service](resolver)
	if err != nil {
		return nil, err
	}
	components := []systemstatus.Component{
		{Name: "postgres", Required: true, Check: store.Ping},
		{Name: "qdrant", Check: func(ctx context.Context) error {
			_, healthErr := qdrantClient.HealthCheck(ctx)
			return healthErr
		}},
		generationStatusComponent(settingsService, &http.Client{Timeout: 5 * time.Second}),
	}
	service := systemstatus.New(systemstatus.Config{
		Version: values.version, WorkspaceID: values.workspaceID, Now: time.Now,
		Components: components,
	})
	return &service, nil
}

// workspaceSettingsReader reads the settings snapshot used when wiring generation clients.
type workspaceSettingsReader interface {
	// GetWorkspace returns the current Workspace Settings snapshot.
	GetWorkspace(ctx context.Context) (domain.WorkspaceSettings, error)
}

func generationStatusComponent(
	reader workspaceSettingsReader,
	client *http.Client,
) systemstatus.Component {
	return systemstatus.Component{
		Name: "generation",
		Check: func(ctx context.Context) error {
			workspaceSettings, err := reader.GetWorkspace(ctx)
			if err != nil {
				return err
			}
			if !workspaceSettings.Generation.Enabled {
				return systemstatus.ErrNotConfigured
			}
			if workspaceSettings.Generation.Endpoint == nil {
				return errors.New("enabled generation endpoint is missing")
			}
			return generation.HealthCheck(ctx, *workspaceSettings.Generation.Endpoint, client)
		},
	}
}

func provideSettingsCache(resolver di.Resolver) (*settings.Cache, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	return settings.NewCache(store, time.Now), nil
}

func provideSettings(resolver di.Resolver) (*settings.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return nil, err
	}
	retrievalManager, err := di.Resolve[*retrieval.Manager](resolver)
	if err != nil {
		return nil, err
	}
	cache, err := di.Resolve[*settings.Cache](resolver)
	if err != nil {
		return nil, err
	}
	return settings.NewWithCache(settings.ServiceConfig{
		WorkspaceID: values.workspaceID, Repository: store, Cache: cache,
		Now: time.Now, Retrieval: retrievalManager,
	}), nil
}

func provideRouter(resolver di.Resolver) (http.Handler, error) {
	dependencies, err := resolveRouterCore(resolver)
	if err != nil {
		return nil, err
	}
	if err := resolveRouterFeatures(resolver, &dependencies); err != nil {
		return nil, err
	}
	return httpapi.NewRouter(dependencies)
}

func resolveRouterCore(resolver di.Resolver) (httpapi.Dependencies, error) {
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return httpapi.Dependencies{}, err
	}
	status, err := di.Resolve[*systemstatus.Service](resolver)
	if err != nil {
		return httpapi.Dependencies{}, err
	}
	feedService, err := di.Resolve[*feed.Service](resolver)
	if err != nil {
		return httpapi.Dependencies{}, err
	}
	sourceService, err := di.Resolve[*sources.Service](resolver)
	if err != nil {
		return httpapi.Dependencies{}, err
	}
	jobService, err := di.Resolve[*jobs.Service](resolver)
	if err != nil {
		return httpapi.Dependencies{}, err
	}
	itemService, err := di.Resolve[*items.Service](resolver)
	if err != nil {
		return httpapi.Dependencies{}, err
	}
	return httpapi.Dependencies{
		WorkspaceID: values.workspaceID, Status: status, Feed: feedService, CORS: values.cors,
		Sources: sourceService, Jobs: jobService, Items: itemService,
	}, nil
}

func resolveRouterFeatures(resolver di.Resolver, dependencies *httpapi.Dependencies) error {
	summaryService, err := di.Resolve[*summaries.Service](resolver)
	if err != nil {
		return err
	}
	dependencies.Summaries = summaryService
	topicService, err := di.Resolve[*topics.Service](resolver)
	if err != nil {
		return err
	}
	embeddingModelService, err := di.Resolve[*embeddingmodels.Service](resolver)
	if err != nil {
		return err
	}
	settingsService, err := di.Resolve[*settings.Service](resolver)
	if err != nil {
		return err
	}
	searchService, err := di.Resolve[*search.Service](resolver)
	if err != nil {
		return err
	}
	eventService, err := di.Resolve[*events.Service](resolver)
	if err != nil {
		return err
	}
	digestService, err := di.Resolve[*digests.Service](resolver)
	if err != nil {
		return err
	}
	portabilityService, err := di.Resolve[*portability.Service](resolver)
	if err != nil {
		return err
	}
	dependencies.Topics = topicService
	dependencies.Settings = settingsService
	dependencies.EmbeddingModels = embeddingModelService
	dependencies.Search = searchService
	dependencies.Digests = digestService
	dependencies.Events = eventService
	dependencies.Portability = portabilityService
	return nil
}

func provideHTTPServer(container *di.Container) error {
	return di.ProvideResource(container, func(resolver di.Resolver) (*http.Server, error) {
		cfg, err := di.Resolve[generated.Config](resolver)
		if err != nil {
			return nil, err
		}
		handler, err := di.Resolve[http.Handler](resolver)
		if err != nil {
			return nil, err
		}
		return &http.Server{Addr: cfg.HTTP.Address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}, nil
	}, func(ctx context.Context, server *http.Server) error {
		return server.Shutdown(ctx)
	})
}
