package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestUnknownErrorIsLoggedOnceWithoutSensitiveText(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	router := echo.New()
	router.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/example", nil)
	request.Header.Set("Authorization", "private-credential")
	ctx := router.NewContext(request, response)
	err := fmt.Errorf("read database: %w", errors.New("postgres://user:private-password@db/private-query"))

	handleHTTPError(ctx, err)
	handleHTTPError(ctx, err)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Equal(t, 1, strings.Count(logs.String(), "HTTP operation failed"))
	require.Contains(t, logs.String(), "error_type")
	require.NotContains(t, logs.String(), "private")
	require.NotContains(t, response.Body.String(), "private")
}
