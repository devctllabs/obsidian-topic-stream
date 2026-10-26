package postgres

import (
	"context"
	"fmt"
	"sort"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// recordDigestSummaryInputs adds separate annotation markers without admitting generation work.
func recordDigestSummaryInputs(ctx context.Context, tx pgx.Tx, issue domain.DigestIssue) error {
	inputs := digestSummaryInputs(issue)
	// Different Digest Definitions can share Items; all transactions lock them in the same order.
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].ItemID.String() < inputs[j].ItemID.String() })
	for _, input := range inputs {
		if err := recordDigestSummaryInput(ctx, tx, issue, input); err != nil {
			return err
		}
	}
	return nil
}

func digestSummaryInputs(issue domain.DigestIssue) []domain.SummaryInput {
	seen := make(map[uuid.UUID]bool)
	var inputs []domain.SummaryInput
	for _, section := range issue.Sections {
		for _, saved := range section.Items {
			input := domain.SummaryInputForItem(saved.Item)
			input.WorkspaceID = issue.WorkspaceID
			if input.Supported() && !seen[input.ItemID] {
				seen[input.ItemID] = true
				inputs = append(inputs, input)
			}
		}
	}
	return inputs
}

func recordDigestSummaryInput(ctx context.Context, tx pgx.Tx, issue domain.DigestIssue, input domain.SummaryInput) error {
	_, err := tx.Exec(ctx, `INSERT INTO item_summary_inputs(workspace_id,item_id,source_version,title,content_origin,created_at)
 VALUES($1,$2,decode($3,'hex'),$4,$5,$6) ON CONFLICT(workspace_id,item_id,source_version) DO NOTHING`, input.WorkspaceID, input.ItemID, input.SourceVersion, input.Title, input.Origin, issue.GeneratedAt)
	if err != nil {
		return fmt.Errorf("record saved summary input version: %w", err)
	}
	var pointers summaryPointers
	err = tx.QueryRow(ctx, `SELECT COALESCE(s.generation_job_id,i.current_job_id),i.ready_summary_id
 FROM item_summary_inputs i LEFT JOIN item_summaries s ON s.id=i.ready_summary_id
 WHERE i.workspace_id=$1 AND i.item_id=$2 AND i.source_version=decode($3,'hex') FOR UPDATE OF i`, input.WorkspaceID, input.ItemID, input.SourceVersion).Scan(&pointers.jobID, &pointers.summaryID)
	if err != nil {
		return fmt.Errorf("lock saved summary input version: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO digest_item_summaries(workspace_id,issue_id,item_id,source_version,job_id,summary_id,requested_at)
 VALUES($1,$2,$3,decode($4,'hex'),$5,$6,$7)`, issue.WorkspaceID, issue.ID, input.ItemID, input.SourceVersion, pointers.jobID, pointers.summaryID, issue.GeneratedAt)
	if err != nil {
		return fmt.Errorf("record saved Digest summary binding: %w", err)
	}
	return nil
}
