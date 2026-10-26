//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCanonicalContentOriginPersistsAndParticipatesInUnchangedDetection(t *testing.T) {
	f := newFeedFixture(t)
	itemID := uuid.New()
	url := "https://fixture.example/" + itemID.String()
	item := domain.Item{ID: itemID, WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, Title: "Body", URL: url, CanonicalURL: url, ContentMarkdown: "Same body", ContentOrigin: domain.ContentOriginArticleDescription, DiscoveredAt: f.now, Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{Description: "Same body"}}}
	entry := domain.SourceEntry{SourceID: f.sourceID, ExternalID: itemID.String(), ItemID: itemID, URL: url, DiscoveredAt: f.now, UpdatedAt: f.now, Details: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{Description: "Same body"}}}
	_, changed, err := f.store.UpsertItem(t.Context(), item, entry)
	require.NoError(t, err)
	require.True(t, changed)
	saved, found, err := f.store.GetItem(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, domain.ContentOriginArticleDescription, saved.ContentOrigin)
	item.ContentOrigin = domain.ContentOriginArticleBody
	_, changed, err = f.store.UpsertItem(t.Context(), item, entry)
	require.NoError(t, err)
	require.True(t, changed, "origin evidence changes must not take the unchanged shortcut")
	saved, found, err = f.store.GetItem(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, domain.ContentOriginArticleBody, saved.ContentOrigin)
	_, changed, err = f.store.UpsertItem(t.Context(), item, entry)
	require.NoError(t, err)
	require.False(t, changed)
}
