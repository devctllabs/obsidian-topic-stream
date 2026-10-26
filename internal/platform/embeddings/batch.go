package embeddings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

const maxBatchInputs = 128
const maxBatchRequestBytes = 4 << 20

// EmbedBatch sends up to 128 unchanged texts in a request of at most 4 MiB and restores input
// order. Empty input does no I/O. A rejected response returns no partial vectors or automatic retry.
func (c *OpenAI) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if err := validateBatchTexts(texts); err != nil {
		return nil, err
	}
	body, err := c.requestBody(texts)
	if err != nil {
		return nil, err
	}
	if len(body) > maxBatchRequestBytes {
		return nil, fmt.Errorf("encoded request size: %w", domain.ErrEmbeddingBatchLimit)
	}
	response, err := c.request(ctx, body)
	if err != nil {
		return nil, err
	}
	return c.decode(response, len(texts), true)
}

func validateBatchTexts(texts []string) error {
	if len(texts) > maxBatchInputs {
		return fmt.Errorf("input count exceeds %d: %w", maxBatchInputs, domain.ErrEmbeddingBatchLimit)
	}
	bytes := 0
	for _, text := range texts {
		if len(text) > maxBatchRequestBytes-bytes {
			return fmt.Errorf("input size: %w", domain.ErrEmbeddingBatchLimit)
		}
		bytes += len(text)
	}
	return nil
}

type embeddingVector struct {
	Index     *int      `json:"index"`
	Embedding []float32 `json:"embedding"`
}

func (c *OpenAI) decode(body []byte, count int, requireIndex bool) ([][]float32, error) {
	var response struct {
		Data []embeddingVector `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, errors.New("invalid embedding response JSON")
	}
	if len(response.Data) != count {
		return nil, fmt.Errorf("embedding provider returned %d vectors, expected %d", len(response.Data), count)
	}
	ordered := make([][]float32, count)
	for _, entry := range response.Data {
		position, err := embeddingPosition(entry.Index, count, requireIndex)
		if err != nil {
			return nil, err
		}
		if ordered[position] != nil {
			return nil, errors.New("embedding response repeats an input index")
		}
		if err := c.validateVector(entry.Embedding); err != nil {
			return nil, err
		}
		ordered[position] = entry.Embedding
	}
	return ordered, nil
}

func embeddingPosition(index *int, count int, required bool) (int, error) {
	if index == nil {
		if required {
			return 0, errors.New("embedding response is missing an input index")
		}
		return 0, nil
	}
	if *index < 0 || *index >= count {
		return 0, errors.New("embedding response input index is out of range")
	}
	return *index, nil
}

func (c *OpenAI) validateVector(vector []float32) error {
	if len(vector) != c.dimensions {
		return fmt.Errorf("embedding provider returned %d dimensions, expected %d", len(vector), c.dimensions)
	}
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return errors.New("embedding response contains a non-finite vector")
		}
	}
	return nil
}
