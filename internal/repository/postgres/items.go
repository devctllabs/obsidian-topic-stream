package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const itemColumns = `
i.id,i.workspace_id,ARRAY(SELECT DISTINCT se.source_id FROM source_entries se WHERE se.item_id=i.id ORDER BY se.source_id),i.kind,i.details,i.title,i.url,i.canonical_url,i.excerpt,
i.content_markdown,i.content_origin,i.extraction_version,i.authors,i.published_at,i.discovered_at,
COALESCE(s.reading_state,'unread'),COALESCE(s.visibility,'visible'),
f.sentiment,f.comment,f.created_at,f.updated_at`

// UpsertItem stores canonical content and Source Entry provenance, returning the projection commit and
// change flag.
func (s *Store) UpsertItem(
	ctx context.Context,
	item domain.Item,
	sourceEntry domain.SourceEntry,
) (domain.ItemProjectionCommit, bool, error) {
	if item.ContentOrigin == "" {
		item.ContentOrigin = domain.ContentOriginUnknown
	}
	encoded, err := encodeItemRecord(item, sourceEntry)
	if err != nil {
		return domain.ItemProjectionCommit{}, false, err
	}
	upsert := itemUpsert{item: item, sourceEntry: sourceEntry, encoded: encoded}
	commit, unchanged, err := s.unchangedItemProjection(ctx, upsert)
	if err != nil || unchanged {
		return commit, false, err
	}
	return s.upsertItemTransaction(ctx, upsert)
}

