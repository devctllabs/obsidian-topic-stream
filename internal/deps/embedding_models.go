package deps

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/devctllabs/go-libs/di"
	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/embeddings"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/embeddingmodels"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

type embeddingModelInitializer struct {
	policy     vectorindex.SearchPolicy
	qdrant     *qdrant.Client
	store      *postgres.Store
	repository *postgres.EmbeddingModels
	http       *http.Client
	now        func() time.Time
}

// Probe checks that the embedding endpoint produces the descriptor's vector dimensions.
func (i embeddingModelInitializer) Probe(ctx context.Context, descriptor domain.EmbeddingModelDescriptor) error {
	client, err := embeddings.NewOpenAI(descriptor.Endpoint, descriptor.ProviderModel, descriptor.Dimensions, i.http)
	if err != nil {
		return err
	}
	_, err = client.Embed(ctx, "topic stream embedding model probe")
	return err
}

// Initialize prepares the Embedding Model's derived collection.
func (i embeddingModelInitializer) Initialize(ctx context.Context, model domain.EmbeddingModel) error {
	collection := vectorindex.CollectionName(model.ID)
	if err := i.setCollectionStatus(ctx, model.ID, domain.EmbeddingModelInitializing, nil); err != nil {
		return err
	}
	if err := i.Probe(ctx, model.EmbeddingModelDescriptor); err != nil {
		reason := "embedding provider probe failed"
		_ = i.setCollectionStatus(ctx, model.ID, domain.EmbeddingModelFailed, &reason)
		return err
	}
	index, err := vectorindex.NewWithSearchPolicy(i.qdrant, collection, model.Dimensions, i.policy)
	if err == nil {
		_, err = embeddingmodels.EnsureCollection(ctx, i.store, domain.ModelCollectionObservation{ModelID: model.ID}, index)
	}
	if err != nil {
		reason := "Qdrant collection initialization failed"
		_ = i.setCollectionStatus(ctx, model.ID, domain.EmbeddingModelFailed, &reason)
		return err
	}
	return i.setCollectionStatus(ctx, model.ID, domain.EmbeddingModelReady, nil)
}

func (i embeddingModelInitializer) setCollectionStatus(
	ctx context.Context,
	modelID uuid.UUID,
	status domain.EmbeddingModelStatus,
	failureReason *string,
) error {
	return i.repository.SetCollectionStatus(ctx, postgres.CollectionStatusUpdate{
		ModelID: modelID, Collection: vectorindex.CollectionName(modelID), Status: status,
		FailureReason: failureReason, UpdatedAt: i.now(),
	})
}

type embeddingModelDependencies struct {
	store  *postgres.Store
	qdrant *qdrant.Client
	values runtimeValues
}

func resolveEmbeddingModelDependencies(resolver di.Resolver) (embeddingModelDependencies, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return embeddingModelDependencies{}, err
	}
	client, err := di.Resolve[*qdrant.Client](resolver)
	if err != nil {
		return embeddingModelDependencies{}, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return embeddingModelDependencies{}, err
	}
	return embeddingModelDependencies{store: store, qdrant: client, values: values}, nil
}

func bootstrapEmbeddingDescriptor(cfg generated.BootstrapConfig) domain.EmbeddingModelDescriptor {
	return domain.EmbeddingModelDescriptor{
		Name:          "Default embedding model",
		Endpoint:      strings.TrimSpace(cfg.BootstrapEmbeddingEndpoint),
		ProviderModel: strings.TrimSpace(cfg.BootstrapEmbeddingModel),
		Dimensions:    cfg.BootstrapEmbeddingDimensions,
	}
}

func provideEmbeddingModels(resolver di.Resolver) (*embeddingmodels.Service, error) {
	dependencies, err := resolveEmbeddingModelDependencies(resolver)
	if err != nil {
		return nil, err
	}
	repository := dependencies.store.EmbeddingModels()
	service, err := embeddingmodels.New(embeddingmodels.Config{
		WorkspaceID: dependencies.values.workspaceID, Repository: repository,
		Initializer: embeddingModelInitializer{
			qdrant: dependencies.qdrant, repository: repository, policy: dependencies.values.searchPolicy,
			store: dependencies.store,
			http:  &http.Client{Timeout: time.Minute}, now: time.Now,
		},
		Now: time.Now,
		NewID: func() uuid.UUID {
			id, idErr := uuid.NewV7()
			if idErr != nil {
				return uuid.Nil
			}
			return id
		},
	})
	if err != nil {
		return nil, err
	}
	if err := ensureDefaultEmbeddingModel(dependencies.values, repository, service); err != nil {
		return nil, err
	}
	return service, nil
}

func ensureDefaultEmbeddingModel(
	values runtimeValues,
	repository *postgres.EmbeddingModels,
	service *embeddingmodels.Service,
) error {
	model, err := service.Get(values.context, domain.DefaultEmbeddingModelID)
	if err != nil {
		return fmt.Errorf("load default embedding model: %w", err)
	}
	if model.InitializationStatus != domain.EmbeddingModelReady {
		model, _ = service.Initialize(values.context, model.ID)
	}
	if model.InitializationStatus == domain.EmbeddingModelReady {
		if err := repository.EnsureWorkspaceState(values.context, values.workspaceID, model.ID, time.Now()); err != nil {
			return err
		}
	}
	return nil
}
