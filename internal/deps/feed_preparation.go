package deps

import (
	"net/http"
	"time"

	"github.com/devctllabs/go-libs/di"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feedpreparation"
	"github.com/ethernity/obsidian-topic-stream/internal/service/indexing"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/ethernity/obsidian-topic-stream/internal/service/retrieval"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/service/topiccalibration"
	"github.com/qdrant/go-client/qdrant"
)

func provideFeedPreparation(resolver di.Resolver) (*feedpreparation.Service, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	values, err := di.Resolve[runtimeValues](resolver)
	if err != nil {
		return nil, err
	}
	queue, err := di.Resolve[*jobs.Service](resolver)
	if err != nil {
		return nil, err
	}
	settings, err := di.Resolve[*settings.Service](resolver)
	if err != nil {
		return nil, err
	}
	calibration, err := di.Resolve[*topiccalibration.Service](resolver)
	if err != nil {
		return nil, err
	}
	client, err := di.Resolve[*qdrant.Client](resolver)
	if err != nil {
		return nil, err
	}
	defaults := ranking.DefaultTopicThresholdDefaults("qwen3-embedding:0.6b")
	factory := feedIndexFactory{factory: retrievalFactory{qdrant: client, http: &http.Client{Timeout: time.Minute}, policy: values.searchPolicy}}
	return feedpreparation.New(feedpreparation.Config{WorkspaceID: values.workspaceID, Repository: store, Settings: settings, Preparer: ranking.NewFeedPreparer(factory, calibration, defaults), Jobs: queue.ForWorkspace(values.workspaceID), Now: time.Now, GateVersion: defaults.Version}), nil
}

type feedIndexFactory struct{ factory retrievalFactory }

// BuildFeedIndex constructs Topic candidate retrieval for the captured generation.
func (f feedIndexFactory) BuildFeedIndex(generation domain.RetrievalIndex) (ranking.TopicCandidateIndex, error) {
	return retrieval.NewReader(retrieval.ReaderConfig{Generation: generation, Factory: f.factory})
}

func provideFeedReconciler(resolver di.Resolver) (*feedpreparation.Reconciler, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	queue, err := di.Resolve[*jobs.Service](resolver)
	if err != nil {
		return nil, err
	}
	return feedpreparation.NewReconciler(store, queue, time.Now, ranking.DefaultTopicThresholdDefaults("qwen3-embedding:0.6b").Version), nil
}
func provideProjectionReconciler(resolver di.Resolver) (*indexing.Reconciler, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	queue, err := di.Resolve[*jobs.Service](resolver)
	if err != nil {
		return nil, err
	}
	return indexing.NewReconciler(store, queue), nil
}

// BuildPersonalIndex constructs personal-feedback retrieval for the captured generation.
func (f feedIndexFactory) BuildPersonalIndex(generation domain.RetrievalIndex) (ranking.PersonalIndex, error) {
	return retrieval.NewReader(retrieval.ReaderConfig{Generation: generation, Factory: f.factory})
}