func (s *Store) upsertItemTransaction(ctx context.Context, upsert itemUpsert) (domain.ItemProjectionCommit, bool, error) {
	item := upsert.item
	tx, err := s.beginFeedChange(ctx, item.WorkspaceID)
	if err != nil {
		return domain.ItemProjectionCommit{}, false, fmt.Errorf("begin item upsert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	id, itemChanged, err := upsertCanonicalItem(ctx, tx, upsert)
	if err != nil {
		return domain.ItemProjectionCommit{}, false, err
	}
	sourceEntryChanged, err := upsertSourceEntry(ctx, tx, upsert, id)
	if err != nil {
		return domain.ItemProjectionCommit{}, false, err
	}
	feedbackChanged, err := applyPendingFeedback(ctx, tx, pendingFeedbackTarget{
		workspaceID: item.WorkspaceID, itemID: id, canonicalURL: item.CanonicalURL,
	})
	if err != nil {
		return domain.ItemProjectionCommit{}, false, err
	}
	changed := itemChanged || sourceEntryChanged || feedbackChanged
	commit, err := upsertProjectionCommit(ctx, tx, projectionCommitInput{
		upsert: upsert, itemID: id, changed: changed,
	})
	if err != nil {
		return domain.ItemProjectionCommit{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ItemProjectionCommit{}, false, fmt.Errorf("commit item upsert: %w", err)
	}
	return commit, changed, nil
}

type itemUpsert struct {
	item        domain.Item
	sourceEntry domain.SourceEntry
	encoded     encodedItemRecord
}

func upsertCanonicalItem(ctx context.Context, tx pgx.Tx, upsert itemUpsert) (uuid.UUID, bool, error) {
	item := upsert.item
	var id uuid.UUID
	var changed bool
	err := tx.QueryRow(ctx, `
WITH changed AS (
INSERT INTO items (id,workspace_id,kind,details,title,url,canonical_url,excerpt,content_markdown,extraction_version,authors,published_at,discovered_at,content_origin)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (workspace_id,canonical_url) DO UPDATE SET
  kind=EXCLUDED.kind,details=EXCLUDED.details,title=EXCLUDED.title,url=EXCLUDED.url,
  excerpt=EXCLUDED.excerpt,content_markdown=EXCLUDED.content_markdown,extraction_version=EXCLUDED.extraction_version,content_origin=EXCLUDED.content_origin,
  authors=EXCLUDED.authors,published_at=EXCLUDED.published_at
WHERE (items.kind,items.details,items.title,items.url,items.excerpt,items.content_markdown,items.extraction_version,items.authors,items.published_at,items.content_origin)
  IS DISTINCT FROM
  (EXCLUDED.kind,EXCLUDED.details,EXCLUDED.title,EXCLUDED.url,EXCLUDED.excerpt,EXCLUDED.content_markdown,EXCLUDED.extraction_version,EXCLUDED.authors,EXCLUDED.published_at,EXCLUDED.content_origin)
RETURNING id,true AS modified
)
SELECT id,modified FROM changed
UNION ALL SELECT id,false FROM items WHERE workspace_id=$2 AND canonical_url=$7
LIMIT 1`, item.ID, item.WorkspaceID, item.Kind, upsert.encoded.itemDetails, item.Title, item.URL,
		item.CanonicalURL, item.Excerpt, item.ContentMarkdown, item.ExtractionVersion, upsert.encoded.authors,
		item.PublishedAt, item.DiscoveredAt, item.ContentOrigin).Scan(&id, &changed)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("upsert item: %w", err)
	}
	return id, changed, nil
}

func upsertSourceEntry(ctx context.Context, tx pgx.Tx, upsert itemUpsert, itemID uuid.UUID) (bool, error) {
	sourceEntry := upsert.sourceEntry
	var changed bool
	err := tx.QueryRow(ctx, `
INSERT INTO source_entries (source_id,external_id,item_id,url,details,extraction_version,published_at,discovered_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (source_id,external_id) DO UPDATE SET item_id=EXCLUDED.item_id,url=EXCLUDED.url,
details=EXCLUDED.details,extraction_version=EXCLUDED.extraction_version,
published_at=EXCLUDED.published_at,updated_at=EXCLUDED.updated_at
WHERE (source_entries.item_id,source_entries.url,source_entries.details,source_entries.extraction_version,source_entries.published_at)
  IS DISTINCT FROM (EXCLUDED.item_id,EXCLUDED.url,EXCLUDED.details,EXCLUDED.extraction_version,EXCLUDED.published_at)
RETURNING true`, sourceEntry.SourceID, sourceEntry.ExternalID, itemID, sourceEntry.URL, upsert.encoded.sourceDetails,
		sourceEntry.ExtractionVersion, sourceEntry.PublishedAt, sourceEntry.DiscoveredAt, sourceEntry.UpdatedAt).Scan(&changed)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("upsert source entry: %w", err)
	}
	return changed, nil
}

type projectionCommitInput struct {
	upsert  itemUpsert
	itemID  uuid.UUID
	changed bool
}

func upsertProjectionCommit(ctx context.Context, tx pgx.Tx, input projectionCommitInput) (domain.ItemProjectionCommit, error) {
	item := input.upsert.item
	if input.changed {
		revision, err := markItemProjectionPending(ctx, tx, itemProjectionChange{
			workspaceID: item.WorkspaceID, itemID: input.itemID, updatedAt: input.upsert.sourceEntry.UpdatedAt,
		})
		if err != nil {
			return domain.ItemProjectionCommit{}, err
		}
		return projectionCommit(item.WorkspaceID, input.itemID, revision), nil
	}
	var revision uuid.UUID
	err := tx.QueryRow(ctx, `SELECT state.revision
FROM item_projection_state state
WHERE state.workspace_id=$1 AND state.item_id=$2
  AND EXISTS (
      SELECT 1 FROM retrieval_projection_pending pending
      WHERE pending.workspace_id=state.workspace_id AND pending.item_id=state.item_id
        AND pending.revision=state.revision
  )`, item.WorkspaceID, input.itemID).Scan(&revision)
	if err == pgx.ErrNoRows {
		return domain.ItemProjectionCommit{}, nil
	}
	if err != nil {
		return domain.ItemProjectionCommit{}, fmt.Errorf("load pending item projection: %w", err)
	}
	return projectionCommit(item.WorkspaceID, input.itemID, revision), nil
}

type pendingFeedbackTarget struct {
	workspaceID  uuid.UUID
	itemID       uuid.UUID
	canonicalURL string
}

func applyPendingFeedback(ctx context.Context, transaction pgx.Tx, target pendingFeedbackTarget) (bool, error) {
	command, err := transaction.Exec(ctx, `
WITH moved AS (
    DELETE FROM pending_item_feedback
    WHERE workspace_id=$1 AND canonical_url=$3
    RETURNING sentiment,comment,created_at,updated_at
)
INSERT INTO item_feedback (workspace_id,item_id,sentiment,comment,created_at,updated_at)
SELECT $1,$2,sentiment,comment,created_at,updated_at FROM moved
ON CONFLICT (workspace_id,item_id) DO UPDATE SET
sentiment=EXCLUDED.sentiment,comment=EXCLUDED.comment,updated_at=EXCLUDED.updated_at`,
		target.workspaceID, target.itemID, target.canonicalURL)
	if err != nil {
		return false, fmt.Errorf("apply pending item feedback: %w", err)
	}
	return command.RowsAffected() == 1, nil
}

type encodedItemRecord struct {
	authors       []byte
	itemDetails   []byte
	sourceDetails []byte
}

func encodeItemRecord(item domain.Item, sourceEntry domain.SourceEntry) (encodedItemRecord, error) {
	authors, err := json.Marshal(item.Authors)
	if err != nil {
		return encodedItemRecord{}, fmt.Errorf("encode item authors: %w", err)
	}
	itemDetails, err := json.Marshal(item.Details)
	if err != nil {
		return encodedItemRecord{}, fmt.Errorf("encode item details: %w", err)
	}
	sourceDetails, err := json.Marshal(sourceEntry.Details)
	if err != nil {
		return encodedItemRecord{}, fmt.Errorf("encode source entry details: %w", err)
	}
	return encodedItemRecord{authors: authors, itemDetails: itemDetails, sourceDetails: sourceDetails}, nil
}

// GetItem returns the Workspace Item and whether it exists.
func (s *Store) GetItem(ctx context.Context, workspaceID, itemID uuid.UUID) (domain.Item, bool, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+itemColumns+` FROM items i
LEFT JOIN item_state s ON s.workspace_id=i.workspace_id AND s.item_id=i.id
LEFT JOIN item_feedback f ON f.workspace_id=i.workspace_id AND f.item_id=i.id
WHERE i.workspace_id=$1 AND i.id=$2`, workspaceID, itemID)
	item, err := scanItem(row)
	if err == pgx.ErrNoRows {
		return domain.Item{}, false, nil
	}
	return item, err == nil, err
}

