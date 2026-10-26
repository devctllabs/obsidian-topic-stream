package postgres

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// One statement provides a consistent read-only linearization point for unchanged ingestion.
// Changed content, provenance or deferred feedback falls through to the coordinated transaction.
func (s *Store) unchangedItemProjection(ctx context.Context, upsert itemUpsert) (domain.ItemProjectionCommit, bool, error) {
	item, entry, encoded := upsert.item, upsert.sourceEntry, upsert.encoded
	var id uuid.UUID
	var revision *uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT i.id,
 (SELECT state.revision FROM item_projection_state state
  WHERE state.workspace_id=i.workspace_id AND state.item_id=i.id
  AND EXISTS(SELECT 1 FROM retrieval_projection_pending pending
   WHERE pending.workspace_id=state.workspace_id AND pending.item_id=state.item_id AND pending.revision=state.revision))
 FROM items i JOIN source_entries entry ON entry.item_id=i.id AND entry.source_id=$12 AND entry.external_id=$13
 WHERE i.workspace_id=$1 AND i.canonical_url=$6
 AND (i.kind,i.details,i.title,i.url,i.excerpt,i.content_markdown,i.extraction_version,i.authors,i.published_at,i.content_origin)
 IS NOT DISTINCT FROM ($2::text,$3::jsonb,$4::text,$5::text,$7::text,$8::text,$9::text,$10::jsonb,$11::timestamptz,$18::text)
 AND (entry.url,entry.details,entry.extraction_version,entry.published_at)
 IS NOT DISTINCT FROM ($14::text,$15::jsonb,$16::text,$17::timestamptz)
 AND NOT EXISTS(SELECT 1 FROM pending_item_feedback feedback
  WHERE feedback.workspace_id=i.workspace_id AND feedback.canonical_url=i.canonical_url)`,
		item.WorkspaceID, item.Kind, encoded.itemDetails, item.Title, item.URL, item.CanonicalURL, item.Excerpt, item.ContentMarkdown, item.ExtractionVersion, encoded.authors, item.PublishedAt,
		entry.SourceID, entry.ExternalID, entry.URL, encoded.sourceDetails, entry.ExtractionVersion, entry.PublishedAt, item.ContentOrigin).Scan(&id, &revision)
	if err == pgx.ErrNoRows {
		return domain.ItemProjectionCommit{}, false, nil
	}
	if err != nil {
		return domain.ItemProjectionCommit{}, false, fmt.Errorf("read unchanged item: %w", err)
	}
	if revision == nil {
		return domain.ItemProjectionCommit{}, true, nil
	}
	return projectionCommit(item.WorkspaceID, id, *revision), true, nil
}
