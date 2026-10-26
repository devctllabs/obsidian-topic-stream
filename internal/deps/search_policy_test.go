package deps

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQdrantPolicyInvalidOperatorValuesFailBeforeAcquisition(t *testing.T) {
	for _, test := range []struct{ key, value string }{
		{"QDRANT_HNSW_EF", "-1"}, {"QDRANT_FULL_SCAN_THRESHOLD_KB", "-1"}, {"QDRANT_ACORN_MAX_SELECTIVITY_PERCENT", "101"}, {"QDRANT_ACORN_MAX_SELECTIVITY_PERCENT", "0"},
	} {
		t.Run(test.key+test.value, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("TOPIC_STREAM_WORKSPACE_ID", "f433587c-d050-44bb-a0d4-e64cdb8d5849")
			t.Setenv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN", "postgres://unreachable.invalid/db")
			t.Setenv("TOPIC_STREAM_"+test.key, test.value)
			_, _, err := NewServe(t.Context(), "test")
			require.ErrorContains(t, err, "Qdrant search policy")
			require.NotContains(t, err.Error(), "connect to Postgres")
		})
	}
}
