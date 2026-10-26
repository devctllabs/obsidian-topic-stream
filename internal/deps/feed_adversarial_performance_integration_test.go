//go:build integration && performance

package deps

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestFeedAdversarialCapacity(t *testing.T) {
	require.NotEmpty(t, os.Getenv("TOPIC_STREAM_TEST_POSTGRES_DSN"))
	f, conn, sourceID := newPerformanceFixture(t)
	_, err := conn.Exec(t.Context(), `INSERT INTO topics(id,workspace_id,name,include_text)
SELECT md5('topic:'||n)::uuid,$1,'Topic '||n,'' FROM generate_series(1,500) n`, f.workspaceID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE feed_publications p SET matched_topic_ids=ARRAY[md5('topic:'||(2+seed.n%499))::uuid]||
CASE WHEN seed.n%2=0 THEN ARRAY[md5('topic:1')::uuid] ELSE '{}'::uuid[] END
FROM perf_seed seed WHERE p.item_id=seed.id`)
	require.NoError(t, err)
	probe := os.Getenv("TOPIC_STREAM_PERF_PROBE") != ""
	var results []map[string]any
	for _, scenario := range []string{"hidden-prefix", "wrong-prefix", "manual-add"} {
		seedAdversarialScenario(t, f, conn, scenario)
		for _, topicNumber := range []int{0, 1, 498, 500, 499} {
			if scenario != "manual-add" && topicNumber > 1 {
				continue
			}
			for _, before := range []int64{1000001, 10000} {
				result := measureAdversarialPage(t, f, conn, adversarialCase{scenario: scenario, topic: topicNumber, before: before, probe: probe, sourceID: sourceID})
				if result != nil {
					results = append(results, result)
					saveAdversarialResults(t, results)
				}
			}
		}
	}

}

func adversarialRoute(t *testing.T, workspaceID uuid.UUID, topicID *uuid.UUID, before int64) string {
	t.Helper()
	snapshot := domain.FeedSnapshot{Version: 1, WorkspaceID: workspaceID, TopicID: topicID, HeadOrdinal: 1000000, CreatedAt: time.Now().UTC()}
	encode := func(kind string, after int64) string {
		payload, err := json.Marshal(struct {
			Kind     string
			Snapshot domain.FeedSnapshot
			After    int64
		}{kind, snapshot, after})
		require.NoError(t, err)
		return base64.RawURLEncoding.EncodeToString(payload)
	}
	route := "/api/v1/feed?limit=25&snapshot=" + encode("snapshot", 0)
	if before <= 1000000 {
		route += "&cursor=" + encode("cursor", before)
	}
	if topicID != nil {
		route += "&topicId=" + topicID.String()
	}
	return route
}

func adversarialOracle(scenario string, topic, before int64) []uuid.UUID {
	var ids []uuid.UUID
	for n := min(before-1, 1000000); n >= 1 && len(ids) < 25; n-- {
		if scenario != "manual-add" && n > 100000 {
			continue
		}
		if !adversarialMember(scenario, topic, n) {
			continue
		}
		ids = append(ids, uuid.UUID(md5.Sum([]byte("item:"+fmt.Sprint(n)))))
	}
	return ids
}

// Read the literal from the repository source so EXPLAIN measures the production key query.
func feedKeysSQL(t *testing.T, topic bool) string {
	t.Helper()
	name := "allFeedKeysSQL"
	if topic {
		name = "topicFeedKeysSQL"
	}
	file, err := parser.ParseFile(token.NewFileSet(), "../repository/postgres/feed_read.go", nil, 0)
	require.NoError(t, err)
	var statement string
	ast.Inspect(file, func(node ast.Node) bool {
		value, ok := node.(*ast.ValueSpec)
		if ok && value.Names[0].Name == name {
			literal := value.Values[0].(*ast.BasicLit)
			statement, err = strconv.Unquote(literal.Value)
		}
		return true
	})
	require.NoError(t, err)
	require.NotEmpty(t, statement)
	return statement + " SELECT * FROM selected"
}

func seedAdversarialScenario(t *testing.T, f stableAPIFixture, conn *pgx.Conn, scenario string) {
	t.Helper()
	_, err := conn.Exec(t.Context(), `TRUNCATE item_state,topic_corrections`)
	require.NoError(t, err)
	switch scenario {
	case "hidden-prefix":
		_, err = conn.Exec(t.Context(), `INSERT INTO item_state(workspace_id,item_id,visibility)
SELECT $1,id,'hidden' FROM perf_seed WHERE n>100000`, f.workspaceID)
	case "wrong-prefix":
		_, err = conn.Exec(t.Context(), `INSERT INTO topic_corrections(topic_id,item_id,membership,updated_at)
SELECT topic_id,p.item_id,'exclude',now() FROM feed_publications p JOIN perf_seed seed ON seed.id=p.item_id
CROSS JOIN LATERAL unnest(p.matched_topic_ids) topic_id WHERE seed.n>100000`)
	default:
		_, err = conn.Exec(t.Context(), `INSERT INTO topic_corrections(topic_id,item_id,membership,updated_at)
SELECT md5('topic:498')::uuid,id,'include',now() FROM perf_seed WHERE n%2=0;
UPDATE topics SET enabled=false WHERE id=md5('topic:500')::uuid;
DELETE FROM topics WHERE id=md5('topic:499')::uuid`)
	}
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `TRUNCATE feed_display_entries`)
	require.NoError(t, err)
	// Direct SQL fixtures seed the positive set from the independent numbered-data rules.
	_, err = conn.Exec(t.Context(), `INSERT INTO feed_display_entries(workspace_id,item_id,ordinal,topic_ids)
SELECT p.workspace_id,p.item_id,p.ordinal,p.matched_topic_ids||
CASE WHEN $2='manual-add' AND seed.n%2=0 THEN ARRAY[md5('topic:498')::uuid] ELSE '{}'::uuid[] END
FROM feed_publications p JOIN perf_seed seed ON seed.id=p.item_id
WHERE p.workspace_id=$1 AND ($2='manual-add' OR seed.n<=100000)`, f.workspaceID, scenario)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `ANALYZE`)
	require.NoError(t, err)
}

type adversarialCase struct {
	scenario string
	topic    int
	before   int64
	probe    bool
	sourceID uuid.UUID
}

func measureAdversarialPage(t *testing.T, f stableAPIFixture, conn *pgx.Conn, c adversarialCase) map[string]any {
	t.Helper()
	scenario, topicNumber, before, probe, sourceID := c.scenario, c.topic, c.before, c.probe, c.sourceID
	var topicID *uuid.UUID
	if topicNumber > 0 {
		id := uuid.UUID(md5.Sum([]byte("topic:" + fmt.Sprint(topicNumber))))
		topicID = &id
	}
	route := adversarialRoute(t, f.workspaceID, topicID, before)
	response := httptest.NewRecorder()
	started := time.Now()
	f.runtime.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
	if scenario == "manual-add" && (topicNumber == 499 || topicNumber == 500) {
		require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
		return nil
	}
	firstMS := float64(time.Since(started)) / float64(time.Millisecond)
	require.Equal(t, 200, response.Code, response.Body.String())
	var page serverhttp.FeedPage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	var ids []uuid.UUID
	for _, entry := range page.Items {
		ids = append(ids, entry.Item.Id)
	}
	require.Equal(t, adversarialOracle(scenario, int64(topicNumber), before), ids)
	args := []any{f.workspaceID, int64(1000000), before, 25}
	if topicID != nil {
		args = []any{f.workspaceID, int64(1000000), before, *topicID, 25}
	}
	var plan json.RawMessage
	require.NoError(t, conn.QueryRow(t.Context(), "EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) "+feedKeysSQL(t, topicID != nil), args...).Scan(&plan))
	result := map[string]any{"scenario": scenario, "topic": topicNumber, "before": before,
		"firstMilliseconds": firstMS, "plan": plan}
	if !probe {
		stop := performanceBackground(t, f.store, f.workspaceID, sourceID)
		measurement := measurePerformanceRoute(t, f.runtime.server.Handler, route)
		stop()
		result["http"] = measurement
		require.LessOrEqual(t, measurement.P95, 200.0)
	}
	t.Logf("%s topic=%d before=%d first=%.2f ms", scenario, topicNumber, before, firstMS)
	return result
}

func adversarialMember(scenario string, topic, n int64) bool {
	if scenario == "manual-add" && (topic == 499 || topic == 500) {
		return false
	}
	if topic == 0 {
		return true
	}
	return topic == 1 && n%2 == 0 || topic == 2+n%499 || scenario == "manual-add" && topic == 498 && n%2 == 0
}

func saveAdversarialResults(t *testing.T, results []map[string]any) {
	t.Helper()
	encoded, err := json.MarshalIndent(results, "", "  ")
	require.NoError(t, err)
	if path := os.Getenv("TOPIC_STREAM_PERF_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(path, append(encoded, '\n'), 0o600))
	}
}
