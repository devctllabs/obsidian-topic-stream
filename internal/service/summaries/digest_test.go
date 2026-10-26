package summaries_test

import (
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestReadDigestSummaryUsesSavedInputAndOneBatchWithoutGeneration(t *testing.T) {
	f := newSummaryFixture(t)
	item := domain.Item{ID: uuid.New(), WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, ContentMarkdown: "Saved body", ContentOrigin: domain.ContentOriginArticleBody}
	key := domain.SummaryInputForItem(item).SummaryInputKey
	issue := domain.DigestIssue{ID: uuid.New(), WorkspaceID: f.workspaceID, Sections: []domain.DigestSection{{Items: []domain.DigestItem{{Item: item}}}}}
	settings := summarySettings()
	settings.Generation.Enabled = false
	f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(settings, nil)
	annotation := domain.AISummary{ID: uuid.New(), SourceVersion: key.SourceVersion, Text: "Saved annotation"}
	f.repository.EXPECT().ReadDigestSummaryBindings(gomock.Any(), f.workspaceID, issue.ID).Return(map[domain.SummaryInputKey]domain.SummaryBinding{key: {Result: &annotation}}, nil)
	states, err := f.service.ReadDigest(t.Context(), issue)
	require.NoError(t, err)
	require.Len(t, states, 1)
	require.Equal(t, domain.ItemSummaryReady, states[item.ID].State)
	require.Equal(t, annotation.ID, states[item.ID].Result.ID)
	require.False(t, states[item.ID].CanGenerate)
	require.Nil(t, issue.Sections[0].Items[0].AISummary)
}
