package vectorindex

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const incarnationMetadataKey = "topic_stream_incarnation"

// CollectionIncarnation reads the collection's durable metadata marker; zero means unmarked.
func (i *Index) CollectionIncarnation(ctx context.Context) (uuid.UUID, error) {
	info, err := i.backend.GetCollectionInfo(ctx, i.collection)
	if status.Code(err) == codes.NotFound {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	if info == nil || info.Config == nil {
		return uuid.Nil, fmt.Errorf("collection %q has no readable metadata", i.collection)
	}
	marker := info.Config.Metadata[incarnationMetadataKey].GetStringValue()
	if marker == "" {
		return uuid.Nil, nil
	}
	value, err := uuid.Parse(marker)
	if err != nil {
		return uuid.Nil, fmt.Errorf("collection %q has invalid incarnation: %w", i.collection, err)
	}
	return value, nil
}

// SetCollectionIncarnation stamps a validated collection while the caller holds model coordination.
func (i *Index) SetCollectionIncarnation(ctx context.Context, incarnation uuid.UUID) error {
	return i.backend.UpdateCollection(ctx, &qdrant.UpdateCollection{CollectionName: i.collection,
		Metadata: map[string]*qdrant.Value{incarnationMetadataKey: qdrant.NewValueString(incarnation.String())}})
}
