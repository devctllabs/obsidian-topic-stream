package summaries_test

import (
	"context"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestSummaryRetryUsesCapturedInputAndDurableAdmission(t *testing.T) {
	f := newSummaryFixture(t)
	require.NotNil(t, f.definition.RetryHandler)
	key := domain.SummaryInputKey{WorkspaceID: f.workspaceID, ItemID: uuid.New(), SourceVersion: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	source := domain.Job{ID: uuid.New(), WorkspaceID: f.workspaceID, Arguments: []byte(`{"itemId":"` + key.ItemID.String() + `","sourceVersion":"` + key.SourceVersion + `"}`)}
	input := domain.SummaryInput{SummaryInputKey: key, Markdown: "Old captured body", Origin: domain.ContentOriginArticleBody}
	f.repository.EXPECT().GetSummaryInput(gomock.Any(), key).Return(input, true, nil)
	f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(summarySettings(), nil)
	accepted := domain.Job{ID: uuid.New(), WorkspaceID: f.workspaceID}
	f.repository.EXPECT().AdmitSummary(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request domain.SummaryAdmission) (domain.SummaryAdmissionResult, error) {
		require.Equal(t, input, request.Input)
		require.Equal(t, &source.ID, request.RetrySourceJobID)
		require.Nil(t, request.IssueID)
		return domain.SummaryAdmissionResult{Job: accepted, Created: true}, nil
	})
	f.events.EXPECT().NotifyJobChanged(f.workspaceID, accepted.ID)
	actual, err := f.definition.RetryHandler(t.Context(), source)
	require.NoError(t, err)
	require.Equal(t, accepted, actual)
}
