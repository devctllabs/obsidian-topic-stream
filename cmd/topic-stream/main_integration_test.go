//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestBinaryMigrationsOutsideCheckout(t *testing.T) {
	dsn := testutil.PostgresSchema(t)
	versions := testutil.MigrationVersions(t)
	latest := versions[len(versions)-1]
	bin := filepath.Join(t.TempDir(), "topic-stream")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".")
	buildOutput, err := build.CombinedOutput()
	require.NoError(t, err, "%s", buildOutput)
	fixture := binaryFixture{path: bin, directory: t.TempDir(), env: append(os.Environ(),
		"TOPIC_STREAM_DB_PRIMARY_KIND=postgres", "TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN="+dsn,
		"TOPIC_STREAM_WORKSPACE_ID=invalid", "TOPIC_STREAM_SHUTDOWN_TIMEOUT=invalid",
	)}

	fixture.expectJSON(t, `{"version":null,"dirty":false}`, "db", "status", "--json")
	fixture.expectError(t, "version=none dirty=false", "db", "down", "--json")
	fixture.expectJSON(t, fmt.Sprintf(`{"version":%d,"dirty":false,"noChange":false}`, versions[0]), "db", "up", "--steps", "1", "--json")
	fixture.expectJSON(t, fmt.Sprintf(`{"version":%d,"dirty":false,"noChange":false}`, versions[2]), "db", "up", "--steps", "2", "--json")
	fixture.expectJSON(t, fmt.Sprintf(`{"version":%d,"dirty":false,"noChange":false}`, latest), "db", "up", "--json")
	fixture.expectJSON(t, fmt.Sprintf(`{"version":%d,"dirty":false,"noChange":true}`, latest), "db", "up", "--json")
	fixture.expectJSON(t, fmt.Sprintf(`{"version":%d,"dirty":false,"noChange":false}`, versions[len(versions)-2]), "db", "down", "--json")
	fixture.expectJSON(t, fmt.Sprintf(`{"version":%d,"dirty":false,"noChange":false}`, versions[2]), "db", "down", "--steps", strconv.Itoa(len(versions)-4), "--json")
	fixture.expectJSON(t, fmt.Sprintf(`{"version":%d,"dirty":false,"noChange":false}`, latest), "db", "up", "--json", "--url", dsn)
	fixture.expectError(t, "steps must be positive", "db", "up", "--steps", "0", "--json")
	fixture.expectError(t, "DSN is required", "db", "up", "--url", "", "--json")
	fixture.expectError(t, "flag provided but not defined", "db", "up", "--unknown")
	fixture.expectError(t, "unexpected positional arguments", "db", "status", "extra")
	fixture.expectError(t, "No help topic", "migrate")
	fixture.expectError(t, "connect migration database failed", "db", "status", "--json", "--url",
		"postgres://user:connection-secret@127.0.0.1:1/absent?connect_timeout=1")

	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(context.Background())) })
	var workspaces int
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT count(*) FROM workspaces").Scan(&workspaces))
	require.Zero(t, workspaces)
	_, err = conn.Exec(t.Context(), "UPDATE topic_stream_schema_migrations SET dirty = true")
	require.NoError(t, err)
	fixture.expectJSON(t, fmt.Sprintf(`{"version":%d,"dirty":true}`, latest), "db", "status", "--json")
	fixture.expectError(t, fmt.Sprintf("version=%d dirty=true", latest), "db", "up", "--json")
	fixture.expectError(t, fmt.Sprintf("version=%d dirty=true", latest), "db", "down", "--json")
	_, err = conn.Exec(t.Context(), "UPDATE topic_stream_schema_migrations SET version = -1")
	require.NoError(t, err)
	fixture.expectJSON(t, `{"version":null,"dirty":true}`, "db", "status", "--json")
	fixture.expectError(t, "version=none dirty=true", "db", "up", "--json")
}

type binaryFixture struct {
	path, directory string
	env             []string
}

func (f binaryFixture) run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	command := exec.CommandContext(t.Context(), f.path, args...)
	command.Dir, command.Env = f.directory, f.env
	var out, errOut bytes.Buffer
	command.Stdout, command.Stderr = &out, &errOut
	err := command.Run()
	return out.String(), errOut.String(), err
}

func (f binaryFixture) expectJSON(t *testing.T, want string, args ...string) {
	t.Helper()
	out, errOut, err := f.run(t, args...)
	require.NoError(t, err, "%s", errOut)
	require.Empty(t, errOut)
	require.JSONEq(t, want, out)
}

func (f binaryFixture) expectError(t *testing.T, want string, args ...string) {
	t.Helper()
	out, errOut, err := f.run(t, args...)
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 1, exit.ExitCode())
	require.Empty(t, out)
	require.Contains(t, errOut, want)
	require.NotContains(t, errOut, "connection-secret")
}
