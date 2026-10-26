//go:build integration

package main

import (
	"os"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunIntegration(m, testutil.IntegrationConfig{Postgres: true}))
}
