package deps

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	benchservice "github.com/ethernity/obsidian-topic-stream/internal/service/bench"
	"github.com/ethernity/obsidian-topic-stream/internal/service/retrieval"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

// Bench owns the read-only resources used by the one-shot benchmark command.
type Bench struct {
	evaluator *benchservice.Evaluator
	store     *postgres.Store
	qdrant    *qdrant.Client
}

// NewBench acquires read-only benchmark dependencies; the caller owns Shutdown.
func NewBench(ctx context.Context, cfg generated.Config) (*Bench, error) {
	workspaceID, err := validateBenchConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	store, err := postgres.Open(ctx, cfg.DBPrimary.PostgresDSN)
	if err != nil {
		return nil, err
	}
	runtime, err := buildBench(ctx, cfg, workspaceID, store)
	if err != nil {
		store.Close()
	}
	return runtime, err
}

func buildBench(
	ctx context.Context,
	cfg generated.Config,
	workspaceID uuid.UUID,
	store *postgres.Store,
) (*Bench, error) {
	workspaceSettings, generation, err := loadBenchState(ctx, store, workspaceID)
	if err != nil {
		return nil, err
	}

	policy, err := qdrantSearchPolicy(cfg.Runtime)
	if err != nil {
		return nil, err
	}
	client, err := vectorindex.NewClient(cfg.Runtime.QdrantUrl)
	if err != nil {
		return nil, err
	}
	reader, err := retrieval.NewReader(retrieval.ReaderConfig{
		Generation: generation,
		Factory: retrievalFactory{
			qdrant: client, policy: policy,
			http: &http.Client{Timeout: time.Minute},
		},
	})
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	evaluator, err := benchservice.New(workspaceID, store, reader, workspaceSettings.Retrieval.RRFK)
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	return &Bench{evaluator: evaluator, store: store, qdrant: client}, nil
}

func loadBenchState(
	ctx context.Context,
	store *postgres.Store,
	workspaceID uuid.UUID,
) (domain.WorkspaceSettings, domain.RetrievalIndex, error) {
	workspaceSettings, found, err := store.GetWorkspaceSettings(ctx, workspaceID)
	if err != nil {
		return domain.WorkspaceSettings{}, domain.RetrievalIndex{}, err
	}
	if !found {
		return domain.WorkspaceSettings{}, domain.RetrievalIndex{}, errors.New(
			"workspace settings do not exist; start topic-stream before running bench",
		)
	}
	generation, found, err := store.GetActive(ctx, workspaceID)
	if err != nil {
		return domain.WorkspaceSettings{}, domain.RetrievalIndex{}, err
	}
	if !found {
		return domain.WorkspaceSettings{}, domain.RetrievalIndex{}, errors.New(
			"active retrieval generation does not exist; index content before running bench",
		)
	}
	return workspaceSettings, generation, nil
}

func validateBenchConfig(ctx context.Context, cfg generated.Config) (uuid.UUID, error) {
	if ctx == nil {
		return uuid.Nil, errors.New("runtime context is required")
	}
	if err := cfg.Validate(); err != nil {
		return uuid.Nil, fmt.Errorf("validate runtime configuration: %w", err)
	}
	if _, err := qdrantSearchPolicy(cfg.Runtime); err != nil {
		return uuid.Nil, err
	}
	workspaceID, err := uuid.Parse(cfg.Runtime.WorkspaceId)
	if err != nil {
		return uuid.Nil, fmt.Errorf("workspace ID must be a UUID: %w", err)
	}
	if cfg.DBPrimary.Kind != "postgres" {
		return uuid.Nil, fmt.Errorf("database kind %q is not supported", cfg.DBPrimary.Kind)
	}
	if strings.TrimSpace(cfg.DBPrimary.PostgresDSN) == "" {
		return uuid.Nil, errors.New("postgres DSN is required")
	}
	if strings.TrimSpace(cfg.Runtime.QdrantUrl) == "" {
		return uuid.Nil, errors.New("qdrant URL is required")
	}
	return workspaceID, nil
}

// Evaluate compares retrieval pipelines against the requested current-feedback split.
func (b *Bench) Evaluate(ctx context.Context, options benchservice.Options) (benchservice.Report, error) {
	return b.evaluator.Evaluate(ctx, options)
}

// Shutdown releases dependencies acquired by this runtime.
func (b *Bench) Shutdown(_ context.Context) error {
	b.store.Close()
	return b.qdrant.Close()
}
