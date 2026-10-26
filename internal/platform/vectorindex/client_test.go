package vectorindex

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestQdrantCallHasDeadlineAndHonorsEarlierCancellation(t *testing.T) {
	for _, earlier := range []bool{false, true} {
		ctx := t.Context()
		var stop context.CancelFunc
		if earlier {
			ctx, stop = context.WithTimeout(ctx, 10*time.Millisecond)
			defer stop()
		}
		failure := errors.New("RPC failed")
		err := boundedCall(ctx, "fixture", nil, nil, nil, func(callCtx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			deadline, bounded := callCtx.Deadline()
			require.True(t, bounded, "an RPC without a caller deadline must still be bounded")
			require.LessOrEqual(t, time.Until(deadline), time.Minute)
			if earlier {
				<-callCtx.Done()
				return callCtx.Err()
			}
			return failure
		})
		if earlier {
			require.ErrorIs(t, err, context.DeadlineExceeded)
		} else {
			require.ErrorIs(t, err, failure)
		}
	}
}

func TestParseClientConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		url    string
		host   string
		port   int
		useTLS bool
	}{
		{name: "local grpc", url: "http://127.0.0.1:6334", host: "127.0.0.1", port: 6334},
		{name: "tls grpc", url: "https://cluster.example.com:443", host: "cluster.example.com", port: 443, useTLS: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config, err := parseClientConfig(test.url)
			require.NoError(t, err)
			require.Equal(t, test.host, config.Host)
			require.Equal(t, test.port, config.Port)
			require.Equal(t, test.useTLS, config.UseTLS)
		})
	}
}

func TestParseClientConfigRejectsUnsupportedURLs(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"127.0.0.1:6334", "http://127.0.0.1", "ftp://127.0.0.1:6334", "http://127.0.0.1:6334/path"} {
		_, err := parseClientConfig(raw)
		require.Error(t, err, raw)
	}
}
