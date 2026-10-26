package deps

import (
	"context"
	"fmt"

	"github.com/devctllabs/go-libs/di"
	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/sourceadapter"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/advisories"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feedpreparation"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/portability"
	"github.com/ethernity/obsidian-topic-stream/internal/service/projection"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/service/sources"
	"github.com/ethernity/obsidian-topic-stream/internal/service/summaries"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

type workspaceHandlerDependencies struct {
	config        generated.Config
	store         *postgres.Store
	jobs          *jobs.Service
	qdrant        *qdrant.Client
	reader        *sourceadapter.Reader
	settingsCache *settings.Cache
	queue         *advisories.PublishQueue
	projection    *projection.Service
}

func installWorkspaceHandlerLoader(container *di.Container, jobService *jobs.Service) error {
	dependencies, err := resolveWorkspaceHandlerDependencies(container, jobService)
	if err != nil {
		return err
	}
	jobService.SetWorkspaceHandlerLoader(func(ctx context.Context, workspaceID uuid.UUID) error {
		return buildWorkspaceHandlers(ctx, workspaceID, dependencies)
	})
	return nil
}

func resolveWorkspaceHandlerDependencies(
	resolver di.Resolver,
	jobService *jobs.Service,
) (workspaceHandlerDependencies, error) {
	config, err := di.Resolve[generated.Config](resolver)
	if err != nil {
		return workspaceHandlerDependencies{}, err
	}
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return workspaceHandlerDependencies{}, err
	}
	qdrantClient, err := di.Resolve[*qdrant.Client](resolver)
	if err != nil {
		return workspaceHandlerDependencies{}, err
	}
	reader, err := di.Resolve[*sourceadapter.Reader](resolver)
	if err != nil {
		return workspaceHandlerDependencies{}, err
	}
	cache, err := di.Resolve[*settings.Cache](resolver)
	if err != nil {
		return workspaceHandlerDependencies{}, err
	}
	queue, err := di.Resolve[*advisories.PublishQueue](resolver)
	if err != nil {
		return workspaceHandlerDependencies{}, err
	}
	projectionService, err := di.Resolve[*projection.Service](resolver)
	if err != nil {
		return workspaceHandlerDependencies{}, err
	}
	return workspaceHandlerDependencies{
		config: config, store: store, jobs: jobService, qdrant: qdrantClient,
		reader: reader, settingsCache: cache, queue: queue, projection: projectionService,
	}, nil
}

func buildWorkspaceHandlers(
	ctx context.Context,
	workspaceID uuid.UUID,
	dependencies workspaceHandlerDependencies,
) error {
	container := di.New()
	policy, err := qdrantSearchPolicy(dependencies.config.Runtime)
	if err != nil {
		return err
	}
	values := runtimeValues{context: ctx, workspaceID: workspaceID, searchPolicy: policy}
	workerEvents := events.NewForWorkspace(uuid.Nil, dependencies.queue)
	if err := registerWorkspaceHandlerProviders(container, values, workerEvents, dependencies); err != nil {
		return err
	}
	for _, resolve := range []func() error{
		func() error { _, err := di.Resolve[*sources.Service](container); return err },
		func() error { _, err := di.Resolve[*summaries.Service](container); return err },
		func() error { _, err := di.Resolve[*digests.Service](container); return err },
		func() error { _, err := di.Resolve[*portability.Service](container); return err },
		func() error { _, err := di.Resolve[*feedpreparation.Service](container); return err },
	} {
		if err := resolve(); err != nil {
			return fmt.Errorf("build workspace job handlers: %w", err)
		}
	}
	return nil
}

func registerWorkspaceHandlerProviders(
	container *di.Container,
	values runtimeValues,
	workerEvents *events.Service,
	dependencies workspaceHandlerDependencies,
) error {
	for _, register := range []func() error{
		func() error { return di.ProvideValue(container, dependencies.config) },
		func() error { return di.ProvideValue(container, values) },
		func() error { return di.ProvideValue(container, dependencies.store) },
		func() error { return di.ProvideValue(container, dependencies.jobs) },
		func() error { return di.ProvideValue(container, dependencies.qdrant) },
		func() error { return di.ProvideValue(container, dependencies.reader) },
		func() error { return di.ProvideValue(container, dependencies.settingsCache) },
		func() error { return di.ProvideValue(container, workerEvents) },
		func() error { return di.Provide(container, provideEmbeddingModels) },
		func() error { return di.ProvideValue(container, dependencies.projection) },
		func() error { return di.Provide(container, provideTopicCalibration) },
		func() error { return di.Provide(container, provideRetrieval) },
		func() error { return di.Provide(container, provideSettings) },
		func() error { return di.Provide(container, provideIndexing) },
		func() error { return di.Provide(container, provideIngest) },
		func() error { return di.Provide(container, provideSources) },
		func() error { return di.Provide(container, provideSummaries) },
		func() error { return di.Provide(container, provideRanking) },
		func() error { return di.Provide(container, provideFeedPreparation) },
		func() error { return di.Provide(container, provideDigests) },
		func() error { return di.Provide(container, providePortability) },
	} {
		if err := register(); err != nil {
			return fmt.Errorf("register workspace handler dependency: %w", err)
		}
	}
	return nil
}
