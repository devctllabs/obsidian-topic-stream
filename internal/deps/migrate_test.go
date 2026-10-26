package deps_test

import (
	"context"
	"os"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	"github.com/stretchr/testify/require"
)

func TestNewMigrateRejectsInvalidConfigurationBeforeConnecting(t *testing.T) {
	for _, scenario := range []struct {
		name, kind, dsn string
		url             *string
		want            string
	}{
		{name: "missing", kind: "postgres", want: "DSN is required"},
		{name: "kind", kind: "sqlite", dsn: "postgres://unreachable.invalid/db", want: "postgres"},
		{name: "url-empty", kind: "postgres", dsn: "postgres://unreachable.invalid/db", url: stringPointer(""), want: "DSN is required"},
		{name: "invalid-url", kind: "postgres", url: stringPointer("https://user:secret@bad.invalid/db"), want: "invalid postgres DSN"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("TOPIC_STREAM_DB_PRIMARY_KIND", scenario.kind)
			t.Setenv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN", scenario.dsn)
			runtime, err := deps.NewMigrate(t.Context(), deps.MigrateOptions{URL: scenario.url})
			require.Nil(t, runtime)
			require.ErrorContains(t, err, scenario.want)
			require.NotContains(t, err.Error(), "secret")
			require.NotContains(t, err.Error(), "connect migration")
		})
	}
}

func TestNewMigrateExplicitEmptyEnvironmentOverridesDotenv(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile(".env", []byte("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN=postgres://unreachable.invalid/db\n"), 0600))
	t.Setenv("TOPIC_STREAM_DB_PRIMARY_KIND", "postgres")
	t.Setenv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN", "")
	_, err := deps.NewMigrate(t.Context(), deps.MigrateOptions{})
	require.ErrorContains(t, err, "DSN is required")
}

func TestNewMigrateCancellationBeforeAcquisition(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := deps.NewMigrate(ctx, deps.MigrateOptions{})
	require.ErrorIs(t, err, context.Canceled)
}

func stringPointer(value string) *string { return &value }

func TestNewMigrateRejectsMalformedDotenvWithoutPrintingInput(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile(".env", []byte("SECRET='unclosed-secret\n"), 0600))
	_, err := deps.NewMigrate(t.Context(), deps.MigrateOptions{URL: stringPointer("postgres://unreachable.invalid/db")})
	require.ErrorContains(t, err, "load migration configuration")
	require.NotContains(t, err.Error(), "unclosed-secret")
}
