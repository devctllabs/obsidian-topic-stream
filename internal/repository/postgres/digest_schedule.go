package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AdmitScheduledDigestBuild coalesces a current due occurrence and advances its calendar atomically.
func (s *Store) AdmitScheduledDigestBuild(ctx context.Context, request domain.DigestScheduledBuild) (domain.DigestBuildAdmission, error) {
	advance := request.Advance
	if request.Job.WorkspaceID != advance.WorkspaceID {
		return domain.DigestBuildAdmission{}, domain.ErrConflict
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.DigestBuildAdmission{}, fmt.Errorf("begin scheduled admission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM digest_definitions WHERE workspace_id=$1 AND id=$2 AND next_run_at=$3
 AND next_run_at<=$4 AND version=$5 AND enabled AND deleted_at IS NULL FOR UPDATE`, advance.WorkspaceID, advance.DigestID, advance.Expected, advance.UpdatedAt, request.ExpectedVersion).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DigestBuildAdmission{}, nil
	}
	if err != nil {
		return domain.DigestBuildAdmission{}, fmt.Errorf("lock due digest: %w", err)
	}
	job, created, err := admitActiveDigestJob(ctx, tx, request.Job)
	if err != nil {
		return domain.DigestBuildAdmission{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE digest_definitions SET last_scheduled_for=$3,next_run_at=$4,updated_at=$5 WHERE workspace_id=$1 AND id=$2`, advance.WorkspaceID, advance.DigestID, advance.Expected, advance.NextRunAt, advance.UpdatedAt)
	if err != nil {
		return domain.DigestBuildAdmission{}, fmt.Errorf("advance admitted digest schedule: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.DigestBuildAdmission{}, fmt.Errorf("commit scheduled admission: %w", err)
	}
	return domain.DigestBuildAdmission{Job: job, Created: created}, nil
}
