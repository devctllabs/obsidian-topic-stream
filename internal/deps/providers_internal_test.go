package deps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devctllabs/go-libs/di"
	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/sourceadapter"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/advisories"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/projection"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/service/systemstatus"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceGraphsShareProcessOwnedProjectionLimiter(t *testing.T) {
	t.Parallel()
	root := di.New()
	require.NoError(t, di.ProvideValue(root, generated.Config{Runtime: generated.RuntimeConfig{
		EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1,
	}}))
	require.NoError(t, di.ProvideValue(root, &postgres.Store{}))
	require.NoError(t, di.ProvideValue(root, &qdrant.Client{}))
	require.NoError(t, di.ProvideValue(root, &sourceadapter.Reader{}))
	require.NoError(t, di.ProvideValue(root, &settings.Cache{}))
	require.NoError(t, di.ProvideValue(root, &advisories.PublishQueue{}))
	require.NoError(t, di.Provide(root, provideProjection))
	shared, err := di.Resolve[*projection.Service](root)
	require.NoError(t, err)
	dependencies, err := resolveWorkspaceHandlerDependencies(root, &jobs.Service{})
	require.NoError(t, err)
	for range 2 {
		workspace := di.New()
		values := runtimeValues{context: t.Context(), workspaceID: uuid.New()}
		require.NoError(t, registerWorkspaceHandlerProviders(workspace, values, events.NewForWorkspace(values.workspaceID, dependencies.queue), dependencies))
		service, resolveErr := di.Resolve[*projection.Service](workspace)
		require.NoError(t, resolveErr)
		require.Same(t, shared, service, "all Workspace delivery must use the same process-owned endpoint limiter")
	}
}

type settingsReaderStub struct {
	settings domain.WorkspaceSettings
}

// GetWorkspace returns the fixture's Workspace Settings snapshot.
func (s *settingsReaderStub) GetWorkspace(context.Context) (domain.WorkspaceSettings, error) {
	return s.settings, nil
}

func TestGenerationStatusComponentReadsCurrentOptIn(t *testing.T) {
	t.Parallel()

	reader := &settingsReaderStub{}
	component := generationStatusComponent(reader, http.DefaultClient)
	require.ErrorIs(t, component.Check(t.Context()), systemstatus.ErrNotConfigured)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	model := "local"
	reader.settings.Generation = domain.GenerationSettings{
		Enabled: true, Endpoint: pointerString(server.URL + "/v1"), SummaryModel: &model, RerankModel: &model,
	}
	component = generationStatusComponent(reader, server.Client())
	require.Equal(t, "generation", component.Name)
	require.False(t, component.Required)
	require.NoError(t, component.Check(t.Context()))
}

func pointerString(value string) *string { return &value }
