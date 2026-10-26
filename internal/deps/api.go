package deps

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/devctllabs/go-libs/di"
	"github.com/devctllabs/go-libs/lifecycle"
	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/service/advisories"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feedpreparation"
	"github.com/ethernity/obsidian-topic-stream/internal/service/indexing"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/retrieval"
	"github.com/ethernity/obsidian-topic-stream/internal/service/sourcepoller"
	"github.com/ethernity/obsidian-topic-stream/internal/service/topiccalibration"
	"github.com/google/uuid"
)

// API owns the dependency container and independently enabled daemon roles.
type API struct {
	container             *di.Container
	server                *http.Server
	poller                *sourcepoller.Service
	jobs                  *jobs.Service
	digests               *digests.Service
	calibration           *topiccalibration.Service
	advisories            *advisories.Transport
	preparationReconciler *feedpreparation.Reconciler
	projectionReconciler  *indexing.Reconciler
	modelReconciler       *retrieval.Reconciler
}

type runtimeValues struct {
	context      context.Context
	version      string
	workspaceID  uuid.UUID
	searchPolicy vectorindex.SearchPolicy
}

// NewAPI builds the selected daemon roles and releases partial dependencies on failure.
// Callers must invoke Shutdown after successful construction.
func NewAPI(ctx context.Context, cfg generated.Config, version string) (_ *API, err error) {
	values, err := validateRuntime(ctx, cfg, version)
	if err != nil {
		return nil, err
	}

	container := di.New()
	defer func() {
		if err != nil {
			err = errors.Join(err, lifecycle.Shutdown(ctx, 5*time.Second, container.Shutdown))
		}
	}()
	for _, provide := range []func(*di.Container) error{
		func(container *di.Container) error { return di.ProvideValue(container, cfg) },
		func(container *di.Container) error { return di.ProvideValue(container, values) },
		provideStore,
		provideServices,
		provideHTTPServer,
	} {
		if err := provide(container); err != nil {
			return nil, fmt.Errorf("register API dependency: %w", err)
		}
	}

	advisoryTransport, err := di.Resolve[*advisories.Transport](container)
	if err != nil {
		return nil, fmt.Errorf("resolve advisory transport: %w", err)
	}
	runtime := &API{container: container, advisories: advisoryTransport}
	if err := resolveRuntimeRoles(container, cfg, runtime); err != nil {
		return nil, err
	}
	return runtime, nil
}

func resolveRuntimeRoles(container *di.Container, cfg generated.Config, runtime *API) error {
	var err error
	if cfg.HTTP.Enabled {
		runtime.server, err = di.Resolve[*http.Server](container)
		if err != nil {
			return fmt.Errorf("resolve HTTP server: %w", err)
		}
	}
	if cfg.Runtime.SchedulersEnabled {
		if err := resolveSchedulers(container, runtime); err != nil {
			return err
		}
	}
	if cfg.Runtime.WorkersEnabled {
		return resolveWorkers(container, runtime)
	}
	return nil
}

func resolveSchedulers(container *di.Container, runtime *API) error {
	var err error
	runtime.poller, err = di.Resolve[*sourcepoller.Service](container)
	if err != nil {
		return fmt.Errorf("resolve source poller: %w", err)
	}
	runtime.digests, err = di.Resolve[*digests.Service](container)
	if err != nil {
		return fmt.Errorf("resolve digest scheduler: %w", err)
	}
	runtime.calibration, err = di.Resolve[*topiccalibration.Service](container)
	if err != nil {
		return fmt.Errorf("resolve topic calibration scheduler: %w", err)
	}
	return nil
}

// Tasks returns the independently enabled runtime roles for lifecycle supervision.
func (a *API) Tasks() []lifecycle.Task {
	tasks := make([]lifecycle.Task, 0, 6)
	if a.advisories != nil {
		tasks = append(tasks, lifecycle.Task{
			Name: "postgres-advisories",
			Run:  a.advisories.Run,
		})
	}
	if a.server != nil {
		tasks = append(tasks, lifecycle.Task{
			Name: "http-api",
			Run:  a.serveHTTP,
		})
	}
	if a.poller != nil {
		tasks = append(tasks, lifecycle.Task{
			Name: "source-poller",
			Run:  a.poller.Run,
		})
	}
	if a.jobs != nil {
		tasks = append(tasks, lifecycle.Task{
			Name: "job-worker",
			Run:  a.jobs.Run,
		})
	}
	if a.preparationReconciler != nil {
		tasks = append(tasks, lifecycle.Task{Name: "feed-preparation-reconciler", Run: a.preparationReconciler.Run})
	}
	if a.projectionReconciler != nil {
		tasks = append(tasks, lifecycle.Task{Name: "item-projection-reconciler", Run: a.projectionReconciler.Run})
	}
	if a.modelReconciler != nil {
		tasks = append(tasks, lifecycle.Task{Name: "model-transition-reconciler", Run: a.modelReconciler.Run})
	}
	if a.digests != nil {
		tasks = append(tasks, lifecycle.Task{
			Name: "digest-scheduler",
			Run:  a.digests.Run,
		})
	}
	if a.calibration != nil {
		tasks = append(tasks, lifecycle.Task{
			Name: "topic-calibration-reconciler",
			Run:  a.calibration.Run,
		})
	}
	return tasks
}

