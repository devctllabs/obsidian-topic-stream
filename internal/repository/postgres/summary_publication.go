package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PublishSummary commits one annotation only for its actual live attempt and exact input version.
func (r *SummaryRepository) PublishSummary(ctx context.Context, publication domain.SummaryPublication) error {
	if publication.Annotation.GenerationJobID == uuid.Nil || publication.Annotation.SourceVersion != publication.Input.SourceVersion || publication.LeaseOwner == "" || publication.Attempt < 1 {
		return domain.ErrSummaryPublicationRejected
	}
	tx, err := r.store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin summary publication: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := publishSummary(ctx, tx, publication); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit summary publication: %w", err)
	}
	return nil
}

func publishSummary(ctx context.Context, tx pgx.Tx, publication domain.SummaryPublication) error {
	currentJob, err := lockSummaryPublicationInput(ctx, tx, publication.Input)
	if err != nil {
		return err
	}
	var recovered bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM item_summaries WHERE workspace_id=$1 AND item_id=$2
 AND source_version=decode($3,'hex') AND generation_job_id=$4)`, publication.Input.WorkspaceID, publication.Input.ItemID, publication.Input.SourceVersion, publication.Annotation.GenerationJobID).Scan(&recovered)
	if err != nil {
		return fmt.Errorf("resolve committed summary: %w", err)
	}
	if recovered {
		return nil
	}
	if currentJob == nil || *currentJob != publication.Annotation.GenerationJobID {
		return domain.ErrSummaryPublicationRejected
	}
	if err := validateSummaryExecution(ctx, tx, publication); err != nil {
		return err
	}
	if err := storeSummaryResult(ctx, tx, publication); err != nil {
		return err
	}
	return fillSummaryBindings(ctx, tx, publication)
}

func lockSummaryPublicationInput(ctx context.Context, tx pgx.Tx, input domain.SummaryInputKey) (*uuid.UUID, error) {
	var current *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT current_job_id FROM item_summary_inputs
 WHERE workspace_id=$1 AND item_id=$2 AND source_version=decode($3,'hex') FOR UPDATE`, input.WorkspaceID, input.ItemID, input.SourceVersion).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSummaryPublicationRejected
	}
	if err != nil {
		return nil, fmt.Errorf("lock summary publication input: %w", err)
	}
	return current, nil
}

func validateSummaryExecution(ctx context.Context, tx pgx.Tx, publication domain.SummaryPublication) error {
	job, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, publication.Input.WorkspaceID, publication.Annotation.GenerationJobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrSummaryPublicationRejected
	}
	if err != nil {
		return fmt.Errorf("lock summary generation execution: %w", err)
	}
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return fmt.Errorf("read summary fence time: %w", err)
	}
	if !liveJobExecution(job, publication.LeaseOwner, publication.Attempt, now) || !matchingSummaryJob(job, publication.Input) {
		return domain.ErrSummaryPublicationRejected
	}
	return nil
}

func storeSummaryResult(ctx context.Context, tx pgx.Tx, publication domain.SummaryPublication) error {
	annotation := publication.Annotation
	if !validSummaryPublication(publication) {
		return domain.ErrSummaryPublicationRejected
	}
	if err := validateReusedSummary(ctx, tx, publication); err != nil {
		return err
	}
	configuration, err := json.Marshal(publication.Configuration)
	if err != nil {
		return fmt.Errorf("encode summary provenance: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO item_summaries(id,workspace_id,item_id,source_version,configuration_fingerprint,configuration,text,generation_job_id,reused_from,created_at)
 VALUES($1,$2,$3,decode($4,'hex'),decode($5,'hex'),$6,$7,$8,$9,$10)`, annotation.ID, publication.Input.WorkspaceID, publication.Input.ItemID, publication.Input.SourceVersion, publication.ConfigurationFingerprint, configuration, annotation.Text, annotation.GenerationJobID, publication.ReusedFrom, annotation.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert immutable summary: %w", err)
	}
	return nil
}

func validSummaryPublication(publication domain.SummaryPublication) bool {
	limit := publication.Configuration.MaxSummaryCharacters
	return publication.Annotation.ID != uuid.Nil && strings.TrimSpace(publication.Annotation.Text) != "" && limit >= 200 && limit <= 4000 && utf8.RuneCountInString(publication.Annotation.Text) <= limit
}

func validateReusedSummary(ctx context.Context, tx pgx.Tx, publication domain.SummaryPublication) error {
	if publication.ReusedFrom == nil {
		return nil
	}
	var valid bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM item_summaries WHERE workspace_id=$1 AND item_id=$2
 AND source_version=decode($3,'hex') AND configuration_fingerprint=decode($4,'hex') AND id=$5 AND text=$6)`, publication.Input.WorkspaceID, publication.Input.ItemID, publication.Input.SourceVersion, publication.ConfigurationFingerprint, *publication.ReusedFrom, publication.Annotation.Text).Scan(&valid)
	if err != nil {
		return fmt.Errorf("validate reused summary: %w", err)
	}
	if !valid {
		return domain.ErrSummaryPublicationRejected
	}
	return nil
}

func fillSummaryBindings(ctx context.Context, tx pgx.Tx, publication domain.SummaryPublication) error {
	input := publication.Input
	_, err := tx.Exec(ctx, `UPDATE item_summary_inputs SET ready_summary_id=$4
 WHERE workspace_id=$1 AND item_id=$2 AND source_version=decode($3,'hex') AND current_job_id=$5`, input.WorkspaceID, input.ItemID, input.SourceVersion, publication.Annotation.ID, publication.Annotation.GenerationJobID)
	if err != nil {
		return fmt.Errorf("publish shared summary pointer: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE digest_item_summaries SET job_id=$4,summary_id=$5
 WHERE workspace_id=$1 AND item_id=$2 AND source_version=decode($3,'hex') AND summary_id IS NULL`, input.WorkspaceID, input.ItemID, input.SourceVersion, publication.Annotation.GenerationJobID, publication.Annotation.ID)
	if err != nil {
		return fmt.Errorf("fill absent Digest annotations: %w", err)
	}
	return nil
}
