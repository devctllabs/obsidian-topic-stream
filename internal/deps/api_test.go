package deps_test

import (
	"context"
	"testing"
	"time"

	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewAPIValidatesEmbeddingBatchLimitsBeforeAcquiringResources(t *testing.T) {
	for name, change := range map[string]func(*generated.RuntimeConfig){
		"zero size":         func(r *generated.RuntimeConfig) { r.EmbeddingBatchSize = 0 },
		"too many inputs":   func(r *generated.RuntimeConfig) { r.EmbeddingBatchSize = 129 },
		"zero bytes":        func(r *generated.RuntimeConfig) { r.EmbeddingBatchInputBytes = 0 },
		"too many requests": func(r *generated.RuntimeConfig) { r.EmbeddingConcurrency = 9 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := generated.Config{Bootstrap: generated.BootstrapConfig{BootstrapEmbeddingEndpoint: "http://fixture/v1", BootstrapEmbeddingModel: "fixture", BootstrapEmbeddingDimensions: 3},
				DBPrimary: generated.DBPrimaryConfig{Kind: "postgres", PostgresDSN: "postgres://unreachable.invalid/test"},
				HTTP:      generated.HTTPConfig{Enabled: true, Address: "127.0.0.1:0"},
				Runtime:   generated.RuntimeConfig{WorkspaceId: uuid.NewString(), EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1}}
			change(&cfg.Runtime)
			ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
			defer cancel()
			_, err := deps.NewAPI(ctx, cfg, "test")
			require.ErrorContains(t, err, "projection batching")
			require.NotContains(t, err.Error(), "connect")
		})
	}
}

func TestNewAPIValidatesWorkspaceBeforeAcquiringResources(t *testing.T) {
	t.Parallel()

	_, err := deps.NewAPI(t.Context(), generated.Config{
		Bootstrap: generated.BootstrapConfig{BootstrapEmbeddingEndpoint: "http://fixture.example/v1", BootstrapEmbeddingModel: "synthetic", BootstrapEmbeddingDimensions: 3},
		DBPrimary: generated.DBPrimaryConfig{PostgresDSN: "postgres://unreachable.invalid/topic_stream"},
		HTTP:      generated.HTTPConfig{Address: "127.0.0.1:0", Enabled: true},
		Runtime:   generated.RuntimeConfig{EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1, WorkspaceId: "not-a-uuid"},
	}, "test")

	require.ErrorContains(t, err, "workspace ID")
	require.NotContains(t, err.Error(), "connect to Postgres")
}

func TestNewAPIAllowsWorkerOnlyRuntimeWithoutWorkspaceID(t *testing.T) {
	t.Parallel()

	_, err := deps.NewAPI(t.Context(), generated.Config{
		Bootstrap: generated.BootstrapConfig{BootstrapEmbeddingEndpoint: "http://fixture.example/v1", BootstrapEmbeddingModel: "synthetic", BootstrapEmbeddingDimensions: 3},
		DBPrimary: generated.DBPrimaryConfig{Kind: "postgres", PostgresDSN: "postgres://unreachable.invalid/topic_stream"},
		HTTP:      generated.HTTPConfig{Enabled: false, CorsAllowedOrigins: "*"},
		Runtime: generated.RuntimeConfig{EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1,
			SchedulersEnabled: false, WorkersEnabled: true,
			WorkerFetchConcurrency: 4, WorkerFetchPerHostConcurrency: 1,
			WorkerGenerationConcurrency: 1, WorkerRetrievalConcurrency: 2, WorkerFeedPreparationConcurrency: 2, WorkerMaintenanceConcurrency: 1,
		},
	}, "test")

	require.Error(t, err)
	require.NotContains(t, err.Error(), "workspace ID")
	require.NotContains(t, err.Error(), "CORS")
}

func TestNewAPIValidatesCORSBeforeAcquiringResources(t *testing.T) {
	for _, origin := range []string{"*", "https://*.example.com", "null", "https://example.com/path"} {
		t.Run(origin, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
			defer cancel()
			_, err := deps.NewAPI(ctx, generated.Config{
				Bootstrap: generated.BootstrapConfig{BootstrapEmbeddingEndpoint: "http://fixture/v1", BootstrapEmbeddingModel: "fixture", BootstrapEmbeddingDimensions: 3},
				DBPrimary: generated.DBPrimaryConfig{Kind: "postgres", PostgresDSN: "postgres://unreachable.invalid/test"},
				HTTP:      generated.HTTPConfig{Enabled: true, Address: "127.0.0.1:0", CorsAllowedOrigins: origin},
				Runtime:   generated.RuntimeConfig{WorkspaceId: uuid.NewString(), EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1},
			}, "test")
			require.ErrorContains(t, err, "CORS")
			require.NotContains(t, err.Error(), "connect")
		})
	}
}
