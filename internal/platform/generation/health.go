package generation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// HealthCheck requests the provider's models endpoint and requires a successful HTTP status.
func HealthCheck(ctx context.Context, endpoint string, client *http.Client) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("generation endpoint must be an HTTP URL")
	}
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		strings.TrimRight(endpoint, "/")+"/models",
		nil,
	)
	if err != nil {
		return fmt.Errorf("create generation health request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request generation health: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("generation provider returned status %d", response.StatusCode)
	}
	return nil
}
