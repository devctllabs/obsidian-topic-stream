//go:build integration && performance

package postgres_test

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/testutil"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestDigestHistoryCapacity(t *testing.T) {
	require.NotEmpty(t, os.Getenv("TOPIC_STREAM_TEST_POSTGRES_DSN"))
	f := newFeedFixture(t)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Daily", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	sections, err := json.Marshal([]domain.DigestSection{{TopicID: uuid.New(), TopicName: "Research", Items: []domain.DigestItem{{Item: domain.Item{ID: uuid.New(), ContentMarkdown: strings.Repeat("Saved Markdown with an image ![alt](https://example.org/image.png)\n", 256)}}}}})
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `INSERT INTO jobs(id,workspace_id,kind,queue,status,payload)
 SELECT md5('generation-job-'||n)::uuid,$1,'generate-digest','generation','succeeded',jsonb_build_object('digestId',$2::text,'scheduledFor',NULL)
 FROM generate_series(1,10000) n`, f.workspaceID, definition.ID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO digest_issues(id,workspace_id,digest_id,generation_job_id,title,status,sections,generated_at)
SELECT md5('issue-'||n)::uuid,$1,$2,md5('generation-job-'||n)::uuid,'Saved '||n,'complete',$3::jsonb,$4::timestamptz-n*interval '1 second'
FROM generate_series(1,10000) n`, f.workspaceID, definition.ID, sections, f.now)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `ANALYZE digest_issues`)
	require.NoError(t, err)
	results := map[string]any{"issues": 10000, "snapshotBytes": len(sections)}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	started := time.Now()
	rows, err := conn.Query(t.Context(), `SELECT sections FROM digest_issues WHERE workspace_id=$1 ORDER BY generated_at DESC,id DESC`, f.workspaceID)
	require.NoError(t, err)
	var snapshots [][]domain.DigestSection
	for rows.Next() {
		var raw []byte
		require.NoError(t, rows.Scan(&raw))
		var decoded []domain.DigestSection
		require.NoError(t, json.Unmarshal(raw, &decoded))
		snapshots = append(snapshots, decoded)
	}
	require.NoError(t, rows.Err())
	require.Len(t, snapshots, 10000)
	results["beforeMilliseconds"] = float64(time.Since(started)) / float64(time.Millisecond)
	runtime.ReadMemStats(&after)
	results["beforeAllocatedBytes"] = after.TotalAlloc - before.TotalAlloc
	runtime.KeepAlive(snapshots)
	for _, variant := range []string{"first", "filtered", "deep"} {
		query := domain.DigestIssueList{WorkspaceID: f.workspaceID, Limit: 21}
		if variant == "filtered" {
			query.DigestID = &definition.ID
		}
		if variant == "deep" {
			query.Cursor = &domain.DigestIssueCursor{GeneratedAt: f.now.Add(-9000 * time.Second), ID: uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")}
		}
		var durations []float64
		var payloadBytes int
		runtime.ReadMemStats(&before)
		for range 120 {
			started = time.Now()
			page, readErr := f.store.ListDigestIssues(t.Context(), query)
			require.NoError(t, readErr)
			require.Len(t, page, 21)
			payload, encodeErr := json.Marshal(page)
			require.NoError(t, encodeErr)
			payloadBytes = len(payload)
			durations = append(durations, float64(time.Since(started))/float64(time.Millisecond))
		}
		runtime.ReadMemStats(&after)
		results[variant] = map[string]any{"latencyMilliseconds": testutil.Percentiles(durations), "allocatedBytesPerPage": (after.TotalAlloc - before.TotalAlloc) / 120, "payloadBytes": payloadBytes}
	}
	encoded, err := json.MarshalIndent(results, "", "  ")
	require.NoError(t, err)
	if path := os.Getenv("TOPIC_STREAM_PERF_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(path, append(encoded, '\n'), 0o600))
	}
	t.Log(string(encoded))
}
