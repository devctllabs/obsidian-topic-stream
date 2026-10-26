package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

const maxResponseBytes = 16 << 20

// OpenAI embeds text through an OpenAI-compatible endpoint and validates the configured vector dimensions.
type OpenAI struct {
	endpoint   string
	model      string
	dimensions int
	http       *http.Client
}

// NewOpenAI validates endpoint, model, and vector dimensions; a nil client uses http.DefaultClient.
func NewOpenAI(endpoint, model string, dimensions int, client *http.Client) (*OpenAI, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("embedding endpoint must be an HTTP URL")
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("embedding model is required")
	}
	if dimensions <= 0 {
		return nil, errors.New("embedding dimensions must be positive")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &OpenAI{
		endpoint: strings.TrimRight(endpoint, "/") + "/embeddings",
		model:    strings.TrimSpace(model), dimensions: dimensions, http: client,
	}, nil
}

// Embed posts one unchanged text and validates the returned vector.
func (c *OpenAI) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := c.requestBody(text)
	if err != nil {
		return nil, err
	}
	response, err := c.request(ctx, body)
	if err != nil {
		return nil, err
	}
	vectors, err := c.decode(response, 1, false)
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

func (c *OpenAI) requestBody(input any) ([]byte, error) {
	body, err := json.Marshal(struct {
		Input      any    `json:"input"`
		Model      string `json:"model"`
		Dimensions int    `json:"dimensions"`
	}{Input: input, Model: c.model, Dimensions: c.dimensions})
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}
	return body, nil
}

func (c *OpenAI) request(ctx context.Context, body []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request embedding: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return nil, embeddingStatusError(response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("embedding response exceeds size limit")
	}
	return data, nil
}

func embeddingStatusError(code int) error {
	switch code {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return fmt.Errorf("embedding provider returned status %d: %w", code, domain.ErrEmbeddingInputRejected)
	default:
		return fmt.Errorf("embedding provider returned status %d", code)
	}
}
