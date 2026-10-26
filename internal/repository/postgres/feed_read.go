package postgres

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The positive access set is updated atomically with publication and canonical visibility/corrections.
// Hidden and all-original-Wrong prefixes never enter the ordered key scan.
const allFeedKeysSQL = `WITH selected AS MATERIALIZED (
 SELECT d.item_id,d.ordinal FROM feed_display_entries d
 WHERE d.workspace_id=$1
 AND d.ordinal<=LEAST($2::bigint,COALESCE(NULLIF($3::bigint,0)-1,$2::bigint))
 ORDER BY d.ordinal DESC LIMIT $4
)`

// Current Topic existence is checked separately: deleted original IDs may remain in the access
// array, while disabled Topics retain their repository view. Custom plans handle Topic skew.
const topicFeedKeysSQL = `WITH selected AS MATERIALIZED (
 SELECT d.item_id,d.ordinal FROM feed_display_entries d
 WHERE d.workspace_id=$1 AND d.topic_ids @> ARRAY[$4::uuid]
 AND EXISTS(SELECT 1 FROM topics t WHERE t.id=$4 AND t.workspace_id=$1)
 AND d.ordinal<=LEAST($2::bigint,COALESCE(NULLIF($3::bigint,0)-1,$2::bigint))
 ORDER BY d.ordinal DESC LIMIT $5
)`

const hydrateFeedPublicationsSQL = `SELECT ` + itemColumns + `,p.batch_id,p.published_at,p.ordinal,p.matched_topic_ids,p.topic_inputs,p.score,p.signals,p.pipeline,p.model_id,m.topic_ids
 FROM selected
 JOIN feed_publications p ON p.workspace_id=$1 AND p.item_id=selected.item_id
 JOIN items i ON i.workspace_id=p.workspace_id AND i.id=p.item_id
 LEFT JOIN item_state s ON s.workspace_id=i.workspace_id AND s.item_id=i.id
 LEFT JOIN item_feedback f ON f.workspace_id=i.workspace_id AND f.item_id=i.id
 CROSS JOIN LATERAL (
  SELECT COALESCE(array_agg(DISTINCT member.topic_id),'{}'::uuid[]) AS topic_ids FROM (
   SELECT unnest(p.matched_topic_ids) AS topic_id
   UNION SELECT c.topic_id FROM topic_corrections c WHERE c.item_id=p.item_id AND c.membership='include'
  ) member JOIN topics t ON t.id=member.topic_id AND t.workspace_id=p.workspace_id
  WHERE NOT EXISTS(SELECT 1 FROM topic_corrections wrong WHERE wrong.item_id=p.item_id AND wrong.topic_id=member.topic_id AND wrong.membership='exclude')
 ) m
 ORDER BY p.ordinal DESC`

// ReadFeedPublications filters visibility and current membership before the limit, in descending saved
// order.
func (s *Store) ReadFeedPublications(ctx context.Context, query domain.FeedPublicationQuery) ([]domain.FeedPublishedItem, error) {
	statement := allFeedKeysSQL + hydrateFeedPublicationsSQL
	args := []any{query.WorkspaceID, query.HeadOrdinal, query.BeforeOrdinal, query.Limit}
	if query.TopicID != nil {
		statement = topicFeedKeysSQL + hydrateFeedPublicationsSQL
		// Topic cardinality is heavily skewed; avoid a cached generic plan that scans all history.
		args = []any{pgx.QueryExecModeDescribeExec, query.WorkspaceID, query.HeadOrdinal, query.BeforeOrdinal, *query.TopicID, query.Limit}
	}
	rows, err := s.pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query saved feed: %w", err)
	}
	defer rows.Close()
	result := make([]domain.FeedPublishedItem, 0)
	for rows.Next() {
		entry := domain.FeedPublishedItem{Publication: domain.FeedPublication{WorkspaceID: query.WorkspaceID}}
		var matched []uuid.UUID
		value, err := scanItem(feedPublicationScanner{row: rows, publication: &entry.Publication, topics: &matched})
		if err != nil {
			return nil, fmt.Errorf("scan saved feed: %w", err)
		}
		value.TopicIDs = matched
		entry.Item = value
		entry.Publication.ItemID = value.ID
		result = append(result, entry)
	}
	return result, rows.Err()
}

type feedPublicationScanner struct {
	row         scanner
	publication *domain.FeedPublication
	topics      *[]uuid.UUID
}

// Scan appends publication destinations before decoding the joined Item row.
func (s feedPublicationScanner) Scan(fields ...any) error {
	p := s.publication
	fields = append(fields, &p.BatchID, &p.PublishedAt, &p.Ordinal, &p.MatchedTopicIDs, &p.TopicInputs, &p.Score, &p.Signals, &p.Pipeline, &p.ModelID, s.topics)
	return s.row.Scan(fields...)
}

// RecentUnpublishedFeed applies the ledger exclusion before the chronological limit.
func (s *Store) RecentUnpublishedFeed(ctx context.Context, query domain.FeedCandidateQuery) ([]domain.Item, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+itemColumns+` FROM items i
 LEFT JOIN item_state s ON s.workspace_id=i.workspace_id AND s.item_id=i.id
 LEFT JOIN item_feedback f ON f.workspace_id=i.workspace_id AND f.item_id=i.id
 WHERE i.workspace_id=$1 AND COALESCE(s.visibility,'visible')<>'hidden' AND COALESCE(f.sentiment,'')<>'dislike'
 AND COALESCE(i.published_at,i.discovered_at)>=$2::timestamptz-interval '30 days'
 AND COALESCE(i.published_at,i.discovered_at)<=$2
 AND NOT EXISTS(SELECT 1 FROM feed_publications p WHERE p.workspace_id=i.workspace_id AND p.item_id=i.id)
 ORDER BY COALESCE(i.published_at,i.discovered_at) DESC,i.id DESC LIMIT $3`, query.WorkspaceID, query.At, query.CandidateLimit)
	if err != nil {
		return nil, fmt.Errorf("read unpublished chronological feed: %w", err)
	}
	defer rows.Close()
	result := make([]domain.Item, 0)
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
