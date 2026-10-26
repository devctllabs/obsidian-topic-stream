//go:build integration

package testutil

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	postgresmigrations "github.com/ethernity/obsidian-topic-stream/migrations/primary/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// DefaultEmbeddingDescriptor returns a synthetic model for PostgreSQL fixtures without a provider.
func DefaultEmbeddingDescriptor() domain.EmbeddingModelDescriptor {
	return domain.EmbeddingModelDescriptor{
		Name: "Fixture embedding model", Endpoint: "http://fixture.example/v1", ProviderModel: "synthetic", Dimensions: 3,
	}
}

// PostgresSchema creates an empty isolated schema and returns a DSN with its search path.
// RunIntegration must have enabled Postgres before the test runs.
func PostgresSchema(t *testing.T) string {
	t.Helper()
	started := time.Now()
	dsn := integration.postgresDSN
	require.NotEmpty(t, dsn, "enable Postgres in TestMain's IntegrationConfig")
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	schema := "topic_stream_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schema}.Sanitize()
	t.Cleanup(func() {
		cleanupStarted := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, dropErr := conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+identifier+" CASCADE")
		require.NoError(t, errors.Join(dropErr, conn.Close(ctx)))
		t.Logf("integration phase=schema_cleanup elapsed_ms=%d", time.Since(cleanupStarted).Milliseconds())
	})
	_, err = conn.Exec(t.Context(), "CREATE SCHEMA "+identifier)
	require.NoError(t, err)
	t.Logf("integration phase=schema_setup elapsed_ms=%d", time.Since(started).Milliseconds())
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, parseErr := url.Parse(dsn)
		require.NoError(t, parseErr)
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return dsn + " search_path=" + schema
}

// MigratedPostgresSchema creates an isolated schema and applies the embedded application migrations.
func MigratedPostgresSchema(t *testing.T) string {
	t.Helper()
	dsn := PostgresSchema(t)
	started := time.Now()
	migrator, err := postgres.OpenMigrator(t.Context(), dsn)
	require.NoError(t, err)
	_, err = migrator.Up(t.Context(), 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(err, migrator.Shutdown(ctx)))
	t.Logf("integration phase=schema_migrations elapsed_ms=%d", time.Since(started).Milliseconds())
	return dsn
}

// MigrationVersions returns the ordered versions embedded in the application binary.
func MigrationVersions(t *testing.T) []uint {
	t.Helper()
	paths, err := fs.Glob(postgresmigrations.Files, "*.up.sql")
	require.NoError(t, err)
	versions := make([]uint, 0, len(paths))
	for _, path := range paths {
		version, err := strconv.ParseUint(strings.SplitN(path, "_", 2)[0], 10, 64)
		require.NoError(t, err)
		versions = append(versions, uint(version))
	}
	require.NotEmpty(t, versions)
	return versions
}