func (a *API) serveHTTP(context.Context) error {
	err := a.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown releases dependencies acquired by this runtime.
func (a *API) Shutdown(ctx context.Context) error {
	return a.container.Shutdown(ctx)
}

func validateRuntime(ctx context.Context, cfg generated.Config, version string) (runtimeValues, error) {
	if ctx == nil {
		return runtimeValues{}, errors.New("runtime context is required")
	}
	if err := cfg.Validate(); err != nil {
		return runtimeValues{}, fmt.Errorf("validate runtime configuration: %w", err)
	}
	workspaceID, err := validateRuntimeRoles(cfg)
	if err != nil {
		return runtimeValues{}, err
	}
	if err := validateWorkerConfig(cfg); err != nil {
		return runtimeValues{}, err
	}
	if err := projectionBatchOptions(cfg.Runtime).Validate(); err != nil {
		return runtimeValues{}, err
	}
	searchPolicy, err := qdrantSearchPolicy(cfg.Runtime)
	if err != nil {
		return runtimeValues{}, err
	}
	if err := validateDatabaseConfig(cfg); err != nil {
		return runtimeValues{}, err
	}
	if err := domain.ValidateEmbeddingModelDescriptor(bootstrapEmbeddingDescriptor(cfg.Bootstrap)); err != nil {
		return runtimeValues{}, fmt.Errorf("bootstrap embedding configuration: %w", err)
	}
	return runtimeValues{context: ctx, version: version, workspaceID: workspaceID, searchPolicy: searchPolicy}, nil
}

func validateRuntimeRoles(cfg generated.Config) (uuid.UUID, error) {
	if !cfg.HTTP.Enabled && !cfg.Runtime.SchedulersEnabled && !cfg.Runtime.WorkersEnabled {
		return uuid.Nil, errors.New("at least one runtime role must be enabled")
	}
	if cfg.HTTP.Enabled && cfg.HTTP.Address == "" {
		return uuid.Nil, errors.New("HTTP address is required")
	}
	if !cfg.HTTP.Enabled && !cfg.Runtime.SchedulersEnabled {
		return uuid.Nil, nil
	}
	workspaceID, err := uuid.Parse(cfg.Runtime.WorkspaceId)
	if err != nil || workspaceID.Version() != 4 {
		return uuid.Nil, errors.New("workspace ID must be a UUIDv4 when HTTP or schedulers are enabled")
	}
	return workspaceID, nil
}

func validateDatabaseConfig(cfg generated.Config) error {
	if cfg.DBPrimary.Kind != "postgres" {
		return fmt.Errorf("database kind %q is not supported", cfg.DBPrimary.Kind)
	}
	if cfg.DBPrimary.PostgresDSN == "" {
		return errors.New("postgres DSN is required")
	}
	return nil
}

func validateWorkerConfig(cfg generated.Config) error {
	if !cfg.Runtime.WorkersEnabled {
		return nil
	}
	if cfg.Runtime.WorkerFetchConcurrency < 1 || cfg.Runtime.WorkerGenerationConcurrency < 1 ||
		cfg.Runtime.WorkerFeedPreparationConcurrency < 1 || cfg.Runtime.WorkerRetrievalConcurrency < 1 || cfg.Runtime.WorkerMaintenanceConcurrency < 1 {
		return errors.New("worker queue concurrency must be positive")
	}
	if cfg.Runtime.WorkerFetchPerHostConcurrency < 1 ||
		cfg.Runtime.WorkerFetchPerHostConcurrency > cfg.Runtime.WorkerFetchConcurrency {
		return errors.New("per-host fetch concurrency must be between one and fetch concurrency")
	}
	return nil
}

func resolveWorkers(container *di.Container, runtime *API) error {
	var err error
	runtime.jobs, err = di.Resolve[*jobs.Service](container)
	if err != nil {
		return fmt.Errorf("resolve job worker: %w", err)
	}
	if err := installWorkspaceHandlerLoader(container, runtime.jobs); err != nil {
		return err
	}
	runtime.preparationReconciler, err = di.Resolve[*feedpreparation.Reconciler](container)
	if err != nil {
		return err
	}
	runtime.projectionReconciler, err = di.Resolve[*indexing.Reconciler](container)
	if err != nil {
		return err
	}
	runtime.modelReconciler, err = di.Resolve[*retrieval.Reconciler](container)
	return err
}
