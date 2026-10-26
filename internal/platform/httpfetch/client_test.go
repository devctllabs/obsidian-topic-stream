package httpfetch_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/platform/httpfetch"
	"github.com/stretchr/testify/require"
)

func TestSharedHostLimiterBoundsConcurrentRequestsUntilBodiesClose(t *testing.T) {
	t.Parallel()

	var active atomic.Int32
	var maximum atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		maximum.Store(max(maximum.Load(), current))
		<-release
		_, _ = io.WriteString(response, "ok")
	}))
	defer server.Close()
	limiter, err := httpfetch.NewHostLimiter(1)
	require.NoError(t, err)
	client := httpfetch.NewClientWithLimiter(true, limiter)
	var group sync.WaitGroup
	group.Add(2)
	for range 2 {
		go func() {
			defer group.Done()
			response, getErr := client.Get(server.URL)
			if getErr == nil {
				_ = response.Body.Close()
			}
		}()
	}
	require.Eventually(t, func() bool { return active.Load() == 1 }, time.Second, time.Millisecond)
	require.Equal(t, int32(1), maximum.Load())
	close(release)
	group.Wait()
	require.Equal(t, int32(1), maximum.Load())
}

func TestClientReturnsTypedRetryAfterWithoutSleeping(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Retry-After", "120")
		response.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	started := time.Now()
	_, err := httpfetch.NewClient(true).Get(server.URL)

	require.Error(t, err)
	var retryAfter httpfetch.RetryAfterError
	require.True(t, errors.As(err, &retryAfter))
	require.Equal(t, 2*time.Minute, retryAfter.Delay())
	require.Less(t, time.Since(started), time.Second)
}

func TestClientBlocksPrivateAddressesUnlessExplicitlyAllowed(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, "ok")
	}))
	defer server.Close()

	_, err := httpfetch.NewClient(false).Get(server.URL)
	require.ErrorContains(t, err, "private or special-use")
	response, err := httpfetch.NewClient(true).Get(server.URL)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, http.StatusOK, response.StatusCode)
}

func TestClientRejectsCredentialsBeforeDial(t *testing.T) {
	t.Parallel()
	_, err := httpfetch.NewClient(true).Get("http://user:secret@example.invalid/feed")
	require.ErrorContains(t, err, "must not contain credentials")
}