// SetFeedback stores current personal feedback and returns its projection commit and Item existence.
func (s *Store) SetFeedback(ctx context.Context, workspaceID, itemID uuid.UUID, feedback domain.Feedback) (domain.FeedbackCommit, bool, error) {
	tx, err := s.beginFeedChange(ctx, workspaceID)
	if err != nil {
		return domain.FeedbackCommit{}, false, fmt.Errorf("begin feedback update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var currentSentiment, currentComment *string
	var currentCreatedAt, currentUpdatedAt *time.Time
	var currentRevision *uuid.UUID
	var identical bool
	err = tx.QueryRow(ctx, `SELECT f.sentiment,f.comment,f.created_at,f.updated_at,p.revision,
COALESCE(f.sentiment=$3 AND f.comment IS NOT DISTINCT FROM $4,false)
FROM items i
LEFT JOIN item_feedback f ON f.workspace_id=i.workspace_id AND f.item_id=i.id
LEFT JOIN item_projection_state p ON p.workspace_id=i.workspace_id AND p.item_id=i.id
WHERE i.workspace_id=$1 AND i.id=$2
FOR UPDATE OF i`, workspaceID, itemID, feedback.Sentiment, feedback.Comment).Scan(
		&currentSentiment, &currentComment, &currentCreatedAt, &currentUpdatedAt, &currentRevision, &identical,
	)
	if err == pgx.ErrNoRows {
		return domain.FeedbackCommit{}, false, nil
	}
	if err != nil {
		return domain.FeedbackCommit{}, false, fmt.Errorf("lock feedback item: %w", err)
	}
	if identical && currentRevision != nil {
		return domain.FeedbackCommit{Feedback: domain.Feedback{
			Sentiment: *currentSentiment, Comment: currentComment,
			CreatedAt: *currentCreatedAt, UpdatedAt: *currentUpdatedAt,
		}, ItemProjectionCommit: projectionCommit(workspaceID, itemID, *currentRevision)}, true, nil
	}
	if identical {
		feedback = domain.Feedback{
			Sentiment: *currentSentiment, Comment: currentComment,
			CreatedAt: *currentCreatedAt, UpdatedAt: *currentUpdatedAt,
		}
	}
	err = tx.QueryRow(ctx, `
INSERT INTO item_feedback (workspace_id,item_id,sentiment,comment,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (workspace_id,item_id) DO UPDATE SET sentiment=EXCLUDED.sentiment,comment=EXCLUDED.comment,updated_at=EXCLUDED.updated_at
RETURNING sentiment,comment,created_at,updated_at`, workspaceID, itemID, feedback.Sentiment, feedback.Comment,
		feedback.CreatedAt, feedback.UpdatedAt).Scan(&feedback.Sentiment, &feedback.Comment, &feedback.CreatedAt, &feedback.UpdatedAt)
	if err != nil {
		return domain.FeedbackCommit{}, false, fmt.Errorf("upsert feedback: %w", err)
	}
	revision, err := markItemProjectionPending(ctx, tx, itemProjectionChange{
		workspaceID: workspaceID, itemID: itemID, updatedAt: feedback.UpdatedAt,
	})
	if err != nil {
		return domain.FeedbackCommit{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FeedbackCommit{}, false, fmt.Errorf("commit feedback update: %w", err)
	}
	return domain.FeedbackCommit{
		Feedback: feedback, ItemProjectionCommit: projectionCommit(workspaceID, itemID, revision),
	}, true, nil
}

type itemProjectionChange struct {
	workspaceID uuid.UUID
	itemID      uuid.UUID
	updatedAt   time.Time
}

// Retained inactive models accumulate only their latest revision; delivery separately selects
// serving and current transition destinations. The initial generation is the zero UUID.
const projectionDestinationsSQL = `SELECT d.model_id,COALESCE(p.generation,'00000000-0000-0000-0000-000000000000'::uuid) AS generation
FROM (SELECT serving_model_id AS model_id FROM workspace_retrieval_state WHERE workspace_id=$1
UNION SELECT transition_model_id FROM workspace_retrieval_state WHERE workspace_id=$1
UNION SELECT embedding_model_id FROM workspace_model_progress WHERE workspace_id=$1 AND retained) d
LEFT JOIN workspace_model_progress p ON p.workspace_id=$1 AND p.embedding_model_id=d.model_id`

func markItemProjectionPending(ctx context.Context, tx pgx.Tx, change itemProjectionChange) (uuid.UUID, error) {
	if err := lockRetrievalState(ctx, tx, change.workspaceID); err != nil {
		return uuid.Nil, err
	}
	revision, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("create item projection revision: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO item_projection_state (workspace_id,item_id,revision,updated_at)
VALUES ($1,$2,$3,$4)
ON CONFLICT (workspace_id,item_id) DO UPDATE SET revision=EXCLUDED.revision,version=item_projection_state.version+1,updated_at=EXCLUDED.updated_at`,
		change.workspaceID, change.itemID, revision, change.updatedAt); err != nil {
		return uuid.Nil, fmt.Errorf("update item projection revision: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO retrieval_projection_pending
(workspace_id,item_id,embedding_model_id,revision,updated_at,model_generation)
SELECT $1,$2,destination.model_id,$3,$4,destination.generation
FROM (`+projectionDestinationsSQL+`) destination
WHERE destination.model_id IS NOT NULL
ON CONFLICT (workspace_id,item_id,embedding_model_id) DO UPDATE SET
revision=EXCLUDED.revision,updated_at=EXCLUDED.updated_at,model_generation=EXCLUDED.model_generation`, change.workspaceID, change.itemID, revision, change.updatedAt); err != nil {
		return uuid.Nil, fmt.Errorf("mark item projection pending: %w", err)
	}
	return revision, nil
}

type itemProjectionBatchChange struct {
	workspaceID uuid.UUID
	itemIDs     []uuid.UUID
	updatedAt   time.Time
}

func markItemProjectionsPending(ctx context.Context, tx pgx.Tx, change itemProjectionBatchChange) ([]domain.ItemProjectionCommit, error) {
	if err := lockRetrievalState(ctx, tx, change.workspaceID); err != nil {
		return nil, err
	}
	commits := make([]domain.ItemProjectionCommit, 0, len(change.itemIDs))
	revisions := make([]uuid.UUID, 0, len(change.itemIDs))
	for _, id := range change.itemIDs {
		revision, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, revision)
		commits = append(commits, projectionCommit(change.workspaceID, id, revision))
	}
	const batchSize = 1000
	for start := 0; start < len(change.itemIDs); start += batchSize {
		end := min(start+batchSize, len(change.itemIDs))
		_, err := tx.Exec(ctx, `WITH advanced AS (
 MERGE INTO item_projection_state state
 USING (SELECT item_id,revision FROM unnest($2::uuid[],$3::uuid[]) batch(item_id,revision)) input
 ON state.workspace_id=$1 AND state.item_id=input.item_id
 WHEN MATCHED THEN UPDATE SET revision=input.revision,version=state.version+1,updated_at=$4
 WHEN NOT MATCHED THEN INSERT(workspace_id,item_id,revision,updated_at) VALUES($1,input.item_id,input.revision,$4)
 RETURNING state.item_id,state.revision
) MERGE INTO retrieval_projection_pending pending
USING (SELECT advanced.item_id,destination.model_id,advanced.revision,destination.generation FROM advanced
 CROSS JOIN (`+projectionDestinationsSQL+`) destination WHERE destination.model_id IS NOT NULL) desired
ON pending.workspace_id=$1 AND pending.item_id=desired.item_id AND pending.embedding_model_id=desired.model_id
WHEN MATCHED THEN UPDATE SET revision=desired.revision,updated_at=$4,model_generation=desired.generation
WHEN NOT MATCHED THEN INSERT(workspace_id,item_id,embedding_model_id,revision,updated_at,model_generation)
 VALUES($1,desired.item_id,desired.model_id,desired.revision,$4,desired.generation)`,
			change.workspaceID, change.itemIDs[start:end], revisions[start:end], change.updatedAt)
		if err != nil {
			return nil, fmt.Errorf("mark item projection batch pending: %w", err)
		}
	}
	return commits, nil
}

// ClearFeedback removes current personal feedback and returns the projection commit and Item existence.
func (s *Store) ClearFeedback(
	ctx context.Context,
	workspaceID, itemID uuid.UUID,
	now time.Time,
) (domain.ItemProjectionCommit, bool, error) {
	tx, err := s.beginFeedChange(ctx, workspaceID)
	if err != nil {
		return domain.ItemProjectionCommit{}, false, fmt.Errorf("begin feedback clear: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sentiment *string
	var currentRevision *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT f.sentiment,p.revision
FROM items i
LEFT JOIN item_feedback f ON f.workspace_id=i.workspace_id AND f.item_id=i.id
LEFT JOIN item_projection_state p ON p.workspace_id=i.workspace_id AND p.item_id=i.id
WHERE i.workspace_id=$1 AND i.id=$2
FOR UPDATE OF i`, workspaceID, itemID).Scan(&sentiment, &currentRevision)
	if err == pgx.ErrNoRows {
		return domain.ItemProjectionCommit{}, false, nil
	}
	if err != nil {
		return domain.ItemProjectionCommit{}, false, fmt.Errorf("lock feedback item: %w", err)
	}
	if sentiment == nil && currentRevision != nil {
		return projectionCommit(workspaceID, itemID, *currentRevision), true, nil
	}
	if sentiment != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM item_feedback WHERE workspace_id=$1 AND item_id=$2`, workspaceID, itemID); err != nil {
			return domain.ItemProjectionCommit{}, false, fmt.Errorf("delete feedback: %w", err)
		}
	}
	revision, err := markItemProjectionPending(ctx, tx, itemProjectionChange{
		workspaceID: workspaceID, itemID: itemID, updatedAt: now,
	})
	if err != nil {
		return domain.ItemProjectionCommit{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ItemProjectionCommit{}, false, fmt.Errorf("commit feedback clear: %w", err)
	}
	return projectionCommit(workspaceID, itemID, revision), true, nil
}

// ListCurrentFeedback returns current personal feedback for the Workspace.
func (s *Store) ListCurrentFeedback(ctx context.Context, workspaceID uuid.UUID) ([]domain.ItemFeedback, error) {
	rows, err := s.pool.Query(ctx, `
SELECT f.item_id,f.sentiment,f.comment,f.created_at,f.updated_at
FROM item_feedback f
JOIN items i ON i.workspace_id=f.workspace_id AND i.id=f.item_id
WHERE f.workspace_id=$1
ORDER BY f.updated_at DESC,f.item_id ASC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("query current feedback: %w", err)
	}
	defer rows.Close()
	result := make([]domain.ItemFeedback, 0)
	for rows.Next() {
		var value domain.ItemFeedback
		if err := rows.Scan(
			&value.ItemID, &value.Feedback.Sentiment, &value.Feedback.Comment,
			&value.Feedback.CreatedAt, &value.Feedback.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan current feedback: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current feedback: %w", err)
	}
	return result, nil
}

// SetReadingState updates the Item's reading state and reports whether it exists.
func (s *Store) SetReadingState(ctx context.Context, workspaceID, itemID uuid.UUID, state domain.ReadingState) (bool, error) {
	return s.setItemState(ctx, itemStateUpdate{workspaceID: workspaceID, itemID: itemID, readingState: &state})
}

// SetVisibility updates the Item's visibility and reports whether it exists.
func (s *Store) SetVisibility(ctx context.Context, workspaceID, itemID uuid.UUID, visibility domain.ItemVisibility) (bool, error) {
	tx, err := s.beginFeedChange(ctx, workspaceID)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current domain.ItemVisibility
	err = tx.QueryRow(ctx, `SELECT COALESCE(s.visibility,'visible') FROM items i LEFT JOIN item_state s ON s.workspace_id=i.workspace_id AND s.item_id=i.id WHERE i.workspace_id=$1 AND i.id=$2 FOR UPDATE OF i`, workspaceID, itemID).Scan(&current)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if current == visibility {
		return true, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO item_state(workspace_id,item_id,visibility) VALUES($1,$2,$3) ON CONFLICT(workspace_id,item_id) DO UPDATE SET visibility=EXCLUDED.visibility,updated_at=clock_timestamp()`, workspaceID, itemID, visibility); err != nil {
		return false, err
	}
	if _, err := markItemProjectionPending(ctx, tx, itemProjectionChange{workspaceID: workspaceID, itemID: itemID, updatedAt: time.Now().UTC()}); err != nil {
		return false, err
	}
	if err := refreshFeedDisplay(ctx, tx, workspaceID, []uuid.UUID{itemID}); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// SetTopicCorrection stores explicit Topic membership and returns the projection commit and Item
// existence.
func (s *Store) SetTopicCorrection(
	ctx context.Context,
	correction domain.TopicCorrection,
) (domain.ItemProjectionCommit, bool, error) {
	tx, err := s.beginFeedChange(ctx, correction.WorkspaceID)
	if err != nil {
		return domain.ItemProjectionCommit{}, false, fmt.Errorf("begin topic correction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, found, err := lockTopicCorrection(ctx, tx, correction)
	if err != nil || !found {
		return domain.ItemProjectionCommit{}, found, err
	}
	if state.identical(correction.Membership) && state.projectionRevision != nil {
		return projectionCommit(correction.WorkspaceID, correction.ItemID, *state.projectionRevision), true, nil
	}
	if err := writeTopicCorrection(ctx, tx, correction); err != nil {
		return domain.ItemProjectionCommit{}, false, err
	}
	revision, err := markItemProjectionPending(ctx, tx, itemProjectionChange{
		workspaceID: correction.WorkspaceID, itemID: correction.ItemID, updatedAt: correction.UpdatedAt,
	})
	if err != nil {
		return domain.ItemProjectionCommit{}, false, err
	}
	if err := requestFeedPreparation(ctx, tx, domain.FeedPreparationRequest{WorkspaceID: correction.WorkspaceID, TopicID: &correction.TopicID, Invalidate: true, At: correction.UpdatedAt}); err != nil {
		return domain.ItemProjectionCommit{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ItemProjectionCommit{}, false, fmt.Errorf("commit topic correction: %w", err)
	}
	return projectionCommit(correction.WorkspaceID, correction.ItemID, revision), true, nil
}

type topicCorrectionState struct {
	membership         *domain.TopicMembership
	projectionRevision *uuid.UUID
}

func lockTopicCorrection(
	ctx context.Context,
	tx pgx.Tx,
	correction domain.TopicCorrection,
) (topicCorrectionState, bool, error) {
	var state topicCorrectionState
	err := tx.QueryRow(ctx, `SELECT correction.membership,p.revision
FROM items i
JOIN topics t ON t.workspace_id=i.workspace_id
LEFT JOIN topic_corrections correction ON correction.item_id=i.id AND correction.topic_id=t.id
LEFT JOIN item_projection_state p ON p.workspace_id=i.workspace_id AND p.item_id=i.id
WHERE i.workspace_id=$1 AND i.id=$2 AND t.id=$3
FOR UPDATE OF i,t`, correction.WorkspaceID, correction.ItemID, correction.TopicID).Scan(
		&state.membership, &state.projectionRevision,
	)
	if err == pgx.ErrNoRows {
		return topicCorrectionState{}, false, nil
	}
	if err != nil {
		return topicCorrectionState{}, false, fmt.Errorf("lock topic correction: %w", err)
	}
	return state, true, nil
}

func (s topicCorrectionState) identical(membership domain.TopicMembership) bool {
	return membership == domain.TopicMembershipAutomatic && s.membership == nil ||
		s.membership != nil && *s.membership == membership
}

func writeTopicCorrection(ctx context.Context, tx pgx.Tx, correction domain.TopicCorrection) error {
	if correction.Membership == domain.TopicMembershipAutomatic {
		if _, err := tx.Exec(ctx, `DELETE FROM topic_corrections WHERE item_id=$1 AND topic_id=$2`, correction.ItemID, correction.TopicID); err != nil {
			return fmt.Errorf("clear topic correction: %w", err)
		}
	} else {
		_, err := tx.Exec(ctx, `
INSERT INTO topic_corrections (topic_id,item_id,membership,updated_at)
SELECT t.id,i.id,$4,$5 FROM items i,topics t
WHERE i.workspace_id=$1 AND i.id=$2 AND t.workspace_id=$1 AND t.id=$3
ON CONFLICT (topic_id,item_id) DO UPDATE SET
membership=EXCLUDED.membership,updated_at=EXCLUDED.updated_at`, correction.WorkspaceID, correction.ItemID,
			correction.TopicID, correction.Membership, correction.UpdatedAt)
		if err != nil {
			return fmt.Errorf("set topic correction: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE topics SET correction_version=correction_version+1
WHERE workspace_id=$1 AND id=$2`, correction.WorkspaceID, correction.TopicID); err != nil {
		return fmt.Errorf("advance topic correction version: %w", err)
	}
	return refreshFeedDisplay(ctx, tx, correction.WorkspaceID, []uuid.UUID{correction.ItemID})
}

func projectionCommit(workspaceID, itemID, revision uuid.UUID) domain.ItemProjectionCommit {
	return domain.ItemProjectionCommit{
		WorkspaceID: workspaceID, ItemID: itemID, ProjectionRevision: revision,
	}
}

type itemStateUpdate struct {
	workspaceID  uuid.UUID
	itemID       uuid.UUID
	readingState *domain.ReadingState
	visibility   *domain.ItemVisibility
}

func (s *Store) setItemState(ctx context.Context, update itemStateUpdate) (bool, error) {
	result, err := s.pool.Exec(ctx, `INSERT INTO item_state(workspace_id,item_id,reading_state,visibility)
 SELECT workspace_id,id,COALESCE($3,'unread'),COALESCE($4,'visible') FROM items WHERE workspace_id=$1 AND id=$2
 ON CONFLICT(workspace_id,item_id) DO UPDATE SET reading_state=COALESCE($3,item_state.reading_state),visibility=COALESCE($4,item_state.visibility),updated_at=clock_timestamp()`, update.workspaceID, update.itemID, update.readingState, update.visibility)
	if err != nil {
		return false, fmt.Errorf("update Item state: %w", err)
	}
	return result.RowsAffected() == 1, nil
}

// scanner decodes one SQL row without depending on a concrete pgx row type.
type scanner interface {
	// Scan decodes the current SQL row into the supplied destination pointers.
	Scan(destinations ...any) error
}

func scanItem(row scanner) (domain.Item, error) {
	var item domain.Item
	var authors []byte
	var details []byte
	var feedbackSentiment *string
	var feedbackComment *string
	var feedbackCreatedAt, feedbackUpdatedAt *time.Time
	if err := row.Scan(
		&item.ID, &item.WorkspaceID, &item.SourceIDs, &item.Kind, &details, &item.Title, &item.URL,
		&item.CanonicalURL, &item.Excerpt, &item.ContentMarkdown, &item.ContentOrigin, &item.ExtractionVersion, &authors, &item.PublishedAt, &item.DiscoveredAt,
		&item.ReadingState, &item.Visibility, &feedbackSentiment, &feedbackComment, &feedbackCreatedAt, &feedbackUpdatedAt,
	); err != nil {
		return domain.Item{}, err
	}
	if err := json.Unmarshal(authors, &item.Authors); err != nil {
		return domain.Item{}, fmt.Errorf("decode item authors: %w", err)
	}
	if err := json.Unmarshal(details, &item.Details); err != nil {
		return domain.Item{}, fmt.Errorf("decode item details: %w", err)
	}
	if feedbackSentiment != nil && feedbackCreatedAt != nil && feedbackUpdatedAt != nil {
		item.Feedback = &domain.Feedback{
			Sentiment: *feedbackSentiment, Comment: feedbackComment,
			CreatedAt: *feedbackCreatedAt, UpdatedAt: *feedbackUpdatedAt,
		}
	}
	item.TopicIDs = []uuid.UUID{}
	return item, nil
}
