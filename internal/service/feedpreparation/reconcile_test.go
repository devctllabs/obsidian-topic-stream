package feedpreparation_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feedpreparation"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feedpreparation/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type dueWorkspaces struct{ ids []uuid.UUID }

// DueFeedWorkspaces returns the configured Workspace IDs for the initial page only.
func (s dueWorkspaces) DueFeedWorkspaces(_ context.Context, q domain.FeedPreparationDueQuery) ([]uuid.UUID, error) {
	if q.After != nil {
		return nil, nil
	}
	return s.ids, nil
}
func TestReconcilerRecoversAllWorkspacesWithStableIdempotencyKeys(t *testing.T) {
	now := time.Now().UTC()
	queue := mocks.NewMockEnqueuer(gomock.NewController(t))
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	for _, workspaceID := range ids {
		queue.EXPECT().Enqueue(gomock.Any(), jobs.EnqueueRequest{WorkspaceID: workspaceID, Definition: feedpreparation.Definition, Arguments: struct{}{}, IdempotencyKey: domain.JobKindPrepareFeed}).Return(domain.Job{}, nil).Times(2)
	}
	reconciler := feedpreparation.NewReconciler(dueWorkspaces{ids: ids}, queue, func() time.Time { return now }, "gate-v1")
	require.NoError(t, reconciler.Reconcile(t.Context()))
	require.NoError(t, reconciler.Reconcile(t.Context()))
}
