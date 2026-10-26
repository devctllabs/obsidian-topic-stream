//go:build integration

package deps

import (
	"encoding/json"
	"net/http"
)

func fixtureEmbeddingInputs(request *http.Request) ([]string, error) {
	var body struct {
		Input json.RawMessage `json:"input"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		return nil, err
	}
	if len(body.Input) > 0 && body.Input[0] == '"' {
		var text string
		if err := json.Unmarshal(body.Input, &text); err != nil {
			return nil, err
		}
		return []string{text}, nil
	}
	var texts []string
	err := json.Unmarshal(body.Input, &texts)
	return texts, err
}

func fixtureEmbeddingResponse(writer http.ResponseWriter, texts []string, vector []float32) {
	data := make([]map[string]any, len(texts))
	for i := range data {
		data[i] = map[string]any{"index": i, "embedding": vector}
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"data": data})
}
