//go:build integration

package deps

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestManualOnlyDigestBuildHTTPRetainsIdentityWithoutAdvancingSchedule(t *testing.T) {
	f := newStableHTTPFixture(t)
	now := time.Now().UTC()
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Manual only", Timezone: "UTC", Version: 1,
		Topics: []domain.DigestTopic{{TopicID: f.topics[0], ItemLimit: 5}}, Recurrence: domain.DigestRecurrence{Frequency: domain.RecurrenceDaily, Hour: 12}, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	identity := uuid.NewString()
	var jobID uuid.UUID
	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/digests/"+definition.ID.String()+"/generate", nil)
		request.Header.Set("Idempotency-Key", identity)
		response := httptest.NewRecorder()
		f.runtime.server.Handler.ServeHTTP(response, request)
		require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
		var result serverhttp.JobAccepted
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		if jobID == uuid.Nil {
			jobID = result.JobId
		} else {
			require.Equal(t, jobID, result.JobId)
		}
	}
	saved, found, err := f.store.GetDigest(t.Context(), f.workspaceID, definition.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, saved.Enabled)
	require.Nil(t, saved.NextRunAt)
}

func TestDigestRetryHTTPReturnsPermanentSuccessor(t *testing.T) {
	f := newStableHTTPFixture(t)
	now := time.Now().UTC()
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Manual Retry", Timezone: "UTC", Version: 1,
		Topics: []domain.DigestTopic{{TopicID: f.topics[0], ItemLimit: 5}}, Recurrence: domain.DigestRecurrence{Frequency: domain.RecurrenceDaily, Hour: 12}, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	source, err := jobs.Prepare(jobs.EnqueueRequest{WorkspaceID: f.workspaceID,
		Definition: jobs.JobDefinition{Kind: "generate-digest", ArgumentsVersion: 1, Queue: domain.JobQueueGeneration,
			Retry: jobs.RetryPolicy{MaxAttempts: 3, BaseDelay: 30 * time.Second, MaxDelay: 15 * time.Minute}},
		Arguments: map[string]any{"digestId": definition.ID, "scheduledFor": nil}, IdempotencyKey: "generate-digest:" + definition.ID.String()}, now)
	require.NoError(t, err)
	source.Status = domain.JobStatusDead
	_, _, err = f.store.Jobs().Enqueue(t.Context(), source)
	require.NoError(t, err)
	var first serverhttp.JobAccepted
	f.exchange(t, apiExchange{method: http.MethodPost, path: "/api/v1/jobs/" + source.ID.String() + "/retry", status: 202}, &first)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET status='dead' WHERE id=$1`, first.JobId)
	require.NoError(t, err)
	var replay serverhttp.JobAccepted
	f.exchange(t, apiExchange{method: http.MethodPost, path: "/api/v1/jobs/" + source.ID.String() + "/retry", status: 202}, &replay)
	require.Equal(t, first.JobId, replay.JobId, "Retry of the original source must not create a second successor")
}

func TestDigestRetryHTTPRejectsSavedResultAfterLostAcknowledgement(t *testing.T) {
	f := newStableHTTPFixture(t)
	now := time.Now().UTC()
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Saved", Timezone: "UTC", Version: 1, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	jobID := uuid.New()
	_, err = conn.Exec(t.Context(), `INSERT INTO jobs(id,workspace_id,kind,queue,status,payload,idempotency_key)
 VALUES($1,$2,'generate-digest','generation','dead',jsonb_build_object('digestId',$3::text,'scheduledFor',NULL),'generate-digest:'||$3::text)`, jobID, f.workspaceID, definition.ID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO digest_issues(id,workspace_id,digest_id,generation_job_id,title,status,sections,generated_at)
 VALUES($1,$2,$3,$4,'Saved','complete','[]',$5)`, uuid.New(), f.workspaceID, definition.ID, jobID, now)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE digest_definitions SET last_successful_at=$2 WHERE id=$1`, definition.ID, now)
	require.NoError(t, err)
	f.exchange(t, apiExchange{method: http.MethodPost, path: "/api/v1/jobs/" + jobID.String() + "/retry", status: 409}, nil)
	var bindings int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM digest_build_requests WHERE workspace_id=$1 AND digest_id=$2 AND request_kind='retry'`, f.workspaceID, definition.ID).Scan(&bindings))
	require.Zero(t, bindings)
}
