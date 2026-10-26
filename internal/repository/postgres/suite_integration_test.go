//go:build integration

package postgres_test

import (
	"os"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunIntegration(m, testutil.IntegrationConfig{Postgres: true}))
}
