package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type summaryAdmissionState struct {
	request domain.SummaryAdmission
	input   summaryPointers
	context summaryPointers
}

type summarySelection struct {
	job             domain.Job
	created         bool
	readyID         *uuid.UUID
	updateShared    bool
	preserveContext bool
}

// AdmitSummary atomically coordinates explicit input-scoped work and requested context bindings.
func (r *SummaryRepository) AdmitSummary(ctx context.Context, request domain.SummaryAdmission) (domain.SummaryAdmissionResult, error) {
	if !request.Input.Supported() || !matchingSummaryJob(request.Job, request.Input.SummaryInputKey) {
		return domain.SummaryAdmissionResult{}, domain.ErrSummaryUnavailable
	}
	tx, err := r.store.pool.Begin(ctx)
	if err != nil {
		return domain.SummaryAdmissionResult{}, fmt.Errorf("begin summary admission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	input, err := lockSummaryInput(ctx, tx, request)
	if err != nil {
		return domain.SummaryAdmissionResult{}, err
	}
	contextBinding, err := readDigestSummaryContext(ctx, tx, request)
	if err != nil {
		return domain.SummaryAdmissionResult{}, err
	}
	state := summaryAdmissionState{request: request, input: input, context: contextBinding}
	selection, err := selectSummaryWork(ctx, tx, state)
	if err != nil {
		return domain.SummaryAdmissionResult{}, err
	}
	if err := bindSummarySelection(ctx, tx, state, selection); err != nil {
		return domain.SummaryAdmissionResult{}, err
	}
	if err := bindSummaryRetry(ctx, tx, request, selection.job); err != nil {
		return domain.SummaryAdmissionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SummaryAdmissionResult{}, fmt.Errorf("commit summary admission: %w", err)
	}
	return domain.SummaryAdmissionResult{Job: selection.job, Created: selection.created}, nil
}

func matchingSummaryJob(job domain.Job, input domain.SummaryInputKey) bool {
	var arguments struct {
		ItemID        uuid.UUID `json:"itemId"`
		SourceVersion string    `json:"sourceVersion"`
	}
	if err := json.Unmarshal(job.Arguments, &arguments); err != nil {
		return false
	}
	if job.Kind != "summarize-item" || job.Queue != domain.JobQueueGeneration || job.ArgumentsVersion != 1 {
		return false
	}
	return job.WorkspaceID == input.WorkspaceID && arguments.ItemID == input.ItemID && arguments.SourceVersion == input.SourceVersion && job.IdempotencyKey != nil && *job.IdempotencyKey == "summarize-item:"+input.ItemID.String()+":"+input.SourceVersion
}

func lockSummaryInput(ctx context.Context, tx pgx.Tx, request domain.SummaryAdmission) (summaryPointers, error) {
	input := request.Input
	_, err := tx.Exec(ctx, `INSERT INTO item_summary_inputs(workspace_id,item_id,source_version,title,markdown,content_origin,created_at)
 VALUES($1,$2,decode($3,'hex'),$4,$5,$6,$7)
 ON CONFLICT(workspace_id,item_id,source_version) DO UPDATE SET markdown=EXCLUDED.markdown
 WHERE item_summary_inputs.markdown IS NULL`, input.WorkspaceID, input.ItemID, input.SourceVersion, input.Title, input.Markdown, input.Origin, request.Job.CreatedAt)
	if err != nil {
		return summaryPointers{}, fmt.Errorf("capture summary input: %w", err)
	}
	var pointers summaryPointers
	err = tx.QueryRow(ctx, `SELECT current_job_id,ready_summary_id FROM item_summary_inputs
 WHERE workspace_id=$1 AND item_id=$2 AND source_version=decode($3,'hex') FOR UPDATE`, input.WorkspaceID, input.ItemID, input.SourceVersion).Scan(&pointers.jobID, &pointers.summaryID)
	if err != nil {
		return summaryPointers{}, fmt.Errorf("lock summary input: %w", err)
	}
	return pointers, nil
}

func readDigestSummaryContext(ctx context.Context, query jobQuerier, request domain.SummaryAdmission) (summaryPointers, error) {
	if request.IssueID == nil {
		return summaryPointers{}, nil
	}
	var pointers summaryPointers
	var version string
	err := query.QueryRow(ctx, `SELECT encode(source_version,'hex'),job_id,summary_id FROM digest_item_summaries
 WHERE workspace_id=$1 AND issue_id=$2 AND item_id=$3`, request.Input.WorkspaceID, *request.IssueID, request.Input.ItemID).Scan(&version, &pointers.jobID, &pointers.summaryID)
	if err != nil {
		_, wrapped := found(err)
		return summaryPointers{}, wrapped
	}
	if version != request.Input.SourceVersion {
		return summaryPointers{}, domain.ErrSummaryUnavailable
	}
	return pointers, nil
}

func selectSummaryWork(ctx context.Context, tx pgx.Tx, state summaryAdmissionState) (summarySelection, error) {
	if state.request.RetrySourceJobID != nil {
		return selectSummaryRetry(ctx, tx, state.request)
	}
	if state.context.summaryID != nil {
		job, err := summaryJobByID(ctx, tx, state.request.Input.WorkspaceID, *state.context.jobID)
		return summarySelection{job: job, readyID: state.context.summaryID, preserveContext: true}, err
	}
	selection, found, err := activeSummarySelection(ctx, tx, state.request.Input.SummaryInputKey)
	if err != nil || found {
		return selection, err
	}
	selection, found, err = cachedSummarySelection(ctx, tx, state.request)
	if err != nil || found {
		return selection, err
	}
	selection, found, err = failedSummarySelection(ctx, tx, state)
	if err != nil || found {
		return selection, err
	}
	job, created, err := enqueueJob(ctx, tx, state.request.Job)
	return summarySelection{job: job, created: created, updateShared: true}, err
}

func activeSummarySelection(ctx context.Context, query jobQuerier, input domain.SummaryInputKey) (summarySelection, bool, error) {
	key := "summarize-item:" + input.ItemID.String() + ":" + input.SourceVersion
	job, err := scanJob(query.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE workspace_id=$1 AND idempotency_key=$2
 AND status IN ('queued','running','retry_wait')`, input.WorkspaceID, key))
	if err != nil {
		ok, wrapped := found(err)
		return summarySelection{}, ok, wrapped
	}
	if !matchingSummaryJob(job, input) {
		return summarySelection{}, false, domain.ErrSummaryUnavailable
	}
	result, ready, err := summaryForJob(ctx, query, input.WorkspaceID, job.ID)
	if err != nil {
		return summarySelection{}, false, err
	}
	selection := summarySelection{job: job, updateShared: true}
	if ready {
		if result.SourceVersion != input.SourceVersion {
			return summarySelection{}, false, domain.ErrSummaryPublicationRejected
		}
		selection.readyID = &result.ID
	}
	return selection, true, nil
}

func cachedSummarySelection(ctx context.Context, query jobQuerier, request domain.SummaryAdmission) (summarySelection, bool, error) {
	result, ready, err := findReadySummary(ctx, query, request.Input.SummaryInputKey, request.ConfigurationFingerprint)
	if err != nil || !ready {
		return summarySelection{}, false, err
	}
	job, err := summaryJobByID(ctx, query, request.Input.WorkspaceID, result.GenerationJobID)
	if err != nil {
		return summarySelection{}, false, err
	}
	return summarySelection{job: job, readyID: &result.ID, updateShared: true}, true, nil
}

func failedSummarySelection(ctx context.Context, query jobQuerier, state summaryAdmissionState) (summarySelection, bool, error) {
	id := state.context.jobID
	if id == nil {
		id = state.input.jobID
	}
	if id == nil {
		return summarySelection{}, false, nil
	}
	job, err := scanJob(query.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE workspace_id=$1 AND id=$2 AND status IN ('dead','cancelled')`, state.request.Input.WorkspaceID, *id))
	if err != nil {
		ok, wrapped := found(err)
		return summarySelection{}, ok, wrapped
	}
	return summarySelection{job: job}, true, nil
}

func bindSummarySelection(ctx context.Context, tx pgx.Tx, state summaryAdmissionState, selection summarySelection) error {
	if selection.preserveContext {
		return nil
	}
	input := state.request.Input
	if selection.updateShared {
		_, err := tx.Exec(ctx, `UPDATE item_summary_inputs SET current_job_id=$4,ready_summary_id=COALESCE($5,ready_summary_id)
 WHERE workspace_id=$1 AND item_id=$2 AND source_version=decode($3,'hex')
 AND (current_job_id,ready_summary_id) IS DISTINCT FROM ($4::uuid,COALESCE($5::uuid,ready_summary_id))`, input.WorkspaceID, input.ItemID, input.SourceVersion, selection.job.ID, selection.readyID)
		if err != nil {
			return fmt.Errorf("bind shared summary work: %w", err)
		}
	}
	if err := bindRequestedDigestSummary(ctx, tx, state.request, selection); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE digest_item_summaries SET job_id=$4,summary_id=$5
 WHERE workspace_id=$1 AND item_id=$2 AND source_version=decode($3,'hex') AND summary_id IS NULL
 AND (job_id IS NULL OR $5::uuid IS NOT NULL)`, input.WorkspaceID, input.ItemID, input.SourceVersion, selection.job.ID, selection.readyID)
	if err != nil {
		return fmt.Errorf("bind waiting Digest annotations: %w", err)
	}
	return nil
}

func bindRequestedDigestSummary(ctx context.Context, tx pgx.Tx, request domain.SummaryAdmission, selection summarySelection) error {
	if request.IssueID == nil {
		return nil
	}
	input := request.Input
	_, err := tx.Exec(ctx, `INSERT INTO digest_item_summaries(workspace_id,issue_id,item_id,source_version,job_id,summary_id,requested_at)
 VALUES($1,$2,$3,decode($4,'hex'),$5,$6,$7)
 ON CONFLICT(issue_id,item_id) DO UPDATE SET job_id=EXCLUDED.job_id,summary_id=EXCLUDED.summary_id
 WHERE digest_item_summaries.summary_id IS NULL
 AND (digest_item_summaries.job_id,digest_item_summaries.summary_id) IS DISTINCT FROM(EXCLUDED.job_id,EXCLUDED.summary_id)`, input.WorkspaceID, *request.IssueID, input.ItemID, input.SourceVersion, selection.job.ID, selection.readyID, request.Job.CreatedAt)
	if err != nil {
		return fmt.Errorf("bind requested Digest summary: %w", err)
	}
	return nil
}
