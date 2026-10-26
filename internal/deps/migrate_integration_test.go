//go:build integration

package deps_test

import (
	"os"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestNewMigrateLoadsOnlyDatabaseSettingsWithCorrectPrecedence(t *testing.T) {
	for _, source := range []string{"dotenv", "environment", "url"} {
		t.Run(source, func(t *testing.T) {
			dsn := testutil.PostgresSchema(t)
			t.Chdir(t.TempDir())
			t.Setenv("TOPIC_STREAM_DB_PRIMARY_KIND", "postgres")
			t.Setenv("TOPIC_STREAM_WORKSPACE_ID", "invalid")
			t.Setenv("TOPIC_STREAM_SHUTDOWN_TIMEOUT", "invalid")
			t.Setenv("TOPIC_STREAM_WORKERS_ENABLED", "invalid")
			fileDSN := "invalid://file-secret"
			if source == "dotenv" {
				fileDSN = dsn
			}
			require.NoError(t, os.WriteFile(".env", []byte("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN="+fileDSN+"\n"), 0600))
			var opts deps.MigrateOptions
			switch source {
			case "dotenv":
				t.Setenv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN", "unused")
				require.NoError(t, os.Unsetenv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN"))
			case "environment":
				t.Setenv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN", dsn)
			case "url":
				t.Setenv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN", "invalid://environment-secret")
				opts.URL = &dsn
			}
			runtime, err := deps.NewMigrate(t.Context(), opts)
			require.NoError(t, err)
			state, err := runtime.Status(t.Context())
			require.NoError(t, err)
			require.Nil(t, state.Version)
			require.NoError(t, runtime.Shutdown(t.Context()))
		})
	}
}
