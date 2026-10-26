package deps

import (
	"context"
	"net"
	"net/http"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	appconfig "github.com/ethernity/obsidian-topic-stream/internal/platform/config"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type policyRecordingServer struct {
	qdrant.UnimplementedPointsServer
	qdrant.UnimplementedQdrantServer
	requests chan *qdrant.QueryPoints
}

func (s *policyRecordingServer) HealthCheck(context.Context, *qdrant.HealthCheckRequest) (*qdrant.HealthCheckReply, error) {
	return &qdrant.HealthCheckReply{Version: "1.19.1"}, nil
}

func (s *policyRecordingServer) Query(_ context.Context, request *qdrant.QueryPoints) (*qdrant.QueryResponse, error) {
	s.requests <- request
	return &qdrant.QueryResponse{}, nil
}

func TestRetrievalFactoryUsesLoadedOperatorPolicy(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg, err := appconfig.Load(t.Context())
	require.NoError(t, err)
	policy, err := qdrantSearchPolicy(cfg.Runtime)
	require.NoError(t, err)
	require.Equal(t, uint64(100000), policy.FullScanThresholdKB)
	require.Equal(t, uint64(256), policy.HNSWEf)
	require.True(t, policy.Acorn)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	probe := &policyRecordingServer{requests: make(chan *qdrant.QueryPoints, 1)}
	server := grpc.NewServer()
	qdrant.RegisterPointsServer(server, probe)
	qdrant.RegisterQdrantServer(server, probe)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); require.NoError(t, <-done) })
	client, err := vectorindex.NewClient("http://" + listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	generation := domain.RetrievalIndex{Collection: "policy"}
	generation.Embedding.Endpoint = "http://fixture.invalid/v1"
	generation.Embedding.Model = "fixture"
	generation.Embedding.Dimensions = 3
	factory := retrievalFactory{qdrant: client, http: http.DefaultClient, policy: policy}
	_, index, err := factory.Build(generation)
	require.NoError(t, err)
	_, err = index.QueryDense(t.Context(), []float32{1, 0, 0}, vectorindex.Filter{WorkspaceID: uuid.New()}, 20)
	require.NoError(t, err)
	request := <-probe.requests
	require.NotNil(t, request.Params)
	require.Nil(t, request.Params.Exact)
	require.Equal(t, uint64(256), request.Params.GetHnswEf())
	require.True(t, request.Params.GetAcorn().GetEnable())
	require.Equal(t, .4, request.Params.GetAcorn().GetMaxSelectivity())
}

func TestQdrantOperatorRollbackPreservesExplicitZeroAndFalse(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("TOPIC_STREAM_QDRANT_FULL_SCAN_THRESHOLD_KB", "0")
	t.Setenv("TOPIC_STREAM_QDRANT_HNSW_EF", "0")
	t.Setenv("TOPIC_STREAM_QDRANT_ACORN_ENABLED", "false")
	t.Setenv("TOPIC_STREAM_QDRANT_ACORN_MAX_SELECTIVITY_PERCENT", "0")
	cfg, err := appconfig.Load(t.Context())
	require.NoError(t, err)
	policy, err := qdrantSearchPolicy(cfg.Runtime)
	require.NoError(t, err)
	require.Equal(t, vectorindex.SearchPolicy{}, policy)
	require.False(t, cfg.Runtime.QdrantAcornEnabled)
}
