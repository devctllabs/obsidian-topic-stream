package vectorindex

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

func projectionDocument(value domain.ItemProjection) Document {
	item := value.Item
	return Document{ItemID: item.ID, WorkspaceID: item.WorkspaceID, SourceIDs: item.SourceIDs, Kind: item.Kind,
		Text: item.RetrievalText(), PublishedAt: item.PublishedAt, DiscoveredAt: item.DiscoveredAt,
		ProjectionRevision: value.Revision, Version: value.Version, Published: value.Published,
		Hidden: item.Visibility == domain.ItemVisibilityHidden, Feedback: value.Feedback, TopicCorrections: value.TopicCorrections}
}

func textFingerprint(text string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(text))) }

// A missing point can be inserted. Existing points accept only this version or an older one.
// Qdrant evaluates the condition atomically with the write, including competing first writes.
func projectionWriteFilter(document Document) *qdrant.Filter {
	return &qdrant.Filter{Must: []*qdrant.Condition{qdrant.NewMatch("workspaceId", document.WorkspaceID.String())},
		MustNot: []*qdrant.Condition{qdrant.NewRange("projectionVersion", &qdrant.Range{Gt: qdrant.PtrOf(float64(document.Version))})}}
}

func (i *Index) projectionPoint(ctx context.Context, itemID uuid.UUID) (*qdrant.RetrievedPoint, error) {
	points, err := i.backend.Get(ctx, &qdrant.GetPoints{CollectionName: i.collection, Ids: []*qdrant.PointId{qdrant.NewIDUUID(itemID.String())},
		WithPayload: qdrant.NewWithPayload(true), WithVectors: qdrant.NewWithVectors(false), ReadConsistency: qdrant.NewReadConsistency(qdrant.ReadConsistencyType_All)})
	if err != nil {
		return nil, fmt.Errorf("read projection delivery: %w", err)
	}
	if len(points) == 0 {
		return nil, nil
	}
	return points[0], nil
}

func (i *Index) verifyProjection(ctx context.Context, document Document) error {
	point, err := i.projectionPoint(ctx, document.ItemID)
	if err != nil {
		return err
	}
	if point == nil {
		return fmt.Errorf("%w: projection point missing after write", domain.ErrConflict)
	}
	payload := point.Payload
	if payload["workspaceId"].GetStringValue() != document.WorkspaceID.String() ||
		payload["projectionVersion"].GetIntegerValue() != document.Version ||
		payload["projectionRevision"].GetStringValue() != document.ProjectionRevision.String() ||
		payload["textFingerprint"].GetStringValue() != textFingerprint(document.Text) ||
		payload["published"].GetBoolValue() != document.Published || payload["hidden"].GetBoolValue() != document.Hidden {
		return fmt.Errorf("%w: projection changed during delivery", domain.ErrConflict)
	}
	return nil
}

// UpdateProjectionPayload replaces the complete canonical payload and preserves both vectors.
// False asks the caller to use the full embedding path for missing/changed text or legacy points.
func (i *Index) UpdateProjectionPayload(ctx context.Context, value domain.ItemProjection) (bool, error) {
	document := projectionDocument(value)
	point, err := i.projectionPoint(ctx, document.ItemID)
	if err != nil {
		return false, err
	}
	if point == nil {
		return false, nil
	}
	if point.Payload["workspaceId"].GetStringValue() != document.WorkspaceID.String() {
		return false, domain.ErrConflict
	}
	if point.Payload["projectionVersion"].GetIntegerValue() > document.Version {
		return false, domain.ErrConflict
	}
	if point.Payload["textFingerprint"].GetStringValue() != textFingerprint(document.Text) {
		return false, nil
	}
	filter := projectionWriteFilter(document)
	filter.Must = append(filter.Must, qdrant.NewHasID(qdrant.NewIDUUID(document.ItemID.String())), qdrant.NewMatch("textFingerprint", textFingerprint(document.Text)))
	_, err = i.backend.OverwritePayload(ctx, &qdrant.SetPayloadPoints{CollectionName: i.collection, Wait: qdrant.PtrOf(true),
		Ordering: &qdrant.WriteOrdering{Type: qdrant.WriteOrderingType_Strong}, Payload: documentPayload(document), PointsSelector: qdrant.NewPointsSelectorFilter(filter)})
	if err != nil {
		return false, fmt.Errorf("replace projection payload: %w", err)
	}
	return true, i.verifyProjection(ctx, document)
}
