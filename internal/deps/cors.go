package deps

import (
	"strings"

	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/labstack/echo/v5"
)

func runtimeCORS(cfg generated.HTTPConfig) (echo.MiddlewareFunc, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	var origins []string
	for _, value := range strings.Split(cfg.CorsAllowedOrigins, ",") {
		if origin := strings.TrimSpace(value); origin != "" {
			origins = append(origins, origin)
		}
	}
	return httpapi.NewCORSMiddleware(origins)
}
