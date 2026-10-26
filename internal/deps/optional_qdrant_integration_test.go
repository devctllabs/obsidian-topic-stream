//go:build integration

package deps

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestReadyModelDatabaseAPIConstructionDoesNotWaitForOptionalQdrantIO(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	var embeddingCalls atomic.Int64
	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		embeddingCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0,0]}]}`))
	}))
	t.Cleanup(embeddingServer.Close)
	store, err := postgres.Open(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	descriptor := testutil.DefaultEmbeddingDescriptor()
	descriptor.Endpoint = embeddingServer.URL
	require.NoError(t, store.EnsureDefaultEmbeddingModel(t.Context(), descriptor, time.Now()))
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE embedding_models SET initialization_status='ready' WHERE id=$1`, domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	require.NoError(t, conn.Close(t.Context()))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var calls atomic.Int64
	server := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		calls.Add(1)
		<-stream.Context().Done()
		return stream.Context().Err()
	}))
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); require.NoError(t, testutil.Receive(t, served)) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type result struct {
		runtime *API
		err     error
	}
	finished := make(chan result, 1)
	go func() {
		runtime, buildErr := NewAPI(ctx, generated.Config{
			Bootstrap: generated.BootstrapConfig{BootstrapEmbeddingEndpoint: embeddingServer.URL, BootstrapEmbeddingModel: "synthetic", BootstrapEmbeddingDimensions: 3},
			DBPrimary: generated.DBPrimaryConfig{Kind: "postgres", PostgresDSN: dsn},
			HTTP:      generated.HTTPConfig{Enabled: true, Address: "127.0.0.1:0"},
			Runtime: generated.RuntimeConfig{QdrantFullScanThresholdKb: 100000, QdrantHnswEf: 256, QdrantAcornEnabled: true, QdrantAcornMaxSelectivityPercent: 40, WorkspaceId: uuid.NewString(), QdrantUrl: "http://" + listener.Addr().String(), ShutdownTimeout: 5 * time.Second,
				EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1},
		}, "optional-qdrant")
		finished <- result{runtime, buildErr}
	}()
	timedOut := false
	var built result
	select {
	case built = <-finished:
	case <-time.After(time.Second):
		timedOut = true
		cancel()
		server.Stop()
		built = testutil.Receive(t, finished)
	}
	if built.runtime != nil {
		t.Cleanup(func() { require.NoError(t, built.runtime.Shutdown(context.Background())) })
	}
	require.False(t, timedOut, "constructing the database API must not perform optional Qdrant I/O")
	require.NoError(t, built.err)
	require.Zero(t, calls.Load())
	require.Zero(t, embeddingCalls.Load(), "ready synthetic models must not be probed again")
	for _, path := range []string{"/api/v1/feed?limit=20", "/api/v1/jobs?limit=20"} {
		response := httptest.NewRecorder()
		built.runtime.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}
}
