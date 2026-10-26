package deps

import (
	"time"

	"github.com/devctllabs/go-libs/di"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/generation"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/service/summaries"
)

func provideSummaries(resolver di.Resolver) (*summaries.Service, error) {
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
	jobService, err := di.Resolve[*jobs.Service](resolver)
	if err != nil {
		return nil, err
	}
	eventService, err := di.Resolve[*events.Service](resolver)
	if err != nil {
		return nil, err
	}
	return summaries.New(summaries.Config{WorkspaceID: values.workspaceID, Repository: store.Summaries(), Settings: settingsService, Jobs: jobService.ForWorkspace(values.workspaceID), Events: eventService, Generator: generation.NewSummaryClient(nil), Now: time.Now})
}
