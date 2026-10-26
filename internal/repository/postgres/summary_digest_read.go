package postgres

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type digestSummaryPointer struct {
	key domain.SummaryInputKey
	summaryPointers
}

// ReadDigestSummaryBindings loads all annotations using at most three queries regardless of Item count.
func (r *SummaryRepository) ReadDigestSummaryBindings(ctx context.Context, workspaceID, issueID uuid.UUID) (map[domain.SummaryInputKey]domain.SummaryBinding, error) {
	rows, err := r.store.pool.Query(ctx, `SELECT item_id,encode(source_version,'hex'),job_id,summary_id
 FROM digest_item_summaries WHERE workspace_id=$1 AND issue_id=$2`, workspaceID, issueID)
	if err != nil {
		return nil, fmt.Errorf("read Digest summary pointers: %w", err)
	}
	pointers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (digestSummaryPointer, error) {
		p := digestSummaryPointer{key: domain.SummaryInputKey{WorkspaceID: workspaceID}}
		err := row.Scan(&p.key.ItemID, &p.key.SourceVersion, &p.jobID, &p.summaryID)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan Digest summary pointers: %w", err)
	}
	var jobIDs, summaryIDs []uuid.UUID
	for _, p := range pointers {
		if p.jobID != nil {
			jobIDs = append(jobIDs, *p.jobID)
		}
		if p.summaryID != nil {
			summaryIDs = append(summaryIDs, *p.summaryID)
		}
	}
	jobs, err := r.summaryJobs(ctx, workspaceID, jobIDs)
	if err != nil {
		return nil, err
	}
	results, err := r.summaryResults(ctx, workspaceID, summaryIDs)
	if err != nil {
		return nil, err
	}
	return digestSummaryBindings(pointers, jobs, results), nil
}

func digestSummaryBindings(pointers []digestSummaryPointer, jobs map[uuid.UUID]*domain.Job, results map[uuid.UUID]*domain.AISummary) map[domain.SummaryInputKey]domain.SummaryBinding {
	bindings := make(map[domain.SummaryInputKey]domain.SummaryBinding, len(pointers))
	for _, p := range pointers {
		binding := domain.SummaryBinding{}
		if p.jobID != nil {
			binding.Job = jobs[*p.jobID]
		}
		if p.summaryID != nil {
			binding.Result = results[*p.summaryID]
		}
		bindings[p.key] = binding
	}
	return bindings
}

func (r *SummaryRepository) summaryJobs(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*domain.Job, error) {
	result := make(map[uuid.UUID]*domain.Job)
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := r.store.pool.Query(ctx, `SELECT `+jobColumns+` FROM jobs WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("read Digest summary Jobs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Digest summary Job: %w", err)
		}
		result[job.ID] = &job
	}
	return result, rows.Err()
}

func (r *SummaryRepository) summaryResults(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*domain.AISummary, error) {
	result := make(map[uuid.UUID]*domain.AISummary)
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := r.store.pool.Query(ctx, `SELECT `+summaryResultColumns+` FROM item_summaries WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("read Digest summaries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		summary, err := scanSummaryResult(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Digest summary: %w", err)
		}
		result[summary.ID] = &summary
	}
	return result, rows.Err()
}
