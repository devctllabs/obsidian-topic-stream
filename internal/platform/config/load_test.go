package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/platform/config"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaultsAndWorkspace(t *testing.T) {
	t.Setenv("TOPIC_STREAM_WORKSPACE_ID", "018fda3a-4e36-7d1d-8835-45165d3f3596")

	loaded, err := config.Load(t.Context())

	require.NoError(t, err)
	require.Equal(t, "http://localhost:11434/v1", loaded.Bootstrap.BootstrapEmbeddingEndpoint)
	require.Equal(t, "qwen3-embedding:0.6b", loaded.Bootstrap.BootstrapEmbeddingModel)
	require.Equal(t, 1024, loaded.Bootstrap.BootstrapEmbeddingDimensions)
	require.Equal(t, "127.0.0.1:7331", loaded.HTTP.Address)
	require.True(t, loaded.HTTP.Enabled)
	require.Equal(t, 10*time.Second, loaded.Runtime.ShutdownTimeout)
	require.Equal(t, "018fda3a-4e36-7d1d-8835-45165d3f3596", loaded.Runtime.WorkspaceId)
	require.True(t, loaded.Runtime.SchedulersEnabled)
	require.True(t, loaded.Runtime.WorkersEnabled)
	require.Equal(t, 4, loaded.Runtime.WorkerFetchConcurrency)
	require.Equal(t, 1, loaded.Runtime.WorkerFetchPerHostConcurrency)
	require.Equal(t, 1, loaded.Runtime.WorkerGenerationConcurrency)
	require.Equal(t, 2, loaded.Runtime.WorkerRetrievalConcurrency)
	require.Equal(t, 2, loaded.Runtime.WorkerFeedPreparationConcurrency)
	require.Equal(t, 1, loaded.Runtime.WorkerMaintenanceConcurrency)
}

func TestLoadBootstrapFileAndProcessPresence(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile(".env", []byte("TOPIC_STREAM_BOOTSTRAP_EMBEDDING_ENDPOINT=http://file.example/v1\nTOPIC_STREAM_BOOTSTRAP_EMBEDDING_MODEL=file-model\nTOPIC_STREAM_BOOTSTRAP_EMBEDDING_DIMENSIONS=3\n"), 0o600))
	loaded, err := config.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, "http://file.example/v1", loaded.Bootstrap.BootstrapEmbeddingEndpoint)
	require.Equal(t, "file-model", loaded.Bootstrap.BootstrapEmbeddingModel)
	require.Equal(t, 3, loaded.Bootstrap.BootstrapEmbeddingDimensions)

	t.Setenv("TOPIC_STREAM_BOOTSTRAP_EMBEDDING_ENDPOINT", "http://process.example/v1")
	t.Setenv("TOPIC_STREAM_BOOTSTRAP_EMBEDDING_MODEL", "")
	loaded, err = config.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, "http://process.example/v1", loaded.Bootstrap.BootstrapEmbeddingEndpoint)
	require.Empty(t, loaded.Bootstrap.BootstrapEmbeddingModel, "explicit emptiness must reach runtime validation")
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	t.Setenv("TOPIC_STREAM_SHUTDOWN_TIMEOUT", "eventually")

	_, err := config.Load(t.Context())

	require.ErrorContains(t, err, "ShutdownTimeout")
}

func TestLoadCORSDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	loaded, err := config.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, "app://obsidian.md", loaded.HTTP.CorsAllowedOrigins)
}

func TestLoadCORSPreservesFileAndProcessPresence(t *testing.T) {
	for _, scenario := range []struct {
		name, file, want string
		process          *string
	}{
		{name: "file list", file: "https://file.example", want: "https://file.example"},
		{name: "file explicitly disabled", file: "", want: ""},
		{name: "process replaces file", file: "https://file.example", process: new("app://obsidian.md,https://process.example"), want: "app://obsidian.md,https://process.example"},
		{name: "empty process disables file", file: "https://file.example", process: new(""), want: ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.WriteFile(".env", []byte("TOPIC_STREAM_CORS_ALLOWED_ORIGINS="+scenario.file+"\n"), 0o600))
			if scenario.process != nil {
				t.Setenv("TOPIC_STREAM_CORS_ALLOWED_ORIGINS", *scenario.process)
			}
			loaded, err := config.Load(t.Context())
			require.NoError(t, err)
			require.Equal(t, scenario.want, loaded.HTTP.CorsAllowedOrigins)
		})
	}
}
