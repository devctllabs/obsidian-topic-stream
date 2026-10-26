package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PublishDigestIssue commits one immutable result only for a live generation execution.
func (s *Store) PublishDigestIssue(ctx context.Context, request domain.DigestPublication) (domain.DigestPublicationResult, error) {
	if request.Issue.GenerationJobID == uuid.Nil || request.LeaseOwner == "" || request.Attempt < 1 {
		return domain.DigestPublicationResult{}, domain.ErrDigestPublicationRejected
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.DigestPublicationResult{}, fmt.Errorf("begin digest publication: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := publishDigestIssue(ctx, tx, request)
	if err != nil {
		return domain.DigestPublicationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.DigestPublicationResult{}, fmt.Errorf("commit digest publication: %w", err)
	}
	return result, nil
}

// GetDigestIssueForJob returns a committed result independently of Definition lifecycle state.
func (s *Store) GetDigestIssueForJob(ctx context.Context, workspaceID, jobID uuid.UUID) (domain.DigestIssue, bool, error) {
	return digestIssueForJob(ctx, s.pool, workspaceID, jobID)
}

func digestIssueForJob(ctx context.Context, query jobQuerier, workspaceID, jobID uuid.UUID) (domain.DigestIssue, bool, error) {
	issue, err := scanDigestIssue(query.QueryRow(ctx, `SELECT `+digestIssueColumns+` FROM digest_issues WHERE workspace_id=$1 AND generation_job_id=$2`, workspaceID, jobID))
	if err != nil {
		ok, wrapped := found(err)
		return domain.DigestIssue{}, ok, wrapped
	}
	return issue, true, nil
}

func publishDigestIssue(ctx context.Context, tx pgx.Tx, request domain.DigestPublication) (domain.DigestPublicationResult, error) {
	issue := request.Issue
	deleted, err := lockPublicationDigest(ctx, tx, issue)
	if err != nil {
		return domain.DigestPublicationResult{}, err
	}
	existing, found, err := digestIssueForJob(ctx, tx, issue.WorkspaceID, issue.GenerationJobID)
	if err != nil {
		return domain.DigestPublicationResult{}, fmt.Errorf("resolve committed digest result: %w", err)
	}
	if found {
		if existing.DigestID != issue.DigestID {
			return domain.DigestPublicationResult{}, domain.ErrDigestPublicationRejected
		}
		return domain.DigestPublicationResult{Issue: existing}, nil
	}
	if deleted {
		return domain.DigestPublicationResult{}, domain.ErrDigestPublicationRejected
	}
	if err := validateDigestExecution(ctx, tx, request); err != nil {
		return domain.DigestPublicationResult{}, err
	}
	if err := storeDigestIssue(ctx, tx, issue); err != nil {
		return domain.DigestPublicationResult{}, err
	}
	return domain.DigestPublicationResult{Issue: issue, Created: true}, nil
}

func lockPublicationDigest(ctx context.Context, tx pgx.Tx, issue domain.DigestIssue) (bool, error) {
	var deletedAt *time.Time
	err := tx.QueryRow(ctx, `SELECT deleted_at FROM digest_definitions WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, issue.WorkspaceID, issue.DigestID).Scan(&deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, domain.ErrDigestPublicationRejected
	}
	if err != nil {
		return false, fmt.Errorf("lock publication digest: %w", err)
	}
	return deletedAt != nil, nil
}

func validateDigestExecution(ctx context.Context, tx pgx.Tx, request domain.DigestPublication) error {
	job, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, request.Issue.WorkspaceID, request.Issue.GenerationJobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrDigestPublicationRejected
	}
	if err != nil {
		return fmt.Errorf("lock digest generation execution: %w", err)
	}
	// Read the database clock after obtaining the Job lock, including time spent waiting for it.
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return fmt.Errorf("read publication fence time: %w", err)
	}
	if !liveJobExecution(job, request.LeaseOwner, request.Attempt, now) || !matchingDigestJob(job, request.Issue) {
		return domain.ErrDigestPublicationRejected
	}
	return nil
}

func matchingDigestJob(job domain.Job, issue domain.DigestIssue) bool {
	var arguments struct {
		DigestID     uuid.UUID  `json:"digestId"`
		ScheduledFor *time.Time `json:"scheduledFor"`
	}
	if err := json.Unmarshal(job.Arguments, &arguments); err != nil {
		return false
	}
	if job.Kind != "generate-digest" || job.Queue != domain.JobQueueGeneration || job.ArgumentsVersion != 1 {
		return false
	}
	return arguments.DigestID == issue.DigestID && equalDigestTime(arguments.ScheduledFor, issue.ScheduledFor) &&
		job.IdempotencyKey != nil && *job.IdempotencyKey == "generate-digest:"+issue.DigestID.String()
}

func equalDigestTime(first, second *time.Time) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return first.Equal(*second)
}

func storeDigestIssue(ctx context.Context, tx pgx.Tx, issue domain.DigestIssue) error {
	sections, err := json.Marshal(issue.Sections)
	if err != nil {
		return fmt.Errorf("encode digest issue sections: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO digest_issues(id,workspace_id,digest_id,generation_job_id,title,status,scheduled_for,sections,generated_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, issue.ID, issue.WorkspaceID, issue.DigestID, issue.GenerationJobID, issue.Title, issue.Status, issue.ScheduledFor, sections, issue.GeneratedAt)
	if err != nil {
		return fmt.Errorf("insert digest issue: %w", err)
	}
	if err := recordDigestSummaryInputs(ctx, tx, issue); err != nil {
		return err
	}
	var itemIDs []uuid.UUID
	for _, section := range issue.Sections {
		for _, item := range section.Items {
			itemIDs = append(itemIDs, item.Item.ID)
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO digest_consumed_items(digest_id,item_id) SELECT $1,unnest($2::uuid[]) ON CONFLICT DO NOTHING`, issue.DigestID, itemIDs)
	if err != nil {
		return fmt.Errorf("record consumed digest items: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE digest_definitions SET last_successful_at=$3 WHERE workspace_id=$1 AND id=$2`, issue.WorkspaceID, issue.DigestID, issue.GeneratedAt)
	if err != nil {
		return fmt.Errorf("update digest success time: %w", err)
	}
	return nil
}
