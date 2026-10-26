package retrieval_test

import (
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/retrieval"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestManagerConstructionLeavesDerivedIOToOperations(t *testing.T) {
	t.Parallel()
	catalog, modelID := catalogFor(domain.EmbeddingSettings{Endpoint: "http://fixture/v1", Model: "fixture", Dimensions: 3})
	factory := &factoryStub{catalog: catalog}
	manager, err := retrieval.New(t.Context(), retrieval.Config{
		WorkspaceID: uuid.New(), Catalog: catalog, Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{},
		Jobs: testQueue(t), Factory: factory, DesiredModelID: modelID, Now: time.Now,
	})
	require.NoError(t, err)
	require.Empty(t, factory.ensured, "construction must only bind derived capabilities")
	vector, err := manager.Embed(t.Context(), "query")
	require.NoError(t, err)
	require.Len(t, vector, 3)
	require.Empty(t, factory.ensured, "embedding a query against a ready collection does not provision it")
	require.NoError(t, manager.Ensure(t.Context()))
	require.NotEmpty(t, factory.ensured, "explicit initialization still ensures the captured collection")
}
