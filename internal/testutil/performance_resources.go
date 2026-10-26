//go:build integration && (performance || live)

package testutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// RequireOwnedQdrant prevents local restart/recovery profiles from targeting another instance.
func RequireOwnedQdrant(t *testing.T) {
	t.Helper()
	container, runID := os.Getenv("TOPIC_STREAM_PERF_QDRANT_CONTAINER"), os.Getenv("TOPIC_STREAM_PERF_RUN_ID")
	require.True(t, strings.HasPrefix(container, "topic-stream-qdrant-search-perf-"), "use the tracked capped local runner")
	require.NotEmpty(t, runID, "runner ownership is required")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	label := performanceDockerValue(t, ctx, "inspect", "--format", `{{index .Config.Labels "topic-stream.performance-run"}}`, container)
	require.Equal(t, runID, label, "Qdrant must belong to this local run")
	limits := performanceDockerValue(t, ctx, "inspect", "--format", "{{.HostConfig.NanoCpus}} {{.HostConfig.Memory}}", container)
	var cpu, memory int64
	_, err := fmt.Sscan(limits, &cpu, &memory)
	require.NoError(t, err)
	require.EqualValues(t, 2_000_000_000, cpu)
	require.EqualValues(t, 6<<30, memory)
	grpc := performanceDockerValue(t, ctx, "port", container, "6334/tcp")
	http := performanceDockerValue(t, ctx, "port", container, "6333/tcp")
	require.Equal(t, "http://"+grpc, os.Getenv("TOPIC_STREAM_TEST_QDRANT_URL"))
	require.Equal(t, "http://"+http, os.Getenv("TOPIC_STREAM_PERF_QDRANT_HTTP"))
}

func performanceDockerValue(t *testing.T, ctx context.Context, arguments ...string) string {
	t.Helper()
	body, err := exec.CommandContext(ctx, "docker", arguments...).Output()
	require.NoError(t, err, "inspect the runner-owned Qdrant resource")
	return strings.TrimSpace(string(body))
}
