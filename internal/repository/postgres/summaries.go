package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

// SummaryRepository owns versioned Item inputs, generated annotations and requested context bindings.
type SummaryRepository struct{ store *Store }

// Summaries returns the independent Item-summary persistence capability.
func (s *Store) Summaries() *SummaryRepository { return &SummaryRepository{store: s} }

// GetSummarySource resolves owned canonical or captured Digest text without fetching external content.
func (r *SummaryRepository) GetSummarySource(ctx context.Context, workspaceID uuid.UUID, source domain.SummarySource) (domain.Item, bool, error) {
	if source.IssueID == nil {
		return r.store.GetItem(ctx, workspaceID, source.ItemID)
	}
	var encoded []byte
	err := r.store.pool.QueryRow(ctx, `SELECT selected.value->'Item' FROM digest_issues issue
 CROSS JOIN LATERAL jsonb_array_elements(COALESCE(NULLIF(issue.sections,'null'::jsonb),'[]'::jsonb)) section(value)
 CROSS JOIN LATERAL jsonb_array_elements(COALESCE(NULLIF(section.value->'Items','null'::jsonb),'[]'::jsonb)) selected(value)
 WHERE issue.workspace_id=$1 AND issue.id=$2 AND selected.value->'Item'->>'ID'=$3 LIMIT 1`, workspaceID, *source.IssueID, source.ItemID.String()).Scan(&encoded)
	if err != nil {
		ok, wrapped := found(err)
		return domain.Item{}, ok, wrapped
	}
	var item domain.Item
	if err := json.Unmarshal(encoded, &item); err != nil {
		return domain.Item{}, false, fmt.Errorf("decode captured summary source: %w", err)
	}
	return item, true, nil
}
