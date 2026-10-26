package vectorindex

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
)

// NewClient validates an HTTP(S) gRPC URL and constructs a client without server I/O.
// Availability and schema checks belong to explicit operations; the caller owns Close.
func NewClient(rawURL string) (*qdrant.Client, error) {
	config, err := parseClientConfig(rawURL)
	if err != nil {
		return nil, err
	}
	config.GrpcOptions = append(config.GrpcOptions, grpc.WithChainUnaryInterceptor(boundedCall))
	client, err := qdrant.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("create Qdrant client: %w", err)
	}
	return client, nil
}

//nolint:revive // grpc.UnaryClientInterceptor defines this signature.
func boundedCall(ctx context.Context, method string, request, response any, connection *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
	callCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return invoke(callCtx, method, request, response, connection, options...)
}

func parseClientConfig(rawURL string) (*qdrant.Config, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("qdrant URL must include an HTTP(S) scheme, host, and gRPC port")
	}
	if err := validateClientURL(parsed); err != nil {
		return nil, err
	}
	port, err := parseGRPCPort(parsed.Port())
	if err != nil {
		return nil, err
	}
	return &qdrant.Config{Host: parsed.Hostname(), Port: port, UseTLS: parsed.Scheme == "https", SkipCompatibilityCheck: true}, nil
}

func validateClientURL(parsed *url.URL) error {
	if parsed.Hostname() == "" || parsed.Port() == "" {
		return errors.New("qdrant URL must include an HTTP(S) scheme, host, and gRPC port")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("qdrant URL must use HTTP or HTTPS")
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return errors.New("qdrant URL cannot include credentials, path, query, or fragment")
	}
	return nil
}

func parseGRPCPort(raw string) (int, error) {
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("qdrant URL has an invalid gRPC port")
	}
	return port, nil
}
