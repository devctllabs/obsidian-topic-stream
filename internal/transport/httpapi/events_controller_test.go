package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEventRouteStreamsStructuredAdvisoryEvents(t *testing.T) {
	service := events.NewForWorkspace(uuid.Nil, nil)
	router := newTestRouter(t, httpapi.Dependencies{Events: service})
	requestContext, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(requestContext)
	response := httptest.NewRecorder()
	itemID := uuid.New()
	jobID := uuid.New()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-requestContext.Done():
				return
			case <-ticker.C:
				service.InvalidateFeed(uuid.New(), itemID)
				service.NotifyJobChanged(uuid.New(), jobID)
			}
		}
	}()

	router.ServeHTTP(response, request)
	testutil.Receive(t, done)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "text/event-stream", response.Header().Get("Content-Type"))
	require.Equal(t, "no-cache", response.Header().Get("Cache-Control"))
	require.True(t, strings.Contains(response.Body.String(), "event: feedInvalidated\n"), response.Body.String())
	require.True(t, strings.Contains(response.Body.String(), itemID.String()), response.Body.String())
	require.True(t, strings.Contains(response.Body.String(), "event: jobChanged\n"), response.Body.String())
	require.True(t, strings.Contains(response.Body.String(), jobID.String()), response.Body.String())
}

func TestEventRouteFlushesConnectionAndStopsOnCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	router := newTestRouter(t, httpapi.Dependencies{})
	response := &eventResponseWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, ": connected\n\n", response.Body.String())
	require.Equal(t, 1, response.flushes)
	require.Equal(t, "keep-alive", response.Header().Get("Connection"))
}

func TestEventRouteDoesNotWriteProblemAfterStreamFailure(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	response := &eventResponseWriter{
		ResponseRecorder: httptest.NewRecorder(), writeErr: errors.New("client disconnected"),
	}
	router := newTestRouter(t, httpapi.Dependencies{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "text/event-stream", response.Header().Get("Content-Type"))
	require.Equal(t, 1, response.writes)
	require.NotContains(t, response.Body.String(), "problem")
}

type eventResponseWriter struct {
	*httptest.ResponseRecorder
	cancel   context.CancelFunc
	writeErr error
	writes   int
	flushes  int
}

// Write records attempts to write after a simulated client disconnection.
func (w *eventResponseWriter) Write(data []byte) (int, error) {
	w.writes++
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.ResponseRecorder.Write(data)
}

// FlushError records the first flush and optionally cancels the request.
func (w *eventResponseWriter) FlushError() error {
	w.flushes++
	if w.cancel != nil {
		w.cancel()
	}
	return nil
}
