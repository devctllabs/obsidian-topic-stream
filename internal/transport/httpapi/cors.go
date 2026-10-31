package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

// NewCORSMiddleware builds the API's exact-origin browser access policy; an empty list disables it.
func NewCORSMiddleware(origins []string) (echo.MiddlewareFunc, error) {
	if len(origins) == 0 {
		return nil, nil
	}
	for _, origin := range origins {
		if strings.Contains(origin, "*") {
			return nil, errors.New("CORS origins must be exact; wildcards are not supported")
		}
	}
	return (middleware.CORSConfig{
		AllowOrigins:  origins,
		AllowMethods:  []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
		AllowHeaders:  []string{"Content-Type", "If-Match", "Idempotency-Key"},
		ExposeHeaders: []string{"ETag"},
	}).ToMiddleware()
}
