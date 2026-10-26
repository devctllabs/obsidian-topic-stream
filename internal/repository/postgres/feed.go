package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// FeedHead reads the committed Workspace head, returning zero when no history exists.
func (s *Store) FeedHead(ctx context.Context, workspaceID uuid.UUID) (int64, error) {
	var head int64
	err := s.pool.QueryRow(ctx, `SELECT head_ordinal FROM feed_state WHERE workspace_id=$1`, workspaceID).Scan(&head)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return head, err
}

func lockFeedHead(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID) (int64, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO feed_state(workspace_id) VALUES($1) ON CONFLICT DO NOTHING`, workspaceID); err != nil {
		return 0, fmt.Errorf("initialize feed head: %w", err)
	}
	var head int64
	err := tx.QueryRow(ctx, `SELECT head_ordinal FROM feed_state WHERE workspace_id=$1 FOR UPDATE`, workspaceID).Scan(&head)
	return head, err
}

// PublishFeed commits eligible, previously unpublished Items under the shared Workspace head.
func (s *Store) PublishFeed(ctx context.Context, command domain.FeedPublishCommand) (domain.FeedPublishResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.FeedPublishResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	head, err := lockFeedHead(ctx, tx, command.WorkspaceID)
	if err != nil {
		return domain.FeedPublishResult{}, err
	}
	result := domain.FeedPublishResult{HeadOrdinal: head, Publications: []domain.FeedPublication{}}
	if len(command.Publications) == 0 {
		return result, tx.Commit(ctx)
	}
	result.Publications, err = insertFeedPublications(ctx, tx, command, head)
	if err != nil {
		return result, err
	}
	if len(result.Publications) > 0 {
		result.HeadOrdinal = head + int64(len(result.Publications))
		if err := completeFeedPublication(ctx, tx, command, result); err != nil {
			return domain.FeedPublishResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FeedPublishResult{}, err
	}
	return result, nil
}

func insertFeedPublications(ctx context.Context, tx pgx.Tx, command domain.FeedPublishCommand, head int64) ([]domain.FeedPublication, error) {
	payload, err := json.Marshal(command.Publications)
	if err != nil {
		return nil, err
	}
	batchID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, publishFeedSQL, command.WorkspaceID, command.PublishedAt, payload, head, batchID, command.TopicID)
	if err != nil {
		return nil, fmt.Errorf("insert feed publications: %w", err)
	}
	defer rows.Close()
	result := make([]domain.FeedPublication, 0)
	for rows.Next() {
		publication, err := scanFeedPublication(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, publication)
	}
	return result, rows.Err()
}

const publishFeedSQL = `
WITH proposed AS (
 SELECT x.*,entry.position FROM jsonb_array_elements($3::jsonb) WITH ORDINALITY entry(value,position)
 CROSS JOIN LATERAL jsonb_to_record(entry.value) x("ItemID" uuid,"MatchedTopicIDs" uuid[],"TopicInputs" jsonb,"Score" double precision,"Signals" jsonb,"Pipeline" text,"ModelID" uuid)
), unique_proposed AS (
 SELECT DISTINCT ON ("ItemID") * FROM proposed ORDER BY "ItemID",position
), eligible AS MATERIALIZED (
 SELECT p.*,ARRAY(SELECT t.id FROM topics t JOIN topic_candidate_state cs ON cs.workspace_id=t.workspace_id AND cs.topic_id=t.id
  WHERE t.workspace_id=$1 AND t.enabled AND cs.prepared_input_version=cs.input_version AND cs.ready_version>0
  AND cs.inputs=p."TopicInputs"->t.id::text
  AND (cs.inputs->>'TopicRevision')::integer=t.revision
  AND (cs.inputs->>'CorrectionEpoch')::bigint=t.correction_epoch
  AND (cs.inputs->>'CorrectionVersion')::bigint=t.correction_version
  AND (cs.inputs->>'ModelID')::uuid=p."ModelID"
 AND (cs.inputs->>'CandidateLimit')::integer=COALESCE((SELECT (document->'retrieval'->>'candidateLimit')::integer FROM workspace_settings WHERE workspace_id=$1),50)
 AND (cs.inputs->>'RRFK')::integer=COALESCE((SELECT (document->'retrieval'->>'rrfK')::integer FROM workspace_settings WHERE workspace_id=$1),60)
  AND (NOT EXISTS(SELECT 1 FROM topic_sources WHERE topic_id=t.id)
   OR EXISTS(SELECT 1 FROM source_entries se JOIN topic_sources ts ON ts.source_id=se.source_id AND ts.topic_id=t.id WHERE se.item_id=i.id))
  AND (NOT EXISTS(SELECT 1 FROM topic_item_kinds WHERE topic_id=t.id)
   OR EXISTS(SELECT 1 FROM topic_item_kinds k WHERE k.topic_id=t.id AND k.item_kind=i.kind))
  AND t.id=ANY(COALESCE(p."MatchedTopicIDs",'{}'))
  AND NOT EXISTS(SELECT 1 FROM topic_corrections c WHERE c.topic_id=t.id AND c.item_id=i.id AND c.membership='exclude')
  ORDER BY t.id) AS matches
 FROM unique_proposed p JOIN items i ON i.workspace_id=$1 AND i.id=p."ItemID"
 LEFT JOIN item_state state ON state.workspace_id=i.workspace_id AND state.item_id=i.id
 LEFT JOIN item_feedback feedback ON feedback.workspace_id=i.workspace_id AND feedback.item_id=i.id
 WHERE p."ModelID"=COALESCE((SELECT serving_model_id FROM workspace_retrieval_state WHERE workspace_id=$1),'00000000-0000-0000-0000-000000000000'::uuid)
 AND COALESCE(state.visibility,'visible')<>'hidden' AND COALESCE(feedback.sentiment,'')<>'dislike'
 AND COALESCE(i.published_at,i.discovered_at)>=$2::timestamptz-interval '30 days'
 AND COALESCE(i.published_at,i.discovered_at)<=$2
 AND NOT EXISTS(SELECT 1 FROM feed_publications old WHERE old.workspace_id=$1 AND old.item_id=i.id)
 AND (cardinality(COALESCE(p."MatchedTopicIDs",'{}'))=0 OR NOT EXISTS(SELECT 1 FROM retrieval_projection_pending pending
 JOIN workspace_retrieval_state r ON r.workspace_id=pending.workspace_id AND r.serving_model_id=pending.embedding_model_id
 WHERE pending.workspace_id=$1 AND pending.item_id=i.id))
 FOR UPDATE OF i
), admitted AS (
 SELECT *,count(*) OVER() AS total,row_number() OVER(ORDER BY position) AS batch_position
 FROM eligible
 WHERE ($6::uuid IS NULL OR $6=ANY(matches))
 AND (cardinality(matches)>0 OR (cardinality(COALESCE("MatchedTopicIDs",'{}'))=0 AND NOT EXISTS(SELECT 1 FROM topics WHERE workspace_id=$1 AND enabled)))
), inserted AS (
 INSERT INTO feed_publications(workspace_id,item_id,batch_id,published_at,ordinal,matched_topic_ids,topic_inputs,score,signals,pipeline,model_id)
 SELECT $1,"ItemID",$5,$2,$4+total-batch_position+1,matches,COALESCE((SELECT jsonb_object_agg(fact.key,fact.value) FROM jsonb_each(COALESCE("TopicInputs",'{}')) fact WHERE fact.key::uuid=ANY(matches)),'{}'),"Score",COALESCE("Signals",'{}'),"Pipeline","ModelID"
 FROM admitted
 RETURNING workspace_id,item_id,batch_id,published_at,ordinal,matched_topic_ids,topic_inputs,score,signals,pipeline,model_id
)
SELECT * FROM inserted ORDER BY ordinal DESC`

func scanFeedPublication(row scanner) (domain.FeedPublication, error) {
	var p domain.FeedPublication
	err := row.Scan(&p.WorkspaceID, &p.ItemID, &p.BatchID, &p.PublishedAt, &p.Ordinal, &p.MatchedTopicIDs, &p.TopicInputs, &p.Score, &p.Signals, &p.Pipeline, &p.ModelID)
	return p, err
}

func completeFeedPublication(ctx context.Context, tx pgx.Tx, command domain.FeedPublishCommand, result domain.FeedPublishResult) error {
	if err := lockRetrievalState(ctx, tx, command.WorkspaceID); err != nil {
		return err
	}
	ids := make([]uuid.UUID, 0, len(result.Publications))
	revisions := make([]uuid.UUID, 0, len(result.Publications))
	for _, p := range result.Publications {
		revision, err := uuid.NewV7()
		if err != nil {
			return err
		}
		ids = append(ids, p.ItemID)
		revisions = append(revisions, revision)
	}
	if _, err := tx.Exec(ctx, `UPDATE feed_state SET head_ordinal=$2 WHERE workspace_id=$1`, command.WorkspaceID, result.HeadOrdinal); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM topic_candidates WHERE workspace_id=$1 AND item_id=ANY($2)`, command.WorkspaceID, ids); err != nil {
		return err
	}
	if err := refreshFeedDisplay(ctx, tx, command.WorkspaceID, ids); err != nil {
		return err
	}
	if err := requestFeedPreparation(ctx, tx, domain.FeedPreparationRequest{WorkspaceID: command.WorkspaceID, At: command.PublishedAt}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO item_projection_state(workspace_id,item_id,revision,updated_at)
 SELECT $1,item_id,revision,$4 FROM unnest($2::uuid[],$3::uuid[]) p(item_id,revision)
 ON CONFLICT(workspace_id,item_id) DO UPDATE SET revision=EXCLUDED.revision,version=item_projection_state.version+1,updated_at=EXCLUDED.updated_at`, command.WorkspaceID, ids, revisions, command.PublishedAt); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO retrieval_projection_pending(workspace_id,item_id,embedding_model_id,revision,updated_at,model_generation)
 SELECT $1,p.item_id,d.model_id,p.revision,$4,d.generation FROM unnest($2::uuid[],$3::uuid[]) p(item_id,revision)
 CROSS JOIN (`+projectionDestinationsSQL+`) d
 WHERE d.model_id IS NOT NULL
 ON CONFLICT(workspace_id,item_id,embedding_model_id) DO UPDATE SET revision=EXCLUDED.revision,updated_at=EXCLUDED.updated_at,model_generation=EXCLUDED.model_generation`, command.WorkspaceID, ids, revisions, command.PublishedAt)
	return err
}

// beginFeedChange serializes canonical eligibility changes with candidate and publication commits.
func (s *Store) beginFeedChange(ctx context.Context, workspaceID uuid.UUID) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := lockFeedHead(ctx, tx, workspaceID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

// HasEnabledFeedTopics reports whether the Workspace has any enabled Topics.
func (s *Store) HasEnabledFeedTopics(ctx context.Context, workspaceID uuid.UUID) (bool, error) {
	var enabled bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM topics WHERE workspace_id=$1 AND enabled)`, workspaceID).Scan(&enabled)
	return enabled, err
}
