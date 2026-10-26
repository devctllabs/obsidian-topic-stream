package deps_test

import (
	"os"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	"github.com/stretchr/testify/require"
)

func TestCLIConstructorsResolveConfigurationBeforeAcquisition(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("TOPIC_STREAM_WORKSPACE_ID", "invalid")
	t.Setenv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN", "postgres://unreachable.invalid/db")
	_, _, err := deps.NewServe(t.Context(), "test")
	require.ErrorContains(t, err, "workspace ID")
	_, err = deps.NewBenchFromEnv(t.Context())
	require.ErrorContains(t, err, "workspace ID")
	_, err = deps.NewReindex(t.Context())
	require.ErrorContains(t, err, "workspace ID")
	require.NotContains(t, err.Error(), "connect to Postgres")
}

func TestNewServeRejectsInvalidBootstrapBeforeAcquiringResources(t *testing.T) {
	for _, test := range []struct {
		name, key, value string
	}{
		{"empty endpoint", "ENDPOINT", ""},
		{"endpoint", "ENDPOINT", "not-a-url"},
		{"credentials", "ENDPOINT", "http://user:secret@localhost/v1"},
		{"query", "ENDPOINT", "http://localhost/v1?token=secret"},
		{"empty model", "MODEL", ""},
		{"model", "MODEL", " "},
		{"empty dimensions", "DIMENSIONS", ""},
		{"dimensions", "DIMENSIONS", "0"},
		{"dimensions upper bound", "DIMENSIONS", "65537"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("TOPIC_STREAM_WORKSPACE_ID", "f433587c-d050-44bb-a0d4-e64cdb8d5849")
			t.Setenv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN", "postgres://unreachable.invalid/db")
			t.Setenv("TOPIC_STREAM_BOOTSTRAP_EMBEDDING_"+test.key, test.value)
			_, _, err := deps.NewServe(t.Context(), "test")
			require.ErrorContains(t, err, "bootstrap embedding")
			require.NotContains(t, err.Error(), "connect to Postgres")
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestNewServeRejectsEmptyBootstrapFileValues(t *testing.T) {
	for _, key := range []string{"ENDPOINT", "MODEL", "DIMENSIONS"} {
		t.Run(key, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("TOPIC_STREAM_WORKSPACE_ID", "f433587c-d050-44bb-a0d4-e64cdb8d5849")
			t.Setenv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN", "postgres://unreachable.invalid/db")
			require.NoError(t, os.WriteFile(".env", []byte("TOPIC_STREAM_BOOTSTRAP_EMBEDDING_"+key+"=\n"), 0o600))
			_, _, err := deps.NewServe(t.Context(), "test")
			require.ErrorContains(t, err, "bootstrap embedding")
			require.NotContains(t, err.Error(), "connect to Postgres")
		})
	}
}
