package vectorindex

import (
	"testing"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
)

func TestNewCollectionsReceiveDifferentIncarnations(t *testing.T) {
	var previous uuid.UUID
	for range 2 {
		backend := &backendSpy{}
		index, err := New(backend, "model_collection", 3)
		require.NoError(t, err)
		require.NoError(t, index.Ensure(t.Context()))
		marker := backend.createRequest.Metadata[incarnationMetadataKey].GetStringValue()
		actual, err := uuid.Parse(marker)
		require.NoError(t, err)
		require.NotEqual(t, previous, actual, "recreation must invalidate a previous checkpoint")
		previous = actual
	}
}

func TestCollectionIncarnationReadsExistingMetadata(t *testing.T) {
	actual := uuid.New()
	info := validCollectionInfo(3)
	info.Config.Metadata = map[string]*qdrant.Value{incarnationMetadataKey: qdrant.NewValueString(actual.String())}
	index, err := New(&backendSpy{exists: true, collectionInfo: info}, "model_collection", 3)
	require.NoError(t, err)
	observed, err := index.CollectionIncarnation(t.Context())
	require.NoError(t, err)
	require.Equal(t, actual, observed)
}
