//go:build integration

package feedpreparation_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feedpreparation"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunIntegration(m, testutil.IntegrationConfig{Postgres: true}))
}

type fixedPreparer struct{ ids []uuid.UUID }

// PrepareFeed returns synthetic prepared candidates without running retrieval.
func (p fixedPreparer) PrepareFeed(_ context.Context, r domain.FeedTopicPreparation) (domain.FeedTopicPrepared, error) {
	result := domain.FeedTopicPrepared{Inputs: r.Inputs, Candidates: []domain.FeedCandidate{}}
	result.Inputs.GateVersion = "synthetic-gate"
	for _, id := range p.ids[:min(len(p.ids), r.Settings.Retrieval.CandidateLimit)] {
		result.Candidates = append(result.Candidates, domain.FeedCandidate{ItemID: id})
	}
	return result, nil
}

func TestFiftyTopicsContinueThroughSuccessfulBoundedJobs(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := postgres.Open(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	workspaceID, sourceID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	old := now.Add(-2 * time.Minute)
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	require.NoError(t, store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), now))
	_, err = store.GetOrCreateActive(t.Context(), workspaceID, domain.DefaultEmbeddingModelID, now)
	require.NoError(t, err)
	require.NoError(t, store.Create(t.Context(), domain.Source{ID: sourceID, WorkspaceID: workspaceID, Name: "Synthetic", Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://fixture.example/feed"}, Enabled: true, PollIntervalMinutes: 60, CreatedAt: old, UpdatedAt: old}))
	for range 50 {
		require.NoError(t, store.CreateTopic(t.Context(), domain.Topic{ID: uuid.New(), WorkspaceID: workspaceID, Name: uuid.NewString(), Mode: domain.TopicModeOrdinary, Revision: 1, Enabled: true, CreatedAt: old, UpdatedAt: old}))
	}
	ids := make([]uuid.UUID, 0, 50)
	for range 50 {
		id := uuid.New()
		url := "https://fixture.example/" + id.String()
		commit, _, err := store.UpsertItem(t.Context(), domain.Item{ID: id, WorkspaceID: workspaceID, Kind: domain.ItemKindArticle, Title: "Synthetic", URL: url, CanonicalURL: url, DiscoveredAt: old, Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{Description: "Fixture"}}}, domain.SourceEntry{SourceID: sourceID, ExternalID: id.String(), URL: url, DiscoveredAt: old, UpdatedAt: old, Details: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{Description: "Fixture"}}})
		require.NoError(t, err)
		_, err = store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{WorkspaceID: workspaceID, ItemID: id, ModelID: domain.DefaultEmbeddingModelID, Revision: commit.ProjectionRevision})
		require.NoError(t, err)
		ids = append(ids, id)
	}
	defaults := settings.DefaultWorkspaceSettings(now)
	_, err = store.GetOrCreateWorkspaceSettings(t.Context(), workspaceID, defaults)
	require.NoError(t, err)
	config := jobs.DefaultConfig()
	config.PollInterval = time.Millisecond
	queue := jobs.New(store.Jobs(), func() time.Time { return now }, config)
	feedpreparation.New(feedpreparation.Config{WorkspaceID: workspaceID, Repository: store, Settings: storedPreparationSettings{store: store, workspaceID: workspaceID}, Preparer: fixedPreparer{ids: ids}, Jobs: queue.ForWorkspace(workspaceID), Now: func() time.Time { return now }, GateVersion: "synthetic-gate"})
	reconciler := feedpreparation.NewReconciler(store, queue, func() time.Time { return now }, "synthetic-gate")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- queue.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, testutil.Receive(t, done)) })
	for batch := 1; batch <= 7; batch++ {
		require.NoError(t, reconciler.Reconcile(t.Context()))
		waitPreparationJobs(t, store, workspaceID, batch)
	}
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(context.Background())) }()
	var references, ready int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM topic_candidates WHERE workspace_id=$1`, workspaceID).Scan(&references))
	require.Equal(t, 2500, references)
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM topic_candidate_state WHERE workspace_id=$1 AND ready_version=requested_version`, workspaceID).Scan(&ready))
	require.Equal(t, 50, ready)
	head, err := store.FeedHead(t.Context(), workspaceID)
	require.NoError(t, err)
	require.Zero(t, head)
	defaults.Retrieval.CandidateLimit = 20
	defaults.Version++
	defaults.UpdatedAt = old
	_, changed, err := store.UpdateWorkspaceSettings(t.Context(), workspaceID, 1, defaults)
	require.NoError(t, err)
	require.True(t, changed)
	stale, err := store.ReadFeedCandidates(t.Context(), domain.FeedCandidateQuery{WorkspaceID: workspaceID, At: now, CandidateLimit: 20})
	require.NoError(t, err)
	require.Empty(t, stale, "the old limit is incompatible immediately")
	for batch := 8; batch <= 14; batch++ {
		require.NoError(t, reconciler.Reconcile(t.Context()))
		waitPreparationJobs(t, store, workspaceID, batch)
	}
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM topic_candidates WHERE workspace_id=$1`, workspaceID).Scan(&references))
	require.Equal(t, 1000, references)

}

type storedPreparationSettings struct {
	store       *postgres.Store
	workspaceID uuid.UUID
}

// GetWorkspace returns the fixture's Workspace Settings snapshot.
func (s storedPreparationSettings) GetWorkspace(ctx context.Context) (domain.WorkspaceSettings, error) {
	value, _, err := s.store.GetWorkspaceSettings(ctx, s.workspaceID)
	return value, err
}

func waitPreparationJobs(t *testing.T, store *postgres.Store, workspaceID uuid.UUID, count int) {
	t.Helper()
	require.Eventually(t, func() bool {
		list, err := store.Jobs().List(t.Context(), domain.JobList{WorkspaceID: workspaceID, Limit: 100})
		if err != nil || len(list) != count {
			return false
		}
		for _, job := range list {
			if job.Status != domain.JobStatusSucceeded {
				return false
			}
		}
		return true
	}, 5*time.Second, 5*time.Millisecond)
}
