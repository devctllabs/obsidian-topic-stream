package deps

import (
	"github.com/devctllabs/go-libs/di"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/retrieval"
)

func provideModelReconciler(resolver di.Resolver) (*retrieval.Reconciler, error) {
	store, err := di.Resolve[*postgres.Store](resolver)
	if err != nil {
		return nil, err
	}
	queue, err := di.Resolve[*jobs.Service](resolver)
	if err != nil {
		return nil, err
	}
	return retrieval.NewReconciler(store, queue), nil
}
