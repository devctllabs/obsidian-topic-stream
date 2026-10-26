//go:build integration

package postgres_test

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestDigestIssueSummaryPagesKeepTiesFiltersAndImmutableDetail(t *testing.T) {
	f := newFeedFixture(t)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Daily", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	for _, id := range ids {
		require.NoError(t, publishDigestFixture(t, f, domain.DigestIssue{ID: id, WorkspaceID: f.workspaceID,
			DigestID: definition.ID, Title: "Saved", Status: domain.DigestIssueComplete, GeneratedAt: f.now,
			Sections: []domain.DigestSection{{Items: []domain.DigestItem{{Item: domain.Item{ContentMarkdown: "immutable"}}}}}}))
	}
	query := domain.DigestIssueList{WorkspaceID: f.workspaceID, Limit: 2}
	first, err := f.store.ListDigestIssues(t.Context(), query)
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.Equal(t, ids[2], first[0].ID)
	query.Cursor = &domain.DigestIssueCursor{GeneratedAt: first[1].GeneratedAt, ID: first[1].ID}
	require.NoError(t, publishDigestFixture(t, f, domain.DigestIssue{ID: uuid.New(), WorkspaceID: f.workspaceID,
		DigestID: definition.ID, Title: "Newer", Status: domain.DigestIssueComplete, GeneratedAt: f.now.Add(time.Second)}))
	last, err := f.store.ListDigestIssues(t.Context(), query)
	require.NoError(t, err)
	require.Len(t, last, 1)
	require.Equal(t, ids[0], last[0].ID)
	query.DigestID = &definition.ID
	filtered, err := f.store.ListDigestIssues(t.Context(), query)
	require.NoError(t, err)
	require.Equal(t, last, filtered)
	otherID := uuid.New()
	query.DigestID = &otherID
	empty, err := f.store.ListDigestIssues(t.Context(), query)
	require.NoError(t, err)
	require.Empty(t, empty)
	query.WorkspaceID, query.DigestID = otherID, nil
	empty, err = f.store.ListDigestIssues(t.Context(), query)
	require.NoError(t, err)
	require.Empty(t, empty)
	detail, found, err := f.store.GetDigestIssue(t.Context(), f.workspaceID, ids[0])
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "immutable", detail.Sections[0].Items[0].Item.ContentMarkdown)
}

func TestDigestIssuesKeepSnapshotsAndAtomicallyRecordConsumedItems(t *testing.T) {
	f := newFeedFixture(t)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Weekly", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	firstID, secondID, rejectedID := uuid.New(), uuid.New(), uuid.New()
	issue := domain.DigestIssue{ID: uuid.New(), WorkspaceID: f.workspaceID, DigestID: definition.ID, Title: "Saved", Status: domain.DigestIssueComplete, ScheduledFor: &f.now, GeneratedAt: f.now,
		Sections: []domain.DigestSection{{Items: []domain.DigestItem{
			{Item: domain.Item{ID: firstID, ContentMarkdown: "Immutable full Markdown"}},
			{Item: domain.Item{ID: secondID}}, {Item: domain.Item{ID: firstID}},
		}}},
	}
	require.NoError(t, publishDigestFixture(t, f, issue))
	rejected := issue
	rejected.ID = uuid.New()
	rejected.Sections = []domain.DigestSection{{Items: []domain.DigestItem{{Item: domain.Item{ID: rejectedID}}}}}
	require.Error(t, publishDigestFixture(t, f, rejected), "one issue per scheduled instant")
	// Fail the final parent update after the snapshot and consumption writes have both run.
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `ALTER TABLE digest_definitions ADD CHECK(last_successful_at>=created_at)`)
	require.NoError(t, err)
	rejected.ID, rejected.ScheduledFor, rejected.GeneratedAt = uuid.New(), nil, f.now.Add(-time.Minute)
	require.Error(t, publishDigestFixture(t, f, rejected))
	_, found, err := f.store.GetDigestIssue(t.Context(), f.workspaceID, rejected.ID)
	require.NoError(t, err)
	require.False(t, found)
	candidates := []uuid.UUID{firstID, secondID, rejectedID, firstID}
	consumed, err := f.store.FindConsumedDigestItemIDs(t.Context(), f.workspaceID, definition.ID, candidates)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{firstID, secondID}, consumed)
	foreign, err := f.store.FindConsumedDigestItemIDs(t.Context(), uuid.New(), definition.ID, candidates)
	require.NoError(t, err)
	require.Empty(t, foreign)
	saved, found, err := f.store.GetDigestIssue(t.Context(), f.workspaceID, issue.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, issue.Sections, saved.Sections)
	// Manual issues can repeat Items. Consumption stays a set across issues and empty sections.
	issue.ID, issue.ScheduledFor = uuid.New(), nil
	require.NoError(t, publishDigestFixture(t, f, issue))
	issue.ID, issue.Sections = uuid.New(), nil
	require.NoError(t, publishDigestFixture(t, f, issue))
	consumed, err = f.store.FindConsumedDigestItemIDs(t.Context(), f.workspaceID, definition.ID, []uuid.UUID{secondID, rejectedID})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{secondID}, consumed)
	empty, err := f.store.FindConsumedDigestItemIDs(t.Context(), f.workspaceID, definition.ID, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}
