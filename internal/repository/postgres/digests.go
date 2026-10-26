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

const digestColumns = `id,workspace_id,name,topics,timezone,recurrence,enabled,version,
next_run_at,last_successful_at,created_at,updated_at`

// CreateDigest stores a Digest Definition and its initial version.
func (s *Store) CreateDigest(ctx context.Context, definition domain.DigestDefinition) error {
	topics, recurrence, err := encodeDigestConfiguration(definition)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO digest_definitions (
    id,workspace_id,name,topics,timezone,recurrence,enabled,version,next_run_at,last_successful_at,created_at,updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		definition.ID, definition.WorkspaceID, definition.Name, topics, definition.Timezone, recurrence,
		definition.Enabled, definition.Version, definition.NextRunAt, definition.LastSuccessfulAt,
		definition.CreatedAt, definition.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert digest definition: %w", err)
	}
	return nil
}

// ListDigests returns the Workspace's Digest Definitions.
func (s *Store) ListDigests(ctx context.Context, workspaceID uuid.UUID) ([]domain.DigestDefinition, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+digestColumns+` FROM digest_definitions
WHERE workspace_id=$1 AND deleted_at IS NULL ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("query digest definitions: %w", err)
	}
	defer rows.Close()
	result := make([]domain.DigestDefinition, 0)
	for rows.Next() {
		definition, scanErr := scanDigest(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan digest definition: %w", scanErr)
		}
		result = append(result, definition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate digest definitions: %w", err)
	}
	return result, nil
}

// GetDigest returns a Digest Definition and whether it exists in the Workspace.
func (s *Store) GetDigest(
	ctx context.Context,
	workspaceID, digestID uuid.UUID,
) (domain.DigestDefinition, bool, error) {
	definition, err := scanDigest(s.pool.QueryRow(ctx, `SELECT `+digestColumns+` FROM digest_definitions
WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL`, workspaceID, digestID))
	if err != nil {
		ok, wrapped := found(err)
		return domain.DigestDefinition{}, ok, wrapped
	}
	return definition, true, nil
}

// UpdateDigest updates a Digest Definition only when its expected version matches.
func (s *Store) UpdateDigest(
	ctx context.Context,
	definition domain.DigestDefinition,
	expectedVersion int,
) (bool, error) {
	topics, recurrence, err := encodeDigestConfiguration(definition)
	if err != nil {
		return false, err
	}
	command, err := s.pool.Exec(ctx, `
UPDATE digest_definitions SET name=$4,topics=$5,timezone=$6,recurrence=$7,enabled=$8,
version=$9,next_run_at=$10,updated_at=$11
WHERE workspace_id=$1 AND id=$2 AND version=$3 AND deleted_at IS NULL`,
		definition.WorkspaceID, definition.ID, expectedVersion, definition.Name, topics, definition.Timezone,
		recurrence, definition.Enabled, definition.Version, definition.NextRunAt, definition.UpdatedAt)
	if err != nil {
		return false, fmt.Errorf("update digest definition: %w", err)
	}
	return command.RowsAffected() == 1, nil
}

// DeleteDigest atomically soft-deletes a Definition and cancels its nonterminal Builds.
func (s *Store) DeleteDigest(ctx context.Context, request domain.DigestDelete) (domain.DigestDeletion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.DigestDeletion{}, fmt.Errorf("begin digest deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `UPDATE digest_definitions
 SET deleted_at=$4,enabled=false,next_run_at=NULL,updated_at=$4,version=version+1
 WHERE workspace_id=$1 AND id=$2 AND version=$3 AND deleted_at IS NULL`, request.WorkspaceID, request.DigestID, request.ExpectedVersion, request.DeletedAt)
	if err != nil {
		return domain.DigestDeletion{}, fmt.Errorf("delete digest definition: %w", err)
	}
	if command.RowsAffected() == 0 {
		return domain.DigestDeletion{}, nil
	}
	rows, err := tx.Query(ctx, `UPDATE jobs SET
 cancellation_requested=CASE WHEN status='running' THEN true ELSE cancellation_requested END,
 status=CASE WHEN status IN ('queued','retry_wait') THEN 'cancelled' ELSE status END,
 reason=CASE WHEN status IN ('queued','retry_wait') THEN 'Digest Definition deleted' ELSE reason END,
 finished_at=CASE WHEN status IN ('queued','retry_wait') THEN $3 ELSE finished_at END,updated_at=$3
 WHERE workspace_id=$1 AND idempotency_key=$2 AND kind='generate-digest'
 AND status IN ('queued','running','retry_wait') AND (status<>'running' OR NOT cancellation_requested)
 RETURNING id`, request.WorkspaceID, "generate-digest:"+request.DigestID.String(), request.DeletedAt)
	if err != nil {
		return domain.DigestDeletion{}, fmt.Errorf("cancel deleted digest builds: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return domain.DigestDeletion{}, fmt.Errorf("read cancelled digest jobs: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.DigestDeletion{}, fmt.Errorf("commit digest deletion: %w", err)
	}
	return domain.DigestDeletion{Deleted: true, JobIDs: ids}, nil
}

func encodeDigestConfiguration(definition domain.DigestDefinition) ([]byte, []byte, error) {
	topics, err := json.Marshal(definition.Topics)
	if err != nil {
		return nil, nil, fmt.Errorf("encode digest topics: %w", err)
	}
	recurrence, err := json.Marshal(definition.Recurrence)
	if err != nil {
		return nil, nil, fmt.Errorf("encode digest recurrence: %w", err)
	}
	return topics, recurrence, nil
}

// digestScanner decodes one SQL row without depending on a concrete pgx row type.
type digestScanner interface {
	// Scan decodes the current SQL row into the supplied destination pointers.
	Scan(destinations ...any) error
}

func scanDigest(row digestScanner) (domain.DigestDefinition, error) {
	var definition domain.DigestDefinition
	var topics, recurrence []byte
	err := row.Scan(
		&definition.ID, &definition.WorkspaceID, &definition.Name, &topics, &definition.Timezone, &recurrence,
		&definition.Enabled, &definition.Version, &definition.NextRunAt, &definition.LastSuccessfulAt,
		&definition.CreatedAt, &definition.UpdatedAt,
	)
	if err != nil {
		return domain.DigestDefinition{}, err
	}
	if err := json.Unmarshal(topics, &definition.Topics); err != nil {
		return domain.DigestDefinition{}, fmt.Errorf("decode digest topics: %w", err)
	}
	if err := json.Unmarshal(recurrence, &definition.Recurrence); err != nil {
		return domain.DigestDefinition{}, fmt.Errorf("decode digest recurrence: %w", err)
	}
	return definition, nil
}

const digestIssueColumns = `id,workspace_id,digest_id,generation_job_id,title,status,scheduled_for,sections,generated_at`

// ListDigestIssues reads a bounded summary page without fetching the JSON snapshots.
func (s *Store) ListDigestIssues(ctx context.Context, query domain.DigestIssueList) ([]domain.DigestIssueSummary, error) {
	if query.Limit < 1 || query.Limit > 101 {
		return nil, fmt.Errorf("digest issue limit must be between 1 and 101")
	}
	statement := `SELECT id,workspace_id,digest_id,generation_job_id,title,status,scheduled_for,generated_at FROM digest_issues
WHERE workspace_id=$1`
	args := []any{query.WorkspaceID}
	if query.DigestID != nil {
		args = append(args, *query.DigestID)
		statement += fmt.Sprintf(" AND digest_id=$%d", len(args))
	}
	if query.Cursor != nil {
		args = append(args, query.Cursor.GeneratedAt, query.Cursor.ID)
		statement += fmt.Sprintf(" AND (generated_at,id)<($%d,$%d)", len(args)-1, len(args))
	}
	args = append(args, query.Limit)
	statement += fmt.Sprintf(" ORDER BY generated_at DESC,id DESC LIMIT $%d", len(args))
	rows, err := s.pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query digest issue summaries: %w", err)
	}
	defer rows.Close()
	result := make([]domain.DigestIssueSummary, 0, query.Limit)
	for rows.Next() {
		var issue domain.DigestIssueSummary
		if err := rows.Scan(&issue.ID, &issue.WorkspaceID, &issue.DigestID, &issue.GenerationJobID, &issue.Title,
			&issue.Status, &issue.ScheduledFor, &issue.GeneratedAt); err != nil {
			return nil, fmt.Errorf("scan digest issue summary: %w", err)
		}
		result = append(result, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate digest issue summaries: %w", err)
	}
	return result, nil
}

// GetDigestIssue returns a saved Digest Issue and whether it exists in the Workspace.
func (s *Store) GetDigestIssue(
	ctx context.Context,
	workspaceID, issueID uuid.UUID,
) (domain.DigestIssue, bool, error) {
	issue, err := scanDigestIssue(s.pool.QueryRow(ctx, `SELECT `+digestIssueColumns+` FROM digest_issues
WHERE workspace_id=$1 AND id=$2`, workspaceID, issueID))
	if err != nil {
		ok, wrapped := found(err)
		return domain.DigestIssue{}, ok, wrapped
	}
	return issue, true, nil
}

func scanDigestIssue(row digestScanner) (domain.DigestIssue, error) {
	var issue domain.DigestIssue
	var sections []byte
	err := row.Scan(
		&issue.ID, &issue.WorkspaceID, &issue.DigestID, &issue.GenerationJobID, &issue.Title, &issue.Status,
		&issue.ScheduledFor, &sections, &issue.GeneratedAt,
	)
	if err != nil {
		return domain.DigestIssue{}, err
	}
	if err := json.Unmarshal(sections, &issue.Sections); err != nil {
		return domain.DigestIssue{}, fmt.Errorf("decode digest issue sections: %w", err)
	}
	return issue, nil
}

// FindConsumedDigestItemIDs intersects candidates with historical consumption without loading snapshots.
func (s *Store) FindConsumedDigestItemIDs(ctx context.Context, workspaceID, digestID uuid.UUID, itemIDs []uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `SELECT consumed.item_id FROM digest_consumed_items consumed
JOIN digest_definitions definition ON definition.id=consumed.digest_id
WHERE definition.workspace_id=$1 AND consumed.digest_id=$2 AND consumed.item_id=ANY($3::uuid[])`, workspaceID, digestID, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("query consumed digest items: %w", err)
	}
	defer rows.Close()
	result := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan consumed digest items: %w", err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate consumed digest items: %w", err)
	}
	return result, nil
}

// ListDueDigests returns enabled Digest Definitions whose next run is due at the supplied time.
func (s *Store) ListDueDigests(
	ctx context.Context,
	workspaceID uuid.UUID,
	now time.Time,
) ([]domain.DigestDefinition, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+digestColumns+` FROM digest_definitions
WHERE workspace_id=$1 AND enabled AND deleted_at IS NULL AND next_run_at <= $2
ORDER BY next_run_at,id`, workspaceID, now)
	if err != nil {
		return nil, fmt.Errorf("query due digest definitions: %w", err)
	}
	defer rows.Close()
	result := make([]domain.DigestDefinition, 0)
	for rows.Next() {
		definition, scanErr := scanDigest(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan due digest definition: %w", scanErr)
		}
		result = append(result, definition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due digest definitions: %w", err)
	}
	return result, nil
}
