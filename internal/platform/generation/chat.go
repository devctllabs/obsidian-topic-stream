package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

func (c *OpenAI) complete(ctx context.Context, body []byte) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create generation request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("request generation: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return "", fmt.Errorf("generation provider returned status %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("read generation response: %w", err)
	}
	if len(payload) > maxResponseBytes {
		return "", errors.New("generation response exceeded the size limit")
	}
	var result chatResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		return "", errors.New("generation provider returned invalid JSON")
	}
	if len(result.Choices) != 1 {
		return "", fmt.Errorf("generation provider returned %d choices, expected one", len(result.Choices))
	}
	return result.Choices[0].Message.Content, nil
}
