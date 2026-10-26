//go:build integration

package testutil

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcqdrant "github.com/testcontainers/testcontainers-go/modules/qdrant"
	"github.com/testcontainers/testcontainers-go/wait"
)

// IntegrationConfig selects the services needed by one test package.
type IntegrationConfig struct {
	Postgres bool
	Qdrant   bool
}

var integration struct {
	postgresDSN string
	qdrantURL   string
	containers  []testcontainers.Container
}

// RunIntegration runs a package's tests against env-configured services or temporary containers.
// Call it from TestMain and pass its result to os.Exit after all cleanup has completed.
func RunIntegration(m *testing.M, config IntegrationConfig) (code int) {
	defer func() {
		if err := stopContainers(); err != nil {
			fmt.Fprintln(os.Stderr, "integration cleanup:", err)
			code = 1
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if config.Postgres {
		started := time.Now()
		if err := startPostgres(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "integration Postgres setup:", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "integration phase=postgres_setup elapsed_ms=%d\n", time.Since(started).Milliseconds())
	}
	if config.Qdrant {
		started := time.Now()
		if err := startQdrant(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "integration Qdrant setup:", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "integration phase=qdrant_setup elapsed_ms=%d\n", time.Since(started).Milliseconds())
	}
	cancel()
	return m.Run()
}

func startPostgres(ctx context.Context) error {
	integration.postgresDSN = os.Getenv("TOPIC_STREAM_TEST_POSTGRES_DSN")
	if integration.postgresDSN == "" {
		container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
			tcpostgres.WithDatabase("topic_stream"), tcpostgres.WithUsername("topic_stream"),
			tcpostgres.WithPassword("topic_stream"), tcpostgres.BasicWaitStrategies(),
		)
		if container != nil {
			integration.containers = append(integration.containers, container)
		}
		if err != nil {
			return err
		}
		integration.postgresDSN, err = container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			return err
		}
	}
	connection, err := pgx.Connect(ctx, integration.postgresDSN)
	if err != nil {
		return err
	}
	var version string
	err = connection.QueryRow(ctx, `SHOW server_version`).Scan(&version)
	if err == nil {
		fmt.Fprintf(os.Stderr, "integration service=postgres version=%s\n", version)
	}
	return errors.Join(err, connection.Close(ctx))
}

func startQdrant(ctx context.Context) error {
	integration.qdrantURL = os.Getenv("TOPIC_STREAM_TEST_QDRANT_URL")
	if integration.qdrantURL == "" {
		container, err := tcqdrant.Run(ctx, "qdrant/qdrant:v1.20",
			testcontainers.WithWaitStrategy(
				wait.ForListeningPort("6333/tcp").WithStartupTimeout(time.Minute),
				wait.ForListeningPort("6334/tcp").WithStartupTimeout(time.Minute),
			),
		)
		if container != nil {
			integration.containers = append(integration.containers, container)
		}
		if err != nil {
			return err
		}
		endpoint, err := container.GRPCEndpoint(ctx)
		if err != nil {
			return err
		}
		// gRPC resolves service-config TXT records for hostnames, including localhost.
		// A local container needs no external DNS lookup; keep remote Docker hosts intact.
		host, port, err := net.SplitHostPort(endpoint)
		if err != nil {
			return err
		}
		if host == "localhost" {
			endpoint = net.JoinHostPort("127.0.0.1", port)
		}
		integration.qdrantURL = "http://" + endpoint
	}
	client, err := vectorindex.NewClient(integration.qdrantURL)
	if err != nil {
		return err
	}
	health, err := client.HealthCheck(ctx)
	if err == nil {
		fmt.Fprintf(os.Stderr, "integration service=qdrant version=%s\n", health.GetVersion())
	}
	return errors.Join(err, client.Close())
}

func stopContainers() error {
	var result error
	for index := len(integration.containers) - 1; index >= 0; index-- {
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		result = errors.Join(result, integration.containers[index].Terminate(ctx))
		cancel()
		fmt.Fprintf(os.Stderr, "integration phase=container_cleanup elapsed_ms=%d\n", time.Since(started).Milliseconds())
	}
	return result
}

// QdrantURL returns the selected service's URL, pointing to its gRPC port.
func QdrantURL(t *testing.T) string {
	t.Helper()
	require.NotEmpty(t, integration.qdrantURL, "enable Qdrant in TestMain's IntegrationConfig")
	return integration.qdrantURL
}
