package feed

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

type historyToken struct {
	Kind     string
	Snapshot domain.FeedSnapshot
	After    int64
}

// Read traverses saved publications under a fixed shared head without ranking or publication.
func (s Service) Read(ctx context.Context, query domain.FeedReadQuery) (domain.FeedPage, error) {
	if s.history == nil {
		return domain.FeedPage{}, domain.ErrNotImplemented
	}
	query, err := s.readSelection(ctx, query)
	if err != nil {
		return domain.FeedPage{}, err
	}
	head, err := s.history.FeedHead(ctx, s.workspaceID)
	if err != nil {
		return domain.FeedPage{}, fmt.Errorf("read feed head: %w", err)
	}
	snapshot, before, err := s.readTraversal(query, head)
	if err != nil {
		return domain.FeedPage{}, err
	}
	rows, err := s.history.ReadFeedPublications(ctx, domain.FeedPublicationQuery{
		WorkspaceID: s.workspaceID, TopicID: query.TopicID, HeadOrdinal: snapshot.HeadOrdinal,
		BeforeOrdinal: before, Limit: query.Limit + 1,
	})
	if err != nil {
		return domain.FeedPage{}, fmt.Errorf("read feed publications: %w", err)
	}
	return savedFeedPage(rows, query.Limit, snapshot)
}

func (s Service) readSelection(ctx context.Context, query domain.FeedReadQuery) (domain.FeedReadQuery, error) {
	if query.Limit == 0 {
		query.Limit = 25
	}
	if query.Limit < 1 || query.Limit > 100 {
		return query, fmt.Errorf("%w: %w", ErrInvalidPageToken, domain.CheckRange("limit", float64(query.Limit), 1, 100))
	}
	if query.TopicID != nil {
		if _, err := s.feedTopic(ctx, *query.TopicID); err != nil {
			return query, err
		}
	}
	return query, nil
}

func savedFeedPage(rows []domain.FeedPublishedItem, limit int, snapshot domain.FeedSnapshot) (domain.FeedPage, error) {
	page := domain.FeedPage{GeneratedAt: snapshot.CreatedAt, Items: make([]domain.FeedEntry, 0)}
	var err error
	page.Snapshot, err = encodeToken(historyToken{Kind: "snapshot", Snapshot: snapshot})
	if err != nil {
		return domain.FeedPage{}, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		cursor, encodeErr := encodeToken(historyToken{Kind: "cursor", Snapshot: snapshot, After: rows[len(rows)-1].Publication.Ordinal})
		if encodeErr != nil {
			return domain.FeedPage{}, encodeErr
		}
		page.NextCursor = &cursor
	}
	for _, row := range rows {
		page.Items = append(page.Items, domain.FeedEntry{Item: row.Item, Score: row.Publication.Score,
			Signals: row.Publication.Signals, Pipeline: row.Publication.Pipeline})
	}
	return page, nil
}

func (s Service) readTraversal(query domain.FeedReadQuery, head int64) (domain.FeedSnapshot, int64, error) {
	snapshot, err := s.readSnapshot(query, head)
	if err != nil {
		return snapshot, 0, err
	}
	if query.Cursor == "" {
		return snapshot, 0, nil
	}
	var cursor historyToken
	if query.Snapshot == "" || decodeToken(query.Cursor, &cursor) != nil || cursor.Kind != "cursor" ||
		!sameSnapshot(snapshot, cursor.Snapshot) || cursor.After < 1 || cursor.After > snapshot.HeadOrdinal {
		return snapshot, 0, fmt.Errorf("%w: %w", ErrInvalidPageToken, domain.CheckCondition("cursor", domain.ValidationFormat, false))
	}
	return snapshot, cursor.After, nil
}

func (s Service) readSnapshot(query domain.FeedReadQuery, head int64) (domain.FeedSnapshot, error) {
	snapshot := domain.FeedSnapshot{Version: 1, WorkspaceID: s.workspaceID, TopicID: query.TopicID, HeadOrdinal: head, CreatedAt: s.now().UTC()}
	if query.Snapshot == "" {
		return snapshot, nil
	}
	var token historyToken
	if err := decodeToken(query.Snapshot, &token); err != nil || token.Kind != "snapshot" {
		return snapshot, fmt.Errorf("%w: %w", ErrInvalidPageToken, domain.CheckCondition("snapshot", domain.ValidationFormat, false))
	}
	snapshot = token.Snapshot
	if !validSnapshot(snapshot, query, s.workspaceID, head) {
		return snapshot, fmt.Errorf("%w: %w", ErrInvalidPageToken, domain.CheckCondition("snapshot", domain.ValidationFormat, false))
	}
	return snapshot, nil
}

func validSnapshot(snapshot domain.FeedSnapshot, query domain.FeedReadQuery, workspaceID uuid.UUID, head int64) bool {
	return snapshot.Version == 1 && snapshot.WorkspaceID == workspaceID && sameTopic(snapshot.TopicID, query.TopicID) &&
		snapshot.HeadOrdinal >= 0 && snapshot.HeadOrdinal <= head && !snapshot.CreatedAt.IsZero()
}

func sameTopic(first, second *uuid.UUID) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return *first == *second
}

func sameSnapshot(first, second domain.FeedSnapshot) bool {
	return first.Version == second.Version && first.WorkspaceID == second.WorkspaceID &&
		sameTopic(first.TopicID, second.TopicID) && first.HeadOrdinal == second.HeadOrdinal && first.CreatedAt.Equal(second.CreatedAt)
}
