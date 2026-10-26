package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/jackc/pgx/v5"
)

func requestFeedPreparation(ctx context.Context, tx pgx.Tx, request domain.FeedPreparationRequest) error {
	var at *time.Time
	if !request.At.IsZero() {
		at = &request.At
	}
	_, err := tx.Exec(ctx, `INSERT INTO topic_candidate_state(workspace_id,topic_id,first_dirty_at)
 SELECT workspace_id,id,COALESCE($3::timestamptz,clock_timestamp()) FROM topics WHERE workspace_id=$1 AND enabled AND ($2::uuid IS NULL OR id=$2)
 ON CONFLICT(workspace_id,topic_id) DO UPDATE SET
 requested_version=topic_candidate_state.requested_version+1,
 input_version=topic_candidate_state.input_version+CASE WHEN $4 THEN 1 ELSE 0 END,
 first_dirty_at=COALESCE(topic_candidate_state.first_dirty_at,EXCLUDED.first_dirty_at)`, request.WorkspaceID, request.TopicID, at, request.Invalidate)
	return err
}

// ReplaceFeedCandidates replaces candidates while lease and inputs remain current.
// Stale work or an invalid candidate bound returns domain.ErrConflict.
func (s *Store) ReplaceFeedCandidates(ctx context.Context, replacement domain.FeedCandidateReplacement) error {
	if replacement.Inputs.CandidateLimit < 1 || replacement.Inputs.CandidateLimit > 100 || len(replacement.Candidates) > replacement.Inputs.CandidateLimit {
		return domain.ErrConflict
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockPreparationClaim(ctx, tx, replacement); err != nil {
		return err
	}
	if _, err := lockFeedHead(ctx, tx, replacement.Work.WorkspaceID); err != nil {
		return err
	}
	if err := validateCandidateReplacement(ctx, tx, replacement); err != nil {
		return err
	}
	if err := writeCandidateReplacement(ctx, tx, replacement); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func lockPreparationClaim(ctx context.Context, tx pgx.Tx, replacement domain.FeedCandidateReplacement) error {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT true FROM jobs WHERE workspace_id=$1 AND id=$2 AND status='running'
 AND kind='prepare-feed' AND queue='feed-preparation' AND lease_owner=$3 AND lease_expires_at>clock_timestamp() AND NOT cancellation_requested FOR UPDATE`,
		replacement.Work.WorkspaceID, replacement.JobID, replacement.LeaseOwner).Scan(&exists)
	if err == pgx.ErrNoRows {
		return domain.ErrConflict
	}
	return err
}

func validateCandidateReplacement(ctx context.Context, tx pgx.Tx, replacement domain.FeedCandidateReplacement) error {
	var requested, ready, inputVersion int64
	var current domain.FeedCandidateInputs
	var enabled bool
	err := tx.QueryRow(ctx, `SELECT cs.requested_version,cs.ready_version,cs.input_version,t.revision,t.correction_epoch,t.correction_version,t.enabled,
 COALESCE((SELECT serving_model_id FROM workspace_retrieval_state WHERE workspace_id=cs.workspace_id),'00000000-0000-0000-0000-000000000000'::uuid),
 COALESCE((SELECT (document->'retrieval'->>'candidateLimit')::integer FROM workspace_settings WHERE workspace_id=cs.workspace_id),50),
 COALESCE((SELECT (document->'retrieval'->>'rrfK')::integer FROM workspace_settings WHERE workspace_id=cs.workspace_id),60)
 FROM topic_candidate_state cs JOIN topics t ON t.workspace_id=cs.workspace_id AND t.id=cs.topic_id
 WHERE cs.workspace_id=$1 AND cs.topic_id=$2 FOR UPDATE OF cs`, replacement.Work.WorkspaceID, replacement.Work.TopicID).
		Scan(&requested, &ready, &inputVersion, &current.TopicRevision, &current.CorrectionEpoch, &current.CorrectionVersion, &enabled, &current.ModelID, &current.CandidateLimit, &current.RRFK)
	if err == pgx.ErrNoRows {
		return domain.ErrConflict
	}
	if err != nil {
		return err
	}
	if !enabled || !currentPreparationWork(replacement.Work, requested, ready, inputVersion) {
		return domain.ErrConflict
	}
	captured := replacement.Inputs
	if !compatibleCandidateInputs(current, captured) {
		return domain.ErrConflict
	}
	return nil
}

func writeCandidateReplacement(ctx context.Context, tx pgx.Tx, replacement domain.FeedCandidateReplacement) error {
	work := replacement.Work
	if _, err := tx.Exec(ctx, `DELETE FROM topic_candidates WHERE workspace_id=$1 AND topic_id=$2`, work.WorkspaceID, work.TopicID); err != nil {
		return err
	}
	payload, err := json.Marshal(replacement.Candidates)
	if err != nil {
		return err
	}
	inputs, err := json.Marshal(replacement.Inputs)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, insertFeedCandidatesSQL, work.WorkspaceID, work.TopicID, payload, replacement.At); err != nil {
		return fmt.Errorf("insert prepared feed candidates: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE topic_candidate_state SET ready_version=$3,prepared_input_version=$4,prepared_at=$5,inputs=$6,
 first_dirty_at=CASE WHEN requested_version=$3 THEN NULL ELSE first_dirty_at END,
 include_embedding=$7,exclude_embedding=$8,next_eligible_at=LEAST($9::timestamptz,
 (SELECT min(COALESCE(i.published_at,i.discovered_at)+interval '30 days 1 microsecond') FROM topic_candidates c
 JOIN items i ON i.workspace_id=c.workspace_id AND i.id=c.item_id WHERE c.workspace_id=$1 AND c.topic_id=$2))
 WHERE workspace_id=$1 AND topic_id=$2`, work.WorkspaceID, work.TopicID, work.RequestedVersion, work.InputVersion, replacement.At, inputs,
		work.IncludeEmbedding, work.ExcludeEmbedding, replacement.NextEligibleAt)
	return err
}

const insertFeedCandidatesSQL = `
INSERT INTO topic_candidates(workspace_id,topic_id,item_id,position,score,signals)
SELECT $1,$2,c."ItemID",entry.position,c."Score",COALESCE(c."Signals",'{}')
FROM jsonb_array_elements($3::jsonb) WITH ORDINALITY entry(value,position)
CROSS JOIN LATERAL jsonb_to_record(entry.value) c("ItemID" uuid,"Score" double precision,"Signals" jsonb)
JOIN items i ON i.workspace_id=$1 AND i.id=c."ItemID"
JOIN topics t ON t.workspace_id=$1 AND t.id=$2
LEFT JOIN item_state state ON state.workspace_id=$1 AND state.item_id=i.id
LEFT JOIN item_feedback feedback ON feedback.workspace_id=$1 AND feedback.item_id=i.id
WHERE COALESCE(state.visibility,'visible')<>'hidden' AND COALESCE(feedback.sentiment,'')<>'dislike'
AND COALESCE(i.published_at,i.discovered_at)>=$4::timestamptz-interval '30 days'
AND COALESCE(i.published_at,i.discovered_at)<=$4
AND NOT EXISTS(SELECT 1 FROM feed_publications p WHERE p.workspace_id=$1 AND p.item_id=i.id)
 AND NOT EXISTS(SELECT 1 FROM retrieval_projection_pending pending JOIN workspace_retrieval_state r ON r.workspace_id=pending.workspace_id AND r.serving_model_id=pending.embedding_model_id WHERE pending.workspace_id=$1 AND pending.item_id=i.id)
AND NOT EXISTS(SELECT 1 FROM topic_corrections wrong WHERE wrong.item_id=i.id AND wrong.topic_id=$2 AND wrong.membership='exclude')
AND (NOT EXISTS(SELECT 1 FROM topic_sources WHERE topic_id=$2)
 OR EXISTS(SELECT 1 FROM source_entries se JOIN topic_sources ts ON ts.source_id=se.source_id AND ts.topic_id=$2 WHERE se.item_id=i.id))
AND (NOT EXISTS(SELECT 1 FROM topic_item_kinds WHERE topic_id=$2)
 OR EXISTS(SELECT 1 FROM topic_item_kinds k WHERE k.topic_id=$2 AND k.item_kind=i.kind))`

// ReadFeedCandidates returns prepared candidates whose inputs remain eligible for Feed admission.
func (s *Store) ReadFeedCandidates(ctx context.Context, query domain.FeedCandidateQuery) ([]domain.FeedCandidate, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.topic_id,c.item_id,c.position,c.score,c.signals,cs.inputs,
 COALESCE((SELECT se.source_id FROM source_entries se WHERE se.item_id=i.id ORDER BY se.source_id LIMIT 1),'00000000-0000-0000-0000-000000000000'::uuid),
 COALESCE(i.published_at,i.discovered_at),
 CASE WHEN feedback.item_id IS NULL THEN NULL ELSE jsonb_build_object('Sentiment',feedback.sentiment,'Comment',feedback.comment,'CreatedAt',feedback.created_at,'UpdatedAt',feedback.updated_at) END
 FROM topic_candidates c JOIN topic_candidate_state cs USING(workspace_id,topic_id)
 JOIN topics t ON t.workspace_id=c.workspace_id AND t.id=c.topic_id
 JOIN items i ON i.workspace_id=c.workspace_id AND i.id=c.item_id
 LEFT JOIN item_state state ON state.workspace_id=c.workspace_id AND state.item_id=c.item_id
 LEFT JOIN item_feedback feedback ON feedback.workspace_id=c.workspace_id AND feedback.item_id=c.item_id
 LEFT JOIN workspace_retrieval_state r ON r.workspace_id=c.workspace_id
 LEFT JOIN workspace_settings ws ON ws.workspace_id=c.workspace_id
 WHERE c.workspace_id=$1 AND t.enabled AND cs.ready_version>0 AND cs.prepared_input_version=cs.input_version
 AND (cs.inputs->>'TopicRevision')::integer=t.revision AND (cs.inputs->>'CorrectionEpoch')::bigint=t.correction_epoch
 AND (cs.inputs->>'CorrectionVersion')::bigint=t.correction_version AND (cs.inputs->>'CandidateLimit')::integer=$3
 AND (cs.inputs->>'CandidateLimit')::integer=COALESCE((ws.document->'retrieval'->>'candidateLimit')::integer,50)
 AND (cs.inputs->>'RRFK')::integer=COALESCE((ws.document->'retrieval'->>'rrfK')::integer,60)
 AND ($4='' OR cs.inputs->>'GateVersion'=$4)
 AND NOT EXISTS(SELECT 1 FROM retrieval_projection_pending pending WHERE pending.workspace_id=c.workspace_id AND pending.item_id=c.item_id AND pending.embedding_model_id=r.serving_model_id)
 AND (cs.inputs->>'ModelID')::uuid=COALESCE(r.serving_model_id,'00000000-0000-0000-0000-000000000000'::uuid)
 AND COALESCE(state.visibility,'visible')<>'hidden' AND COALESCE(feedback.sentiment,'')<>'dislike'
 AND COALESCE(i.published_at,i.discovered_at)>=$2::timestamptz-interval '30 days' AND COALESCE(i.published_at,i.discovered_at)<=$2
 AND NOT EXISTS(SELECT 1 FROM feed_publications p WHERE p.workspace_id=c.workspace_id AND p.item_id=c.item_id)
 ORDER BY c.topic_id,c.position`, query.WorkspaceID, query.At, query.CandidateLimit, query.GateVersion)
	if err != nil {
		return nil, fmt.Errorf("read prepared feed candidates: %w", err)
	}
	defer rows.Close()
	result := make([]domain.FeedCandidate, 0)
	for rows.Next() {
		c := domain.FeedCandidate{WorkspaceID: query.WorkspaceID}
		var inputs []byte
		if err := rows.Scan(&c.TopicID, &c.ItemID, &c.Position, &c.Score, &c.Signals, &inputs, &c.SourceID, &c.ContentAt, &c.Feedback); err != nil {
			return nil, err
		}
		if len(result) > 0 && result[len(result)-1].TopicID == c.TopicID {
			c.Inputs = result[len(result)-1].Inputs
		} else if err := json.Unmarshal(inputs, &c.Inputs); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func currentPreparationWork(work domain.FeedPreparationWork, requested, ready, inputVersion int64) bool {
	return work.InputVersion == inputVersion && work.RequestedVersion <= requested && work.RequestedVersion >= ready
}

func compatibleCandidateInputs(current, captured domain.FeedCandidateInputs) bool {
	return current.TopicRevision == captured.TopicRevision && current.CorrectionEpoch == captured.CorrectionEpoch && current.CorrectionVersion == captured.CorrectionVersion && current.ModelID == captured.ModelID && current.CandidateLimit == captured.CandidateLimit && current.RRFK == captured.RRFK
}
