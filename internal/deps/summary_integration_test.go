//go:build integration

package deps

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devctllabs/go-libs/di"
	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestSummaryHTTPWorkerGeneratesOnlyExplicitlyAndReadsReadyAfterOptOut(t *testing.T) {
	f := newStableAPIFixture(t)
	var calls atomic.Int64
	started, release := make(chan struct{}), make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Equal(t, "independent-summary-model", request.Model)
		require.Contains(t, request.Messages[1].Content, "Synthetic systems article")
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"text":"A concise systems summary."}`}}}}))
	}))
	t.Cleanup(provider.Close)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	settingsService, err := di.Resolve[*settings.Service](f.runtime.container)
	require.NoError(t, err)
	current, err := settingsService.GetWorkspace(t.Context())
	require.NoError(t, err)
	endpoint, model, rerankModel := provider.URL+"/v1", "independent-summary-model", "separate-rerank-model"
	current, err = settingsService.UpdateWorkspace(t.Context(), current.Version, domain.WorkspaceSettingsPatch{Generation: &domain.GenerationSettings{Enabled: true, Endpoint: &endpoint, SummaryModel: &model, RerankModel: &rerankModel}})
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	var itemID uuid.UUID
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT id FROM items WHERE workspace_id=$1 ORDER BY id LIMIT 1`, f.workspaceID).Scan(&itemID))
	_, err = conn.Exec(t.Context(), `UPDATE items SET content_origin='article_body' WHERE workspace_id=$1 AND id=$2`, f.workspaceID, itemID)
	require.NoError(t, err)
	path := "/api/v1/items/" + itemID.String() + "/ai-summary"
	var state serverhttp.ItemSummary
	f.exchange(t, apiExchange{method: http.MethodGet, path: path, status: 200}, &state)
	require.Equal(t, serverhttp.ItemSummaryState("not_requested"), state.State)
	f.prepare(t)
	f.feed(t, http.MethodPost, "/api/v1/feed/refresh")
	require.Zero(t, calls.Load(), "settings, GET, Feed preparation and Refresh must not generate")
	var first, replay serverhttp.JobAccepted
	f.exchange(t, apiExchange{method: http.MethodPost, path: path, status: 202}, &first)
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("summary worker did not reach provider")
	}
	f.exchange(t, apiExchange{method: http.MethodPost, path: path, status: 202}, &replay)
	require.Equal(t, first.JobId, replay.JobId)
	f.exchange(t, apiExchange{method: http.MethodGet, path: path, status: 200}, &state)
	require.Equal(t, serverhttp.ItemSummaryState("pending"), state.State)
	close(release)
	require.Eventually(t, func() bool {
		f.exchange(t, apiExchange{method: http.MethodGet, path: path, status: 200}, &state)
		return state.Result != nil
	}, 10*time.Second, 20*time.Millisecond)
	require.Equal(t, "A concise systems summary.", state.Result.Text)
	require.Equal(t, first.JobId, state.Result.GenerationJobId)
	_, err = settingsService.UpdateWorkspace(t.Context(), current.Version, domain.WorkspaceSettingsPatch{Generation: &domain.GenerationSettings{Enabled: false}})
	require.NoError(t, err)
	f.exchange(t, apiExchange{method: http.MethodGet, path: path, status: 200}, &state)
	require.False(t, state.CanGenerate)
	require.NotNil(t, state.Result)
	f.exchange(t, apiExchange{method: http.MethodPost, path: path, status: 409}, nil)
	require.EqualValues(t, 1, calls.Load())
}
