//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSummarySourceKeepsCapturedBodyAndProvenanceAfterCanonicalChangeAndDefinitionDeletion(t *testing.T) {
	f := newFeedFixture(t)
	itemID := f.item(t)
	item, found, err := f.store.GetItem(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, found)
	item.ContentOrigin = domain.ContentOriginArticleBody
	item.ContentMarkdown = "Old full article body"
	entry := domain.SourceEntry{SourceID: f.sourceID, ExternalID: itemID.String(), ItemID: itemID, URL: item.URL, DiscoveredAt: f.now, UpdatedAt: f.now, Details: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{Description: "Fixture"}}}
	_, _, err = f.store.UpsertItem(t.Context(), item, entry)
	require.NoError(t, err)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Saved", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	issueID := uuid.New()
	require.NoError(t, publishDigestFixture(t, f, domain.DigestIssue{ID: issueID, WorkspaceID: f.workspaceID, DigestID: definition.ID, Title: "Snapshot", Status: domain.DigestIssueComplete, GeneratedAt: f.now, Sections: []domain.DigestSection{{Items: []domain.DigestItem{{Item: item}}}}}))
	item.ContentMarkdown = "New description"
	item.ContentOrigin = domain.ContentOriginArticleDescription
	_, _, err = f.store.UpsertItem(t.Context(), item, entry)
	require.NoError(t, err)
	_, err = f.store.DeleteDigest(t.Context(), domain.DigestDelete{WorkspaceID: f.workspaceID, DigestID: definition.ID, ExpectedVersion: 1, DeletedAt: f.now})
	require.NoError(t, err)
	canonical, found, err := f.store.Summaries().GetSummarySource(t.Context(), f.workspaceID, domain.SummarySource{ItemID: itemID})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "New description", canonical.ContentMarkdown)
	captured, found, err := f.store.Summaries().GetSummarySource(t.Context(), f.workspaceID, domain.SummarySource{ItemID: itemID, IssueID: &issueID})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "Old full article body", captured.ContentMarkdown)
	require.Equal(t, domain.ContentOriginArticleBody, captured.ContentOrigin)
	_, found, err = f.store.Summaries().GetSummarySource(t.Context(), uuid.New(), domain.SummarySource{ItemID: itemID, IssueID: &issueID})
	require.NoError(t, err)
	require.False(t, found)
	_, found, err = f.store.Summaries().GetSummarySource(t.Context(), f.workspaceID, domain.SummarySource{ItemID: uuid.New(), IssueID: &issueID})
	require.NoError(t, err)
	require.False(t, found)
}
