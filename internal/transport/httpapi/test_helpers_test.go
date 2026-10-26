package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi/mocks"
	"go.uber.org/mock/gomock"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/embeddingmodels"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feed"
	"github.com/ethernity/obsidian-topic-stream/internal/service/systemstatus"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newTestRouter(t *testing.T, overrides httpapi.Dependencies) http.Handler {
	t.Helper()
	dependencies := completeTestDependencies(t)
	applyDependencyOverrides(&dependencies, overrides)
	router, err := httpapi.NewRouter(dependencies)
	require.NoError(t, err)
	return router
}

func completeTestDependencies(t *testing.T) httpapi.Dependencies {
	controller := gomock.NewController(t)
	return httpapi.Dependencies{
		WorkspaceID: uuid.MustParse("018fda3a-4e36-7d1d-8835-45165d3f3596"),
		Status:      &systemstatus.Service{}, Feed: &feed.Service{}, Sources: mocks.NewMockSources(controller),
		Jobs: mocks.NewMockJobs(controller), Items: mocks.NewMockItems(controller), Topics: mocks.NewMockTopics(controller),
		Settings: mocks.NewMockSettings(controller), EmbeddingModels: &embeddingmodels.Service{}, Search: mocks.NewMockSearch(controller),
		Digests: mocks.NewMockDigests(controller), Events: events.NewForWorkspace(uuid.Nil, nil), Portability: &portabilityServiceStub{},
		Summaries: mocks.NewMockSummaries(controller),
	}
}

type portabilityServiceStub struct {
	archive   domain.WorkspaceArchive
	job       domain.Job
	imported  *domain.WorkspaceArchive
	exportErr error
	importErr error
}

// Export returns the configured archive or export failure.
func (s *portabilityServiceStub) Export(context.Context) (domain.WorkspaceArchive, error) {
	return s.archive, s.exportErr
}

// Import records the archive and returns the configured durable Job or failure.
func (s *portabilityServiceStub) Import(_ context.Context, archive domain.WorkspaceArchive) (domain.Job, error) {
	s.imported = &archive
	return s.job, s.importErr
}

func applyDependencyOverrides(target *httpapi.Dependencies, overrides httpapi.Dependencies) {
	applyCoreDependencyOverrides(target, overrides)
	applyFeatureDependencyOverrides(target, overrides)
}

func applyCoreDependencyOverrides(target *httpapi.Dependencies, overrides httpapi.Dependencies) {
	if overrides.WorkspaceID != uuid.Nil {
		target.WorkspaceID = overrides.WorkspaceID
	}
	if overrides.Status != nil {
		target.Status = overrides.Status
	}
	if overrides.Feed != nil {
		target.Feed = overrides.Feed
	}
	if overrides.Sources != nil {
		target.Sources = overrides.Sources
	}
	if overrides.Jobs != nil {
		target.Jobs = overrides.Jobs
	}
	if overrides.Items != nil {
		target.Items = overrides.Items
	}
}

func applyFeatureDependencyOverrides(target *httpapi.Dependencies, overrides httpapi.Dependencies) {
	if overrides.Summaries != nil {
		target.Summaries = overrides.Summaries
	}
	if overrides.Topics != nil {
		target.Topics = overrides.Topics
	}
	if overrides.Settings != nil {
		target.Settings = overrides.Settings
	}
	if overrides.EmbeddingModels != nil {
		target.EmbeddingModels = overrides.EmbeddingModels
	}
	if overrides.Search != nil {
		target.Search = overrides.Search
	}
	if overrides.Digests != nil {
		target.Digests = overrides.Digests
	}
	if overrides.Events != nil {
		target.Events = overrides.Events
	}
	if overrides.Portability != nil {
		target.Portability = overrides.Portability
	}
}
