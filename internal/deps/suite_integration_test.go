//go:build integration

package deps_test

import (
	"os"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunIntegration(m, testutil.IntegrationConfig{Postgres: true, Qdrant: true}))
}
