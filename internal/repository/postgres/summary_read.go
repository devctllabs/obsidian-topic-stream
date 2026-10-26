package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

const summaryResultColumns = `id,generation_job_id,encode(source_version,'hex'),configuration,text,created_at`

type summaryPointers struct {
	jobID     *uuid.UUID
	summaryID *uuid.UUID
}

// GetSummaryInput reads immutable text previously captured by an explicit request.
func (r *SummaryRepository) GetSummaryInput(ctx context.Context, key domain.SummaryInputKey) (domain.SummaryInput, bool, error) {
	input := domain.SummaryInput{SummaryInputKey: key}
	var markdown *string
	err := r.store.pool.QueryRow(ctx, `SELECT title,markdown,content_origin FROM item_summary_inputs
 WHERE workspace_id=$1 AND item_id=$2 AND source_version=decode($3,'hex')`, key.WorkspaceID, key.ItemID, key.SourceVersion).Scan(&input.Title, &markdown, &input.Origin)
	if err != nil {
		ok, wrapped := found(err)
		return domain.SummaryInput{}, ok, wrapped
	}
	if markdown == nil {
		return domain.SummaryInput{}, false, nil
	}
	input.Markdown = *markdown
	return input, true, nil
}

// GetSummaryResultForJob recovers committed work independently of mutable source/settings state.
func (r *SummaryRepository) GetSummaryResultForJob(ctx context.Context, workspaceID, jobID uuid.UUID) (domain.AISummary, bool, error) {
	return summaryForJob(ctx, r.store.pool, workspaceID, jobID)
}

func summaryForJob(ctx context.Context, query jobQuerier, workspaceID, jobID uuid.UUID) (domain.AISummary, bool, error) {
	result, err := scanSummaryResult(query.QueryRow(ctx, `SELECT `+summaryResultColumns+` FROM item_summaries WHERE workspace_id=$1 AND generation_job_id=$2`, workspaceID, jobID))
	if err != nil {
		ok, wrapped := found(err)
		return domain.AISummary{}, ok, wrapped
	}
	return result, true, nil
}

// FindReadySummary reads cache only for the exact Item/input and relevant generation semantics.
func (r *SummaryRepository) FindReadySummary(ctx context.Context, key domain.SummaryInputKey, fingerprint string) (domain.AISummary, bool, error) {
	return findReadySummary(ctx, r.store.pool, key, fingerprint)
}

func findReadySummary(ctx context.Context, query jobQuerier, key domain.SummaryInputKey, fingerprint string) (domain.AISummary, bool, error) {
	result, err := scanSummaryResult(query.QueryRow(ctx, `SELECT `+summaryResultColumns+` FROM item_summaries
 WHERE workspace_id=$1 AND item_id=$2 AND source_version=decode($3,'hex') AND configuration_fingerprint=decode($4,'hex')
 ORDER BY created_at DESC,id DESC LIMIT 1`, key.WorkspaceID, key.ItemID, key.SourceVersion, fingerprint))
	if err != nil {
		ok, wrapped := found(err)
		return domain.AISummary{}, ok, wrapped
	}
	return result, true, nil
}

func scanSummaryResult(row scanner) (domain.AISummary, error) {
	var result domain.AISummary
	var encoded []byte
	if err := row.Scan(&result.ID, &result.GenerationJobID, &result.SourceVersion, &encoded, &result.Text, &result.CreatedAt); err != nil {
		return domain.AISummary{}, err
	}
	var configuration domain.SummaryConfiguration
	if err := json.Unmarshal(encoded, &configuration); err != nil {
		return domain.AISummary{}, fmt.Errorf("decode summary provenance: %w", err)
	}
	result.Model = configuration.Model
	result.Language = configuration.Language
	result.MaxInputChars = configuration.MaxInputChars
	result.MaxSummaryCharacters = configuration.MaxSummaryCharacters
	result.InstructionsVersion = configuration.InstructionsVersion
	result.AlgorithmVersion = configuration.AlgorithmVersion
	return result, nil
}

// ReadSummaryBinding reads accepted state only and never starts generation or binds another version.
func (r *SummaryRepository) ReadSummaryBinding(ctx context.Context, query domain.SummaryBindingQuery) (domain.SummaryBinding, error) {
	pointers, err := readSummaryPointers(ctx, r.store.pool, query)
	if err != nil {
		return domain.SummaryBinding{}, err
	}
	return hydrateSummaryBinding(ctx, r.store.pool, query.WorkspaceID, pointers)
}

func readSummaryPointers(ctx context.Context, query jobQuerier, input domain.SummaryBindingQuery) (summaryPointers, error) {
	var pointers summaryPointers
	statement := `SELECT current_job_id,ready_summary_id FROM item_summary_inputs WHERE workspace_id=$1 AND item_id=$2 AND source_version=decode($3,'hex')`
	args := []any{input.WorkspaceID, input.ItemID, input.SourceVersion}
	if input.IssueID != nil {
		statement = `SELECT job_id,summary_id FROM digest_item_summaries WHERE workspace_id=$1 AND item_id=$2 AND source_version=decode($3,'hex') AND issue_id=$4`
		args = append(args, *input.IssueID)
	}
	if err := query.QueryRow(ctx, statement, args...).Scan(&pointers.jobID, &pointers.summaryID); err != nil {
		_, wrapped := found(err)
		return summaryPointers{}, wrapped
	}
	return pointers, nil
}

func hydrateSummaryBinding(ctx context.Context, query jobQuerier, workspaceID uuid.UUID, pointers summaryPointers) (domain.SummaryBinding, error) {
	var binding domain.SummaryBinding
	if pointers.jobID != nil {
		job, err := summaryJobByID(ctx, query, workspaceID, *pointers.jobID)
		if err != nil {
			return binding, err
		}
		binding.Job = &job
	}
	if pointers.summaryID != nil {
		result, err := scanSummaryResult(query.QueryRow(ctx, `SELECT `+summaryResultColumns+` FROM item_summaries WHERE workspace_id=$1 AND id=$2`, workspaceID, *pointers.summaryID))
		if err != nil {
			return binding, fmt.Errorf("read bound summary: %w", err)
		}
		binding.Result = &result
	}
	return binding, nil
}

func summaryJobByID(ctx context.Context, query jobQuerier, workspaceID, jobID uuid.UUID) (domain.Job, error) {
	job, err := scanJob(query.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE workspace_id=$1 AND id=$2`, workspaceID, jobID))
	if err != nil {
		return domain.Job{}, fmt.Errorf("read summary job: %w", err)
	}
	return job, nil
}
