package deps

import (
	"net/http"
	"testing"

	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/service/advisories"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/stretchr/testify/require"
)

func TestJobConfigUsesOperatorQueueConcurrency(t *testing.T) {
	t.Parallel()

	config := jobConfig(generated.RuntimeConfig{EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1,
		WorkerFetchConcurrency:       7,
		WorkerGenerationConcurrency:  3,
		WorkerRetrievalConcurrency:   5,
		WorkerMaintenanceConcurrency: 2,
	}, nil)

	require.Equal(t, 7, config.FetchConcurrency)
	require.Equal(t, 3, config.GenerationConcurrency)
	require.Equal(t, 5, config.RetrievalConcurrency)
	require.Equal(t, 2, config.MaintenanceConcurrency)
}

func TestTasksIncludeOnlyResolvedRuntimeRoles(t *testing.T) {
	t.Parallel()

	runtime := &API{
		server:     &http.Server{},
		jobs:       &jobs.Service{},
		advisories: &advisories.Transport{},
	}
	tasks := runtime.Tasks()
	names := make([]string, 0, len(tasks))
	for _, task := range tasks {
		names = append(names, task.Name)
	}

	require.Equal(t, []string{"postgres-advisories", "http-api", "job-worker"}, names)
}
