package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/service/systemstatus"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGetSystemStatus(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.MustParse("018fda3a-4e36-7d1d-8835-45165d3f3596")
	now := time.Date(2026, time.September, 27, 13, 0, 0, 0, time.UTC)
	status := systemstatus.New(systemstatus.Config{
		Version: "0.1.0-test", WorkspaceID: workspaceID, Now: func() time.Time { return now },
		Components: []systemstatus.Component{{Name: "postgres", Required: true, Check: func(context.Context) error { return nil }}},
	})
	router := newTestRouter(t, httpapi.Dependencies{Status: &status})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))

	var body serverhttp.SystemStatus
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	require.Equal(t, serverhttp.SystemStatusStatusReady, body.Status)
	require.Equal(t, workspaceID, body.WorkspaceId)
	require.Equal(t, "0.1.0-test", body.Version)
	require.Equal(t, now, body.Now)
	require.Equal(t, []serverhttp.ComponentStatus{{Name: "postgres", Status: serverhttp.ComponentStatusStatusReady}}, body.Components)
}
