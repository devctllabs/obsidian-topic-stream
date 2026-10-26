//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	repository "github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestMigrationLifecycle(t *testing.T) {
	dsn := testutil.PostgresSchema(t)
	versions := testutil.MigrationVersions(t)
	require.Len(t, versions, 8, "the clean-install baseline has one pair per functional area")
	latest := versions[len(versions)-1]
	migrator, err := repository.OpenMigrator(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, migrator.Shutdown(context.Background())) })
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(context.Background())) })
	state, err := migrator.Status(t.Context())
	require.NoError(t, err)
	require.Nil(t, state.Version)
	require.False(t, state.Dirty)
	assertMigrationRelation(t, conn, "topic_stream_schema_migrations", true)
	_, err = migrator.Down(t.Context(), 1)
	require.Error(t, err)

	noChange, err := migrator.Up(t.Context(), 1)
	require.NoError(t, err)
	require.False(t, noChange)
	assertMigrationVersion(t, migrator, versions[0])
	assertMigrationRelation(t, conn, "workspaces", true)
	assertMigrationRelation(t, conn, "sources", false)
	var workspaceCount int
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT count(*) FROM workspaces").Scan(&workspaceCount))
	require.Zero(t, workspaceCount)

	_, err = migrator.Up(t.Context(), 1)
	require.NoError(t, err)
	assertMigrationVersion(t, migrator, versions[1])
	assertMigrationRelation(t, conn, "items_workspace_id_id_key", true)
	assertMigrationRelation(t, conn, "items_content_date_idx", true)
	_, err = conn.Exec(t.Context(), `
		INSERT INTO workspaces (id) VALUES ('00000000-0000-4000-8000-000000000001');
		INSERT INTO items (id, workspace_id, details, title, url, canonical_url)
		VALUES ('00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001',
			'{}', 'Migration fixture', 'https://example.com/item', 'https://example.com/item');`)
	require.NoError(t, err)

	_, err = migrator.Up(t.Context(), 1)
	require.NoError(t, err)
	assertMigrationVersion(t, migrator, versions[2])
	assertMigrationRelation(t, conn, "topics_workspace_id_id_key", true)
	_, err = conn.Exec(t.Context(), `INSERT INTO topics (id, workspace_id, name, include_text)
		VALUES ('00000000-0000-4000-8000-000000000003', '00000000-0000-4000-8000-000000000001',
			'Migration fixture', 'Databases')`)
	require.NoError(t, err)

	_, err = migrator.Up(t.Context(), 1)
	require.NoError(t, err)
	assertMigrationVersion(t, migrator, versions[3])
	assertMigrationRelation(t, conn, "jobs_workspace_status_created_idx", true)
	_, err = conn.Exec(t.Context(), `INSERT INTO jobs (id, workspace_id, kind, queue)
		VALUES ('00000000-0000-4000-8000-000000000004', '00000000-0000-4000-8000-000000000001',
			'prepare-topic', 'feed-preparation')`)
	require.NoError(t, err)

	_, err = migrator.Up(t.Context(), 1)
	require.NoError(t, err)
	assertMigrationVersion(t, migrator, versions[4])
	var projectionVersion int64
	require.NoError(t, conn.QueryRow(t.Context(), `INSERT INTO item_projection_state
		(workspace_id, item_id, revision, updated_at)
		VALUES ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002',
			'00000000-0000-4000-8000-000000000005', now()) RETURNING version`).Scan(&projectionVersion))
	require.Equal(t, int64(1), projectionVersion)
	_, err = conn.Exec(t.Context(), "UPDATE item_projection_state SET version = 0")
	var constraintError *pgconn.PgError
	require.ErrorAs(t, err, &constraintError)
	require.Equal(t, "23514", constraintError.Code)

	_, err = migrator.Up(t.Context(), 10)
	var short migrate.ErrShortLimit
	require.ErrorAs(t, err, &short)
	assertMigrationVersion(t, migrator, latest)
	assertMigrationRelation(t, conn, "digest_issues", true)
	assertMigrationRelation(t, conn, "digest_consumed_items", true)
	assertMigrationRelation(t, conn, "feed_publications", true)
	assertMigrationRelation(t, conn, "topic_candidate_boundary_idx", true)
	_, err = conn.Exec(t.Context(), `
		INSERT INTO feed_state (workspace_id, head_ordinal)
		VALUES ('00000000-0000-4000-8000-000000000001', 1);
		INSERT INTO feed_publications
			(workspace_id, item_id, batch_id, published_at, ordinal, score, pipeline, model_id)
		VALUES ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002',
			'00000000-0000-4000-8000-000000000006', now(), 1, 1, 'fixture',
			'00000000-0000-4000-8000-000000000007');
		INSERT INTO topic_candidate_state (workspace_id, topic_id)
		VALUES ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000003');
		INSERT INTO topic_candidates (workspace_id, topic_id, item_id, position, score)
		VALUES ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000003',
			'00000000-0000-4000-8000-000000000002', 1, 1);`)
	require.NoError(t, err)

	noChange, err = migrator.Up(t.Context(), 0)
	require.NoError(t, err)
	require.True(t, noChange)
	_, err = migrator.Down(t.Context(), len(versions)-7)
	require.NoError(t, err)
	assertMigrationRelation(t, conn, "digest_build_requests", true)
	assertMigrationRelation(t, conn, "item_summary_inputs", false)
	assertMigrationRelation(t, conn, "item_summaries", false)
	assertMigrationRelation(t, conn, "model_transition_jobs", true)
	_, err = migrator.Down(t.Context(), 1)
	require.NoError(t, err)
	assertMigrationVersion(t, migrator, versions[5])
	assertMigrationRelation(t, conn, "items", true)
	assertMigrationRelation(t, conn, "topics", true)
	assertMigrationRelation(t, conn, "items_workspace_id_id_key", true)
	assertMigrationRelation(t, conn, "topics_workspace_id_id_key", true)
	assertMigrationRelation(t, conn, "items_content_date_idx", true)
	for _, name := range []string{"feed_state", "feed_publications", "topic_candidate_state", "topic_candidates", "topic_candidate_boundary_idx"} {
		assertMigrationRelation(t, conn, name, false)
	}
	var jobs int
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT count(*) FROM jobs WHERE queue = 'feed-preparation'").Scan(&jobs))
	require.Equal(t, 1, jobs)
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT version FROM item_projection_state").Scan(&projectionVersion))
	require.Equal(t, int64(1), projectionVersion)

	_, err = migrator.Up(t.Context(), 0)
	require.NoError(t, err)
	_, err = migrator.Down(t.Context(), len(versions))
	require.NoError(t, err)
	state, err = migrator.Status(t.Context())
	require.NoError(t, err)
	require.Nil(t, state.Version)
	require.False(t, state.Dirty)
	var tables int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name <> 'topic_stream_schema_migrations'`).Scan(&tables))
	require.Zero(t, tables)

	_, err = migrator.Up(t.Context(), 0)
	require.NoError(t, err)
	assertMigrationVersion(t, migrator, latest)
	assertMigrationRelation(t, conn, "digest_issues", true)
	assertMigrationRelation(t, conn, "feed_publications", true)
	require.Len(t, versions, 8)
}

func TestMigrationDirtyAndCancellation(t *testing.T) {
	dsn := testutil.PostgresSchema(t)
	versions := testutil.MigrationVersions(t)
	latest := versions[len(versions)-1]
	migrator, err := repository.OpenMigrator(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, migrator.Shutdown(context.Background())) })
	_, err = migrator.Up(t.Context(), 0)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = migrator.Down(ctx, 1)
	require.ErrorIs(t, err, context.Canceled)
	assertMigrationVersion(t, migrator, latest)

	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(context.Background())) })
	_, err = conn.Exec(t.Context(), "UPDATE topic_stream_schema_migrations SET dirty = true")
	require.NoError(t, err)
	state, err := migrator.Status(t.Context())
	require.NoError(t, err)
	require.True(t, state.Dirty)
	_, err = migrator.Up(t.Context(), 0)
	var dirty migrate.ErrDirty
	require.ErrorAs(t, err, &dirty)
	_, err = migrator.Down(t.Context(), 1)
	require.ErrorAs(t, err, &dirty)
}

func TestMigrationFunctionalRollbackOwnership(t *testing.T) {
	dsn := testutil.PostgresSchema(t)
	migrator, err := repository.OpenMigrator(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, migrator.Shutdown(context.Background())) })
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(context.Background())) })
	_, err = migrator.Up(t.Context(), 0)
	require.NoError(t, err)
	areas := [][]string{
		{"workspaces", "embedding_models", "workspace_settings", "preference_profiles", "preference_profile_revisions", "workspace_imports"},
		{"sources", "items", "source_entries", "item_feedback", "pending_item_feedback", "item_state"},
		{"topics", "topic_sources", "topic_item_kinds", "topic_revisions", "topic_corrections", "topic_gate_calibrations"},
		{"jobs"},
		{"embedding_model_collections", "workspace_retrieval_state", "item_projection_state", "workspace_model_progress", "retrieval_rebuilds", "retrieval_projection_pending", "retrieval_rebuild_progress", "retrieval_rebuild_cohort", "model_transition_jobs"},
		{"digest_definitions", "digest_issues", "digest_consumed_items", "digest_build_requests"},
		{"feed_state", "feed_preparation_requests", "feed_publications", "feed_display_entries", "topic_candidate_state", "topic_candidates"},
		{"item_summary_inputs", "item_summaries", "digest_item_summaries", "item_summary_retries"},
	}
	require.Len(t, testutil.MigrationVersions(t), len(areas))
	for last := len(areas) - 1; last >= 0; last-- {
		_, err = migrator.Down(t.Context(), 1)
		require.NoError(t, err, "rollback %s", areas[last][0])
		for area, relations := range areas {
			for _, relation := range relations {
				assertMigrationRelation(t, conn, relation, area < last)
			}
		}
	}
	state, err := migrator.Status(t.Context())
	require.NoError(t, err)
	require.Nil(t, state.Version)
	require.False(t, state.Dirty)
}

func TestMigrationSQLFailureKeepsDirtyStateWithoutPrintingSQL(t *testing.T) {
	dsn := testutil.PostgresSchema(t)
	versions := testutil.MigrationVersions(t)
	migrator, err := repository.OpenMigrator(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, migrator.Shutdown(context.Background())) })
	_, err = migrator.Up(t.Context(), 1)
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(context.Background())) })
	_, err = conn.Exec(t.Context(), "CREATE TABLE sources (secret text)")
	require.NoError(t, err)
	_, err = migrator.Up(t.Context(), 1)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "CREATE TABLE")
	require.NotContains(t, err.Error(), "secret")
	require.NotNil(t, errors.Unwrap(err))
	state, err := migrator.Status(t.Context())
	require.NoError(t, err)
	require.Equal(t, versions[1], *state.Version)
	require.True(t, state.Dirty)
}

func TestMigrationStatusPreservesDirtyWithoutAppliedVersion(t *testing.T) {
	dsn := testutil.PostgresSchema(t)
	migrator, err := repository.OpenMigrator(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, migrator.Shutdown(context.Background())) })
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(context.Background())) })
	// Rolling back the first migration marks the target nil version dirty before executing SQL.
	_, err = conn.Exec(t.Context(), "INSERT INTO topic_stream_schema_migrations (version, dirty) VALUES (-1, true)")
	require.NoError(t, err)
	state, err := migrator.Status(t.Context())
	require.NoError(t, err)
	require.Nil(t, state.Version)
	require.True(t, state.Dirty)
	_, err = migrator.Up(t.Context(), 0)
	var dirty migrate.ErrDirty
	require.ErrorAs(t, err, &dirty)
	_, err = migrator.Down(t.Context(), 1)
	require.ErrorAs(t, err, &dirty)
}

func assertMigrationVersion(t *testing.T, migrator *repository.Migrator, version uint) {
	t.Helper()
	state, err := migrator.Status(t.Context())
	require.NoError(t, err)
	require.NotNil(t, state.Version)
	require.Equal(t, version, *state.Version)
	require.False(t, state.Dirty)
}

func assertMigrationRelation(t *testing.T, conn *pgx.Conn, name string, exists bool) {
	t.Helper()
	var found bool
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&found))
	require.Equal(t, exists, found, name)
}

func TestMigrationConstructorReleasesConnectionAfterLockFailure(t *testing.T) {
	dsn := testutil.PostgresSchema(t)
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(context.Background())) })
	var schema string
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT current_schema()").Scan(&schema))
	lockID, err := database.GenerateAdvisoryLockId(conn.Config().Database, schema, "topic_stream_schema_migrations")
	require.NoError(t, err)
	lock, err := strconv.ParseInt(lockID, 10, 64)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), "SELECT pg_advisory_lock($1)", lock)
	require.NoError(t, err)
	name := schema + "_failed"
	failedDSN := dsn + " application_name=" + name + " statement_timeout=100"
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, parseErr := url.Parse(dsn)
		require.NoError(t, parseErr)
		query := parsed.Query()
		query.Set("application_name", name)
		query.Set("statement_timeout", "100")
		parsed.RawQuery = query.Encode()
		failedDSN = parsed.String()
	}
	runtime, err := repository.OpenMigrator(t.Context(), failedDSN)
	require.Nil(t, runtime)
	require.Error(t, err)
	require.Eventually(t, func() bool {
		var connections int
		queryErr := conn.QueryRow(t.Context(), "SELECT count(*) FROM pg_stat_activity WHERE application_name = $1", name).Scan(&connections)
		return queryErr == nil && connections == 0
	}, time.Second, 10*time.Millisecond, "constructor left a migration connection open")
}
