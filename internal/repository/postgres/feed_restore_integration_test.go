//go:build integration

package postgres_test

import (
	"context"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	repository "github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestPostgresDumpRestorePreservesFeedHistoryAndLifetimeUniqueness(t *testing.T) {
	f := newFeedFixture(t)
	first, second := f.item(t), f.item(t)
	command := domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now, Publications: []domain.FeedPublication{{ItemID: first, Pipeline: "restore"}, {ItemID: second, Pipeline: "restore"}}}
	original, err := f.store.PublishFeed(t.Context(), command)
	require.NoError(t, err)
	require.Len(t, original.Publications, 2)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(context.Background())) }()
	var schema string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT current_schema()`).Scan(&schema))
	baseDSN := strings.TrimSuffix(f.dsn, " search_path="+schema)
	if strings.HasPrefix(f.dsn, "postgres://") || strings.HasPrefix(f.dsn, "postgresql://") {
		parsed, err := url.Parse(f.dsn)
		require.NoError(t, err)
		query := parsed.Query()
		query.Del("search_path")
		parsed.RawQuery = query.Encode()
		baseDSN = parsed.String()
	}
	dump, err := exec.LookPath("pg_dump")
	require.NoError(t, err)
	restore, err := exec.LookPath("pg_restore")
	require.NoError(t, err)
	archive := filepath.Join(t.TempDir(), "isolated-workspace.dump")
	output, err := exec.CommandContext(t.Context(), dump, "--dbname", baseDSN, "--schema", schema, "--format=custom", "--file", archive).CombinedOutput()
	require.NoError(t, err, "%s", output)
	f.store.Close()
	_, err = conn.Exec(t.Context(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	require.NoError(t, err)
	output, err = exec.CommandContext(t.Context(), restore, "--dbname", baseDSN, "--exit-on-error", archive).CombinedOutput()
	require.NoError(t, err, "%s", output)
	restored, err := repository.Open(t.Context(), f.dsn)
	require.NoError(t, err)
	defer restored.Close()
	head, err := restored.FeedHead(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, original.HeadOrdinal, head)
	rows, err := restored.ReadFeedPublications(t.Context(), domain.FeedPublicationQuery{WorkspaceID: f.workspaceID, HeadOrdinal: head, Limit: 25})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for index, row := range rows {
		expected := original.Publications[index]
		require.Equal(t, expected.ItemID, row.Item.ID)
		require.Equal(t, expected.BatchID, row.Publication.BatchID)
		require.Equal(t, expected.Ordinal, row.Publication.Ordinal)
		require.WithinDuration(t, expected.PublishedAt, row.Publication.PublishedAt, 0)
	}
	duplicate, err := restored.PublishFeed(t.Context(), command)
	require.NoError(t, err)
	require.Empty(t, duplicate.Publications)
	require.Equal(t, head, duplicate.HeadOrdinal)
}
