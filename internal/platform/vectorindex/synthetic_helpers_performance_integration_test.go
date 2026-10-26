//go:build integration && performance

package vectorindex_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func syntheticRecallIDs(exact, approximate []string) float64 {
	truth := make(map[string]bool, len(exact))
	for _, id := range exact {
		truth[id] = true
	}
	if len(truth) == 0 {
		return 0
	}
	matched := map[string]bool{}
	for _, id := range approximate {
		if truth[id] {
			matched[id] = true
		}
	}
	return float64(len(matched)) / float64(len(truth))
}

func syntheticRecallTarget(values []float64) float64 {
	for _, target := range []float64{.99, .95} {
		for _, value := range values {
			if value >= target {
				return target
			}
		}
	}
	return 0
}

func syntheticOptimizationIdle(body []byte) (bool, error) {
	var envelope struct {
		Result struct {
			Summary *struct {
				Queued *int `json:"queued_optimizations"`
			} `json:"summary"`
			Running []json.RawMessage `json:"running"`
			Queued  []json.RawMessage `json:"queued"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false, err
	}
	result := envelope.Result
	if result.Summary == nil || result.Summary.Queued == nil || result.Running == nil || result.Queued == nil {
		return false, errors.New("optimizer response lacks observed summary/running/queued state")
	}
	return *result.Summary.Queued == 0 && len(result.Running) == 0 && len(result.Queued) == 0, nil
}

func syntheticHTTP(t *testing.T, endpoint, path string) json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+path, nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	return body
}
