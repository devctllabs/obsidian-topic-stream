package summaries

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

// ReadDigest reads annotation states for immutable saved Items without admitting any work.
func (s *Service) ReadDigest(ctx context.Context, issue domain.DigestIssue) (map[uuid.UUID]domain.ItemSummary, error) {
	if issue.WorkspaceID != s.config.WorkspaceID {
		return nil, domain.ErrSummarySourceNotFound
	}
	settings, err := s.config.Settings.GetWorkspace(ctx)
	if err != nil {
		return nil, fmt.Errorf("read Digest annotation settings: %w", err)
	}
	bindings, err := s.config.Repository.ReadDigestSummaryBindings(ctx, s.config.WorkspaceID, issue.ID)
	if err != nil {
		return nil, fmt.Errorf("read Digest annotations: %w", err)
	}
	states := make(map[uuid.UUID]domain.ItemSummary)
	for _, section := range issue.Sections {
		for _, saved := range section.Items {
			input := domain.SummaryInputForItem(saved.Item)
			input.WorkspaceID = s.config.WorkspaceID
			state := summaryPermission(input, settings, false)
			states[input.ItemID] = boundSummaryState(state, bindings[input.SummaryInputKey], state.CanGenerate)
		}
	}
	return states, nil
}
