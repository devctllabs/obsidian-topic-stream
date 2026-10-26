package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// refreshFeedDisplay maintains the positive read set under the caller's shared Feed head lock.
// Publication facts stay immutable. Any effective Topic membership also makes All eligible.
func refreshFeedDisplay(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, itemIDs []uuid.UUID) error {
	const batchSize = 2000
	for start := 0; start < len(itemIDs); start += batchSize {
		ids := itemIDs[start:min(start+batchSize, len(itemIDs))]
		args := []any{workspaceID, ids}
		if len(ids) > 1 {
			// Plan against actual bulk arrays; reusing a generic plan makes reconciliation much slower.
			args = append([]any{pgx.QueryExecModeDescribeExec}, args...)
		}
		if _, err := tx.Exec(ctx, refreshFeedDisplaySQL, args...); err != nil {
			return fmt.Errorf("refresh Feed display: %w", err)
		}
	}
	return nil
}

// Aggregate scoped corrections before per-Item membership. Correlating their join to Topics
// lets stale Workspace cardinality estimates multiply point lookups by every Topic in the Workspace.
const refreshFeedDisplaySQL = `WITH corrections AS MATERIALIZED (
 SELECT correction.item_id,
  array_agg(correction.topic_id) FILTER(WHERE correction.membership='include' AND topic.workspace_id=$1) AS added,
  array_agg(correction.topic_id) FILTER(WHERE correction.membership='exclude') AS excluded
 FROM topic_corrections correction LEFT JOIN topics topic ON topic.id=correction.topic_id
 WHERE correction.item_id=ANY($2::uuid[]) GROUP BY correction.item_id
), membership AS MATERIALIZED (
 SELECT p.workspace_id,p.item_id,p.ordinal,m.topic_ids,cardinality(p.matched_topic_ids)=0 AS unscoped
 FROM feed_publications p
 LEFT JOIN item_state s ON s.workspace_id=p.workspace_id AND s.item_id=p.item_id
 LEFT JOIN corrections correction ON correction.item_id=p.item_id
 CROSS JOIN LATERAL (
  SELECT ARRAY(SELECT DISTINCT member.topic_id
  FROM unnest(p.matched_topic_ids||COALESCE(correction.added,'{}'::uuid[])) member(topic_id)
  WHERE NOT (member.topic_id=ANY(COALESCE(correction.excluded,'{}'::uuid[])))
  ORDER BY member.topic_id) AS topic_ids
 ) m
 WHERE p.workspace_id=$1 AND p.item_id=ANY($2::uuid[])
 AND COALESCE(s.visibility,'visible')<>'hidden'
), eligible AS NOT MATERIALIZED (
 SELECT workspace_id,item_id,ordinal,topic_ids FROM membership WHERE unscoped OR cardinality(topic_ids)>0
), removed AS (
 DELETE FROM feed_display_entries d WHERE d.workspace_id=$1 AND d.item_id=ANY($2::uuid[])
 AND NOT EXISTS(SELECT 1 FROM eligible e WHERE e.item_id=d.item_id)
)
INSERT INTO feed_display_entries(workspace_id,item_id,ordinal,topic_ids)
SELECT eligible.workspace_id,eligible.item_id,eligible.ordinal,eligible.topic_ids FROM eligible
LEFT JOIN feed_display_entries current_display ON current_display.workspace_id=eligible.workspace_id AND current_display.item_id=eligible.item_id
WHERE current_display.topic_ids IS DISTINCT FROM eligible.topic_ids
ON CONFLICT(workspace_id,item_id) DO UPDATE SET topic_ids=EXCLUDED.topic_ids
WHERE feed_display_entries.topic_ids IS DISTINCT FROM EXCLUDED.topic_ids`
