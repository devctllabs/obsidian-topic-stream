//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	repository "github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPostgresAdvisoryBackendDeliversAcrossConnections(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	backend := repository.NewAdvisoryBackend(store)
	listenContext, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	received := make(chan domain.Advisory, 1)
	expected := domain.Advisory{
		Kind: domain.AdvisoryJobChanged, WorkspaceID: uuid.New(), ResourceID: uuid.New(), Origin: uuid.New(),
	}
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, testutil.Receive(t, done), context.Canceled)
	})
	go func() {
		done <- backend.Listen(listenContext, func() error {
			return backend.Publish(listenContext, expected)
		}, func(advisory domain.Advisory) error {
			if advisory.WorkspaceID != expected.WorkspaceID {
				return nil
			}
			select {
			case received <- advisory:
			default:
			}
			return nil
		})
	}()

	require.Equal(t, expected, testutil.Receive(t, received))
}

func TestPostgresAdvisoryBackendPropagatesCallbackErrors(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	backend := repository.NewAdvisoryBackend(store)
	failure := errors.New("callback failed")

	t.Run("ready", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		readyCalls, accepted := 0, false
		err := backend.Listen(ctx, func() error {
			readyCalls++
			return failure
		}, func(domain.Advisory) error {
			accepted = true
			return nil
		})
		require.ErrorIs(t, err, failure)
		require.Equal(t, 1, readyCalls)
		require.False(t, accepted)
	})

	t.Run("accept", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		expected := domain.Advisory{
			Kind: domain.AdvisoryFeedInvalidated, WorkspaceID: uuid.New(), ResourceID: uuid.New(),
		}
		readyCalls, acceptCalls := 0, 0
		err := backend.Listen(ctx, func() error {
			readyCalls++
			return backend.Publish(ctx, expected)
		}, func(value domain.Advisory) error {
			if value.WorkspaceID != expected.WorkspaceID {
				return nil
			}
			acceptCalls++
			return failure
		})
		require.ErrorIs(t, err, failure)
		require.Equal(t, 1, readyCalls)
		require.Equal(t, 1, acceptCalls)
	})
}

func TestWorkspaceSettingsUpdatePublishesCommittedAdvisory(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	defer store.Close()
	workspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	cleanupWorkspace(t, dsn, workspaceID)
	listener, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, listener.Close(context.Background())) }()
	_, err = listener.Exec(t.Context(), `LISTEN topic_stream_advisories`)
	require.NoError(t, err)

	require.NoError(t, store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), time.Now()))
	current, err := store.GetOrCreateWorkspaceSettings(t.Context(), workspaceID, defaultWorkspaceSettings(time.Now()))
	require.NoError(t, err)
	current.Version++
	current.Retrieval.CandidateLimit = 75
	_, changed, err := store.UpdateWorkspaceSettings(t.Context(), workspaceID, 1, current)
	require.NoError(t, err)
	require.True(t, changed)
	waitContext, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var advisory domain.Advisory
	for advisory.WorkspaceID != workspaceID {
		notification, err := listener.WaitForNotification(waitContext)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal([]byte(notification.Payload), &advisory))
	}
	require.Equal(t, domain.AdvisorySettingsChanged, advisory.Kind)
	require.Equal(t, workspaceID, advisory.WorkspaceID)
	require.Equal(t, 2, advisory.Version)
}

func TestStorePersistsSourceItemAndCurrentFeedback(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)

	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	defer store.Close()
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	var migrationVersion int
	var dirty bool
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT version,dirty FROM topic_stream_schema_migrations`).Scan(&migrationVersion, &dirty))
	versions := testutil.MigrationVersions(t)
	require.Equal(t, int(versions[len(versions)-1]), migrationVersion)
	require.False(t, dirty)

	workspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	cleanupWorkspace(t, dsn, workspaceID)

	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	source := domain.Source{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: "Research",
		Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://example.com/feed.xml"}, Enabled: true, PollIntervalMinutes: 60,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(t.Context(), source))

	item := domain.Item{
		ID: uuid.New(), WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{source.ID}, Kind: domain.ItemKindArticle,
		Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{Description: "Useful", Content: "Useful"}},
		Title:   "Persistent item", URL: "https://example.com/one", CanonicalURL: "https://example.com/one",
		Excerpt: "Useful", ContentMarkdown: "Useful", ExtractionVersion: "readability-v1", Authors: []string{"Ada"}, DiscoveredAt: now,
		ReadingState: domain.ReadingStateUnread, Visibility: domain.ItemVisibilityVisible,
	}
	entry := domain.SourceEntry{
		SourceID: source.ID, ExternalID: "entry-1", ItemID: item.ID, URL: item.URL,
		Details: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{
			Description: "Useful",
		}},
		ExtractionVersion: "readability-v1", DiscoveredAt: now, UpdatedAt: now,
	}
	_, changed, err := store.UpsertItem(t.Context(), item, entry)
	require.NoError(t, err)
	require.True(t, changed)
	_, changed, err = store.UpsertItem(t.Context(), item, entry)
	require.NoError(t, err)
	require.False(t, changed)
	var sourceDetailsJSON []byte
	var sourceExtractionVersion string
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT details,extraction_version FROM source_entries WHERE source_id=$1 AND external_id=$2`,
		source.ID, entry.ExternalID).Scan(&sourceDetailsJSON, &sourceExtractionVersion))
	var sourceDetails domain.SourceEntryDetails
	require.NoError(t, json.Unmarshal(sourceDetailsJSON, &sourceDetails))
	require.Equal(t, entry.Details, sourceDetails)
	require.Equal(t, entry.ExtractionVersion, sourceExtractionVersion)

	createdAt := now.Add(time.Minute)
	feedback, found, err := store.SetFeedback(t.Context(), workspaceID, item.ID, domain.Feedback{
		Sentiment: "like", CreatedAt: createdAt, UpdatedAt: createdAt,
	})
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, createdAt.Equal(feedback.CreatedAt))

	updatedAt := now.Add(2 * time.Minute)
	feedback, found, err = store.SetFeedback(t.Context(), workspaceID, item.ID, domain.Feedback{
		Sentiment: "dislike", CreatedAt: updatedAt, UpdatedAt: updatedAt,
	})
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, createdAt.Equal(feedback.CreatedAt))
	require.True(t, updatedAt.Equal(feedback.UpdatedAt))

	stored, found, err := store.GetItem(t.Context(), workspaceID, item.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "dislike", stored.Feedback.Sentiment)
	require.Equal(t, "readability-v1", stored.ExtractionVersion)
	require.Equal(t, domain.ItemVisibilityVisible, stored.Visibility, "an Item without item_state defaults to visible")
	currentFeedback, err := store.ListCurrentFeedback(t.Context(), workspaceID)
	require.NoError(t, err)
	require.Equal(t, []domain.ItemFeedback{{ItemID: item.ID, Feedback: feedback.Feedback}}, currentFeedback)
	found, err = store.SetVisibility(t.Context(), workspaceID, item.ID, domain.ItemVisibilityHidden)
	require.NoError(t, err)
	require.True(t, found)
	stored, found, err = store.GetItem(t.Context(), workspaceID, item.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, domain.ItemVisibilityHidden, stored.Visibility)

}

func TestStorePersistsScopedTopicAndSemanticRevision(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)

	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	defer store.Close()

	workspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	cleanupWorkspace(t, dsn, workspaceID)

	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	source := domain.Source{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: "Research",
		Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://example.com/" + workspaceID.String() + "/feed.xml"}, Enabled: true, PollIntervalMinutes: 60,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(t.Context(), source))

	topic := domain.Topic{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: "AI", SourceIDs: []uuid.UUID{source.ID},
		ItemKinds: []domain.ItemKind{domain.ItemKindArticle, domain.ItemKindPaper}, Include: "inference systems",
		Exclude: "beginner tutorials", Mode: domain.TopicModeOrdinary, Enabled: true, Revision: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateTopic(t.Context(), topic))

	topic.Include = "production inference systems"
	topic.Mode = domain.TopicModeStrict
	topic.Revision = 2
	topic.UpdatedAt = now.Add(time.Minute)
	_, err = store.UpdateTopic(t.Context(), domain.TopicUpdate{Topic: topic, SemanticChanged: true})
	require.NoError(t, err)

	loaded, found, err := store.GetTopic(t.Context(), workspaceID, topic.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, topic.SourceIDs, loaded.SourceIDs)
	require.Equal(t, topic.ItemKinds, loaded.ItemKinds)
	require.Equal(t, 2, loaded.Revision)
	require.Equal(t, domain.TopicModeStrict, loaded.Mode)
}

func TestStoreProjectsCurrentTopicCorrections(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)

	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	defer store.Close()
	workspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	cleanupWorkspace(t, dsn, workspaceID)

	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	source := domain.Source{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: "Research",
		Config:  domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://example.com/" + workspaceID.String() + "/feed.xml"},
		Enabled: true, PollIntervalMinutes: 60, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(t.Context(), source))
	topic := domain.Topic{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: "AI", Include: "inference",
		Mode: domain.TopicModeOrdinary, Enabled: true, Revision: 1,
		CorrectionEpoch: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateTopic(t.Context(), topic))
	item := domain.Item{
		ID: uuid.New(), WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{source.ID}, Kind: domain.ItemKindArticle,
		Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{}},
		Title:   "Inference", URL: "https://example.com/item", CanonicalURL: "https://example.com/item",
		DiscoveredAt: now, ReadingState: domain.ReadingStateUnread, Visibility: domain.ItemVisibilityVisible,
	}
	entry := domain.SourceEntry{SourceID: source.ID, ExternalID: "entry", ItemID: item.ID, URL: item.URL, DiscoveredAt: now, UpdatedAt: now}
	_, _, err = store.UpsertItem(t.Context(), item, entry)
	require.NoError(t, err)
	_, found, err := store.SetTopicCorrection(t.Context(), domain.TopicCorrection{
		WorkspaceID: workspaceID, ItemID: item.ID, TopicID: topic.ID,
		Membership: domain.TopicMembershipInclude, UpdatedAt: now,
	})
	require.NoError(t, err)
	require.True(t, found)

	projection, found, err := store.LoadItemProjection(t.Context(), workspaceID, item.ID)

	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []domain.TopicCorrectionProjection{{TopicID: topic.ID, Membership: domain.TopicMembershipInclude, CorrectionEpoch: 1}}, projection.Projection.TopicCorrections)

	topic.Include = "production inference"
	topic.Revision = 2
	topic.UpdatedAt = now.Add(time.Minute)
	_, err = store.UpdateTopic(t.Context(), domain.TopicUpdate{Topic: topic, SemanticChanged: true})
	require.NoError(t, err)
	projection, found, err = store.LoadItemProjection(t.Context(), workspaceID, item.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []domain.TopicCorrectionProjection{{TopicID: topic.ID, Membership: domain.TopicMembershipInclude, CorrectionEpoch: 1}}, projection.Projection.TopicCorrections)

	cleared, err := store.UpdateTopic(t.Context(), domain.TopicUpdate{Topic: topic, ClearCorrections: true})
	require.NoError(t, err)
	require.Len(t, cleared.ProjectionCommits, 1)
	require.Equal(t, item.ID, cleared.ProjectionCommits[0].ItemID)
	projection, found, err = store.LoadItemProjection(t.Context(), workspaceID, item.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Empty(t, projection.Projection.TopicCorrections)
	loaded, found, err := store.GetTopic(t.Context(), workspaceID, topic.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(2), loaded.CorrectionEpoch)
	require.Equal(t, int64(2), loaded.CorrectionVersion)

	retried, err := store.UpdateTopic(t.Context(), domain.TopicUpdate{Topic: loaded, ClearCorrections: true})
	require.NoError(t, err)
	require.Empty(t, retried.ProjectionCommits)
	unchanged, found, err := store.GetTopic(t.Context(), workspaceID, topic.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, loaded.CorrectionEpoch, unchanged.CorrectionEpoch)
	require.Equal(t, loaded.CorrectionVersion, unchanged.CorrectionVersion)
}

func TestStoreMaterializesTopicGateCalibrationMonotonically(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)

	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	defer store.Close()
	workspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	cleanupWorkspace(t, dsn, workspaceID)
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), now))
	_, err = store.GetOrCreateActive(t.Context(), workspaceID, domain.DefaultEmbeddingModelID, now)
	require.NoError(t, err)

	source := domain.Source{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: "Calibration source",
		Config:  domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://example.com/" + workspaceID.String() + "/calibration.xml"},
		Enabled: true, PollIntervalMinutes: 60, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(t.Context(), source))
	topic := domain.Topic{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: "Calibration topic", SourceIDs: []uuid.UUID{source.ID},
		ItemKinds: []domain.ItemKind{domain.ItemKindArticle}, Include: "include", Exclude: "exclude",
		Mode: domain.TopicModeOrdinary, Enabled: true, Revision: 1, CorrectionEpoch: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateTopic(t.Context(), topic))
	item := domain.Item{
		ID: uuid.New(), WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{source.ID}, Kind: domain.ItemKindArticle,
		Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{}},
		Title:   "Calibration item", URL: "https://example.com/calibration-item", CanonicalURL: "https://example.com/calibration-item",
		DiscoveredAt: now, ReadingState: domain.ReadingStateUnread, Visibility: domain.ItemVisibilityVisible,
	}
	entry := domain.SourceEntry{SourceID: source.ID, ExternalID: "calibration-item", ItemID: item.ID,
		URL: item.URL, DiscoveredAt: now, UpdatedAt: now}
	_, _, err = store.UpsertItem(t.Context(), item, entry)
	require.NoError(t, err)
	_, found, err := store.SetTopicCorrection(t.Context(), domain.TopicCorrection{
		WorkspaceID: workspaceID, TopicID: topic.ID, ItemID: item.ID,
		Membership: domain.TopicMembershipInclude, UpdatedAt: now,
	})
	require.NoError(t, err)
	require.True(t, found)

	snapshot, found, err := store.LoadTopicCalibrationSnapshot(
		t.Context(), workspaceID, topic.ID, domain.DefaultEmbeddingModelID,
	)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(1), snapshot.Topic.CorrectionVersion)
	require.Equal(t, []uuid.UUID{item.ID}, []uuid.UUID{snapshot.Corrections[0].ItemID})

	calibration := domain.TopicGateCalibration{
		TopicID: topic.ID, EmbeddingModelID: domain.DefaultEmbeddingModelID,
		TopicRevision: 1, CorrectionEpoch: 1, CorrectionVersion: 1,
		AlgorithmVersion: "gate-v1", IncludeThreshold: 0.7, ExcludeThreshold: 0.8, UpdatedAt: now,
	}
	saved, err := store.SaveTopicGateCalibration(t.Context(), calibration)
	require.NoError(t, err)
	require.True(t, saved)
	older := calibration
	older.CorrectionVersion = 0
	older.IncludeThreshold = 0.1
	saved, err = store.SaveTopicGateCalibration(t.Context(), older)
	require.NoError(t, err)
	require.False(t, saved)
	loaded, found, err := store.GetTopicGateCalibration(t.Context(), topic.ID, domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 0.7, loaded.IncludeThreshold)

	stale, err := store.ListTopicsNeedingCalibration(
		t.Context(), workspaceID, domain.DefaultEmbeddingModelID, "gate-v1",
	)
	require.NoError(t, err)
	require.Empty(t, stale)
	_, found, err = store.SetTopicCorrection(t.Context(), domain.TopicCorrection{
		WorkspaceID: workspaceID, TopicID: topic.ID, ItemID: item.ID,
		Membership: domain.TopicMembershipExclude, UpdatedAt: now.Add(time.Minute),
	})
	require.NoError(t, err)
	require.True(t, found)
	stale, err = store.ListTopicsNeedingCalibration(
		t.Context(), workspaceID, domain.DefaultEmbeddingModelID, "gate-v1",
	)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{topic.ID}, stale)
}

func TestStoreLeasesJobsDeduplicatesAndRecoversExpiredWork(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)

	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	defer store.Close()
	workspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	cleanupWorkspace(t, dsn, workspaceID)

	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	key := "sync-source:one"
	queued := domain.Job{
		ID: uuid.New(), WorkspaceID: workspaceID, Kind: "sync-source", Queue: domain.JobQueueFetch, ArgumentsVersion: 1,
		Arguments:      json.RawMessage(`{"sourceId":"018fda3a-4e36-7d1d-8835-45165d3f3596"}`),
		IdempotencyKey: &key, Status: domain.JobStatusQueued, MaxAttempts: 3,
		AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
	stored, created, err := store.Jobs().Enqueue(t.Context(), queued)
	require.NoError(t, err)
	require.True(t, created)
	duplicate := queued
	duplicate.ID = uuid.New()
	duplicate, created, err = store.Jobs().Enqueue(t.Context(), duplicate)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, stored.ID, duplicate.ID)

	claim, err := store.Jobs().Claim(t.Context(), domain.JobClaimRequest{
		Queue: domain.JobQueueFetch, Owner: "worker-a", Now: now, LeaseExpiresAt: now.Add(time.Minute),
	})
	require.NoError(t, err)
	claimed := claim.Job
	require.Equal(t, []domain.JobIdentity{{ID: claimed.ID, WorkspaceID: workspaceID}}, claim.ChangedJobs)
	require.Equal(t, 1, claimed.Attempt)
	claim, err = store.Jobs().Claim(t.Context(), domain.JobClaimRequest{
		Queue: domain.JobQueueFetch, Owner: "worker-b", Now: now, LeaseExpiresAt: now.Add(time.Minute),
	})
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, claim.Job.ID)

	claim, err = store.Jobs().Claim(t.Context(), domain.JobClaimRequest{
		Queue: domain.JobQueueFetch, Owner: "worker-b", Now: now.Add(2 * time.Minute), LeaseExpiresAt: now.Add(3 * time.Minute),
	})
	require.NoError(t, err)
	recovered := claim.Job
	require.Equal(t, claimed.ID, recovered.ID)
	require.Equal(t, 2, recovered.Attempt)
	require.Equal(t, "worker-b", *recovered.LeaseOwner)
	requested, owned, err := store.Jobs().Heartbeat(t.Context(), domain.JobHeartbeat{
		ID: recovered.ID, Owner: "worker-b", Now: now.Add(2 * time.Minute), LeaseExpiresAt: now.Add(4 * time.Minute),
	})
	require.NoError(t, err)
	require.True(t, owned)
	require.False(t, requested)
	requestedJob, found, err := store.Jobs().RequestCancel(t.Context(), recovered.ID, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, requestedJob.CancellationRequested)
	finished, err := store.Jobs().Finish(t.Context(), domain.JobFinish{
		ID: recovered.ID, Owner: "worker-b", Status: domain.JobStatusSucceeded,
		AvailableAt: now, FinishedAt: pointerTime(now.Add(2 * time.Minute)), UpdatedAt: now.Add(2 * time.Minute),
	})
	require.NoError(t, err)
	require.True(t, finished)
	stored, found, err = store.Jobs().Get(t.Context(), recovered.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, domain.JobStatusCancelled, stored.Status, "a committed cancellation wins the completion race")

	exhausted := queued
	exhausted.ID = uuid.New()
	exhausted.IdempotencyKey = nil
	exhausted.MaxAttempts = 1
	exhausted.AvailableAt = now.Add(3 * time.Minute)
	exhausted.CreatedAt = exhausted.AvailableAt
	exhausted.UpdatedAt = exhausted.AvailableAt
	exhausted, created, err = store.Jobs().Enqueue(t.Context(), exhausted)
	require.NoError(t, err)
	require.True(t, created)
	claim, err = store.Jobs().Claim(t.Context(), domain.JobClaimRequest{
		Queue: domain.JobQueueFetch, Owner: "lost-worker", Now: exhausted.AvailableAt, LeaseExpiresAt: now.Add(4 * time.Minute),
	})
	require.NoError(t, err)
	require.Equal(t, exhausted.ID, claim.Job.ID)
	claim, err = store.Jobs().Claim(t.Context(), domain.JobClaimRequest{
		Queue: domain.JobQueueFetch, Owner: "recovery-worker", Now: now.Add(5 * time.Minute), LeaseExpiresAt: now.Add(6 * time.Minute),
	})
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, claim.Job.ID)
	require.Equal(t, []domain.JobIdentity{{ID: exhausted.ID, WorkspaceID: workspaceID}}, claim.ChangedJobs)
	exhausted, found, err = store.Jobs().Get(t.Context(), exhausted.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, domain.JobStatusDead, exhausted.Status)
}

func TestStoreUpdatesWorkspaceSettingsOptimistically(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	defer store.Close()
	workspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	cleanupWorkspace(t, dsn, workspaceID)

	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	defaults := defaultWorkspaceSettings(now)
	_, found, err := store.GetWorkspaceSettings(t.Context(), workspaceID)
	require.NoError(t, err)
	require.False(t, found)
	require.NoError(t, store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), time.Now()))
	current, err := store.GetOrCreateWorkspaceSettings(t.Context(), workspaceID, defaults)
	require.NoError(t, err)
	require.Equal(t, 1, current.Version)
	readOnly, found, err := store.GetWorkspaceSettings(t.Context(), workspaceID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, current, readOnly)
	current.Version = 2
	current.Retrieval.CandidateLimit = 60
	current.UpdatedAt = now.Add(time.Minute)
	updated, changed, err := store.UpdateWorkspaceSettings(t.Context(), workspaceID, 1, current)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, 60, updated.Retrieval.CandidateLimit)
	_, changed, err = store.UpdateWorkspaceSettings(t.Context(), workspaceID, 1, current)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestStoreKeepsServingModelUntilSharedTargetCutover(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	defer store.Close()
	workspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	cleanupWorkspace(t, dsn, workspaceID)

	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), time.Now()))
	settings, err := store.GetOrCreateWorkspaceSettings(t.Context(), workspaceID, defaultWorkspaceSettings(now))
	require.NoError(t, err)
	_, found, err := store.GetActive(t.Context(), workspaceID)
	require.NoError(t, err)
	require.False(t, found)
	first, err := store.GetOrCreateActive(t.Context(), workspaceID, domain.DefaultEmbeddingModelID, now)
	require.NoError(t, err)
	require.Equal(t, domain.DefaultEmbeddingModelID, first.ModelID)
	require.Equal(t, domain.RetrievalIndexActive, first.Status)
	readOnly, found, err := store.GetActive(t.Context(), workspaceID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, first, readOnly)

	targetID := uuid.New()
	target := domain.EmbeddingModel{
		ID: targetID, EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{
			Name: "second-" + workspaceID.String(), Endpoint: "http://localhost:11434/v1",
			ProviderModel: "second", Dimensions: 3,
		}, Enabled: true, InitializationStatus: domain.EmbeddingModelReady,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.EmbeddingModels().Create(t.Context(), target))
	settings.Version++
	settings.EmbeddingModelID = targetID
	settings.UpdatedAt = now.Add(time.Minute)
	_, changed, err := store.UpdateWorkspaceSettings(t.Context(), workspaceID, 1, settings)
	require.NoError(t, err)
	require.True(t, changed)
	transitionJobID := enqueueTransitionJob(t, store, workspaceID, now)
	selection, _, err := store.GetActive(t.Context(), workspaceID)
	require.NoError(t, err)
	second, err := store.Begin(t.Context(), domain.ModelTransitionStart{
		WorkspaceID: workspaceID, ModelID: targetID, JobID: transitionJobID, StartedAt: now.Add(time.Minute),
		SelectionGeneration: selection.SelectionGeneration,
	})
	require.NoError(t, err)
	require.Equal(t, targetID, second.ModelID)
	require.Equal(t, domain.RetrievalIndexBuilding, second.Status)
	status, err := store.EmbeddingModels().RetrievalStatus(t.Context(), workspaceID)
	require.NoError(t, err)
	require.Equal(t, &transitionJobID, status.TransitionJobID)
	unrelatedSettings := settings
	unrelatedSettings.Retrieval.CandidateLimit++
	unrelatedSettings.Version++
	unrelatedSettings.UpdatedAt = now.Add(100 * time.Second)
	settings, changed, err = store.UpdateWorkspaceSettings(t.Context(), workspaceID, settings.Version, unrelatedSettings)
	require.NoError(t, err)
	require.True(t, changed, "unrelated settings remain editable during a model transition")
	stillFirst, found, err := store.GetActive(t.Context(), workspaceID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, first.ModelID, stillFirst.ModelID)

	require.NoError(t, store.Activate(t.Context(), domain.ModelTransitionCutover{
		WorkspaceID: workspaceID, ModelID: targetID, JobID: transitionJobID, ActivatedAt: now.Add(2 * time.Minute),
		SelectionGeneration: selection.SelectionGeneration,
	}))
	active, found, err := store.GetActive(t.Context(), workspaceID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, targetID, active.ModelID)
	require.Equal(t, second.Collection, active.Collection)
	require.Equal(t, domain.RetrievalIndexActive, active.Status)

	otherWorkspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), otherWorkspaceID))
	cleanupWorkspace(t, dsn, otherWorkspaceID)
	otherSettings := defaultWorkspaceSettings(now)
	otherSettings.EmbeddingModelID = targetID
	_, err = store.GetOrCreateWorkspaceSettings(t.Context(), otherWorkspaceID, otherSettings)
	require.NoError(t, err)
	other, err := store.GetOrCreateActive(t.Context(), otherWorkspaceID, targetID, now)
	require.NoError(t, err)
	require.Equal(t, active.Collection, other.Collection, "one model must use one shared collection")
}

func TestStoreBlocksModelCutoverUntilExistingItemProjectionIsDelivered(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	defer store.Close()
	workspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	cleanupWorkspace(t, dsn, workspaceID)
	now := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), time.Now()))
	_, err = store.GetOrCreateWorkspaceSettings(t.Context(), workspaceID, defaultWorkspaceSettings(now))
	require.NoError(t, err)
	_, err = store.GetOrCreateActive(t.Context(), workspaceID, domain.DefaultEmbeddingModelID, now)
	require.NoError(t, err)

	source := domain.Source{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: "Transition source",
		Config:  domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://example.com/" + workspaceID.String()},
		Enabled: true, PollIntervalMinutes: 60, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(t.Context(), source))
	item := domain.Item{
		ID: uuid.New(), WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{source.ID}, Kind: domain.ItemKindArticle,
		Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{}},
		Title:   "Existing item", URL: source.Config.URL + "/item", CanonicalURL: source.Config.URL + "/item",
		DiscoveredAt: now, ReadingState: domain.ReadingStateUnread, Visibility: domain.ItemVisibilityVisible,
	}
	commit, _, err := store.UpsertItem(t.Context(), item, domain.SourceEntry{
		SourceID: source.ID, ExternalID: item.ID.String(), ItemID: item.ID, URL: item.URL,
		DiscoveredAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	target := domain.EmbeddingModel{
		ID: uuid.New(), EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{
			Name: "cutover-" + workspaceID.String(), Endpoint: "http://localhost:11434/v1",
			ProviderModel: "cutover", Dimensions: 3,
		}, Enabled: true, InitializationStatus: domain.EmbeddingModelReady,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.EmbeddingModels().Create(t.Context(), target))
	settings, found, err := store.GetWorkspaceSettings(t.Context(), workspaceID)
	require.NoError(t, err)
	require.True(t, found)
	settings.EmbeddingModelID = target.ID
	settings.Version++
	settings.UpdatedAt = now.Add(time.Minute)
	_, changed, err := store.UpdateWorkspaceSettings(t.Context(), workspaceID, 1, settings)
	require.NoError(t, err)
	require.True(t, changed)

	transitionJobID := enqueueTransitionJob(t, store, workspaceID, now)
	selection, _, err := store.GetActive(t.Context(), workspaceID)
	require.NoError(t, err)
	_, err = store.Begin(t.Context(), domain.ModelTransitionStart{
		WorkspaceID: workspaceID, ModelID: target.ID, JobID: transitionJobID, StartedAt: now.Add(time.Minute),
		SelectionGeneration: selection.SelectionGeneration,
	})
	require.NoError(t, err)
	_, pendingModels := projectionState(t, dsn, workspaceID, item.ID)
	require.Contains(t, pendingModels, target.ID)
	require.Error(t, store.Activate(t.Context(), domain.ModelTransitionCutover{
		WorkspaceID: workspaceID, ModelID: target.ID, JobID: transitionJobID, ActivatedAt: now.Add(2 * time.Minute),
		SelectionGeneration: selection.SelectionGeneration,
	}))

	completed, err := store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{
		WorkspaceID: workspaceID, ItemID: item.ID, ModelID: target.ID, Revision: commit.ProjectionRevision,
	})
	require.NoError(t, err)
	require.True(t, completed)
	require.NoError(t, store.Activate(t.Context(), domain.ModelTransitionCutover{
		WorkspaceID: workspaceID, ModelID: target.ID, JobID: transitionJobID, ActivatedAt: now.Add(3 * time.Minute),
		SelectionGeneration: selection.SelectionGeneration,
	}))
}

func TestStoreOnlyClearsTheMatchingFailedModelTransition(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	defer store.Close()
	workspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	cleanupWorkspace(t, dsn, workspaceID)

	now := time.Date(2026, time.September, 30, 13, 0, 0, 0, time.UTC)
	require.NoError(t, store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), time.Now()))
	_, err = store.GetOrCreateWorkspaceSettings(t.Context(), workspaceID, defaultWorkspaceSettings(now))
	require.NoError(t, err)
	serving, err := store.GetOrCreateActive(t.Context(), workspaceID, domain.DefaultEmbeddingModelID, now)
	require.NoError(t, err)
	target := domain.EmbeddingModel{
		ID: uuid.New(), EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{
			Name: "failed-transition-" + workspaceID.String(), Endpoint: "http://localhost:11434/v1",
			ProviderModel: "failed-transition", Dimensions: 3,
		},
		Enabled: true, InitializationStatus: domain.EmbeddingModelReady,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.EmbeddingModels().Create(t.Context(), target))
	selectionGeneration := selectWorkspaceModel(t, store, workspaceID, target.ID)
	jobID := enqueueTransitionJob(t, store, workspaceID, now)
	_, err = store.Begin(t.Context(), domain.ModelTransitionStart{
		WorkspaceID: workspaceID, ModelID: target.ID, JobID: jobID, StartedAt: now,
		SelectionGeneration: selectionGeneration,
	})
	require.NoError(t, err)

	require.NoError(t, store.Fail(t.Context(), domain.RetrievalIndexFailure{
		WorkspaceID: workspaceID, ModelID: target.ID, JobID: uuid.New(),
		Generation: target.Version, Reason: "stale worker", FailedAt: now.Add(time.Minute),
		SelectionGeneration: selectionGeneration,
	}))
	status, err := store.EmbeddingModels().RetrievalStatus(t.Context(), workspaceID)
	require.NoError(t, err)
	require.Equal(t, &jobID, status.TransitionJobID)

	require.NoError(t, store.Fail(t.Context(), domain.RetrievalIndexFailure{
		WorkspaceID: workspaceID, ModelID: target.ID, JobID: jobID,
		Generation: target.Version, Reason: "qdrant unavailable", FailedAt: now.Add(2 * time.Minute),
		SelectionGeneration: selectionGeneration,
	}))
	status, err = store.EmbeddingModels().RetrievalStatus(t.Context(), workspaceID)
	require.NoError(t, err)
	require.Nil(t, status.TransitionJobID)
	stillServing, found, err := store.GetActive(t.Context(), workspaceID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, serving.ModelID, stillServing.ModelID)
}

func TestFeedbackCommitCreatesDualProjectionWorkAndIdenticalRetryKeepsRevision(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	defer store.Close()
	workspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	cleanupWorkspace(t, dsn, workspaceID)

	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), time.Now()))
	_, err = store.GetOrCreateWorkspaceSettings(t.Context(), workspaceID, defaultWorkspaceSettings(now))
	require.NoError(t, err)
	_, err = store.GetOrCreateActive(t.Context(), workspaceID, domain.DefaultEmbeddingModelID, now)
	require.NoError(t, err)
	target := domain.EmbeddingModel{
		ID: uuid.New(), EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{
			Name: "projection-target-" + workspaceID.String(), Endpoint: "http://localhost:11434/v1",
			ProviderModel: "target-" + workspaceID.String(), Dimensions: 3,
		},
		Enabled: true, InitializationStatus: domain.EmbeddingModelReady,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.EmbeddingModels().Create(t.Context(), target))
	selectionGeneration := selectWorkspaceModel(t, store, workspaceID, target.ID)
	transitionJobID := enqueueTransitionJob(t, store, workspaceID, now)
	_, err = store.Begin(t.Context(), domain.ModelTransitionStart{
		WorkspaceID: workspaceID, ModelID: target.ID, JobID: transitionJobID, StartedAt: now,
		SelectionGeneration: selectionGeneration,
	})
	require.NoError(t, err)

	source := domain.Source{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: "Projection source",
		Config:  domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://example.com/" + workspaceID.String()},
		Enabled: true, PollIntervalMinutes: 60, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(t.Context(), source))
	item := domain.Item{
		ID: uuid.New(), WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{source.ID}, Kind: domain.ItemKindArticle,
		Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{}},
		Title:   "Projection target", URL: source.Config.URL + "/item", CanonicalURL: source.Config.URL + "/item",
		DiscoveredAt: now, ReadingState: domain.ReadingStateUnread, Visibility: domain.ItemVisibilityVisible,
	}
	itemCommit, changed, err := store.UpsertItem(t.Context(), item, domain.SourceEntry{
		SourceID: source.ID, ExternalID: item.ID.String(), ItemID: item.ID, URL: item.URL,
		Details:      domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{}},
		DiscoveredAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	require.True(t, changed)
	itemRevision, itemPendingModels := projectionState(t, dsn, workspaceID, item.ID)
	require.NotEqual(t, uuid.Nil, itemRevision)
	require.Equal(t, itemRevision, itemCommit.ProjectionRevision)
	require.Equal(t, workspaceID, itemCommit.WorkspaceID)
	require.Equal(t, item.ID, itemCommit.ItemID)
	require.ElementsMatch(t, []uuid.UUID{domain.DefaultEmbeddingModelID, target.ID}, itemPendingModels)
	retriedItemCommit, changed, err := store.UpsertItem(t.Context(), item, domain.SourceEntry{
		SourceID: source.ID, ExternalID: item.ID.String(), ItemID: item.ID, URL: item.URL,
		Details:      domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{}},
		DiscoveredAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, itemCommit, retriedItemCommit, "an unchanged retry must expose still-pending projection work")
	pendingCommits, err := store.ListPendingItemProjections(t.Context(), workspaceID, nil, 10)
	require.NoError(t, err)
	require.Equal(t, []domain.ItemProjectionCommit{itemCommit}, pendingCommits)

	first, found, err := store.SetFeedback(t.Context(), workspaceID, item.ID, domain.Feedback{
		Sentiment: "like", CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	require.True(t, found)
	revision, pendingModels := projectionState(t, dsn, workspaceID, item.ID)
	require.ElementsMatch(t, []uuid.UUID{domain.DefaultEmbeddingModelID, target.ID}, pendingModels)

	retried, found, err := store.SetFeedback(t.Context(), workspaceID, item.ID, domain.Feedback{
		Sentiment: "like", CreatedAt: now.Add(time.Hour), UpdatedAt: now.Add(time.Hour),
	})
	require.NoError(t, err)
	require.True(t, found)
	retriedRevision, retriedPendingModels := projectionState(t, dsn, workspaceID, item.ID)
	require.Equal(t, first.UpdatedAt, retried.UpdatedAt)
	require.Equal(t, revision, retriedRevision)
	require.ElementsMatch(t, pendingModels, retriedPendingModels)

	_, cleared, err := store.ClearFeedback(t.Context(), workspaceID, item.ID, now.Add(2*time.Hour))
	require.NoError(t, err)
	require.True(t, cleared)
	clearedRevision, clearedPendingModels := projectionState(t, dsn, workspaceID, item.ID)
	require.NotEqual(t, revision, clearedRevision)
	require.ElementsMatch(t, pendingModels, clearedPendingModels)
	_, cleared, err = store.ClearFeedback(t.Context(), workspaceID, item.ID, now.Add(3*time.Hour))
	require.NoError(t, err)
	require.True(t, cleared)
	retriedClearRevision, _ := projectionState(t, dsn, workspaceID, item.ID)
	require.Equal(t, clearedRevision, retriedClearRevision)

	topic := domain.Topic{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: "Projection topic", Mode: domain.TopicModeOrdinary,
		Enabled: true, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateTopic(t.Context(), topic))
	correction := domain.TopicCorrection{
		WorkspaceID: workspaceID, ItemID: item.ID, TopicID: topic.ID,
		Membership: domain.TopicMembershipInclude, UpdatedAt: now.Add(4 * time.Hour),
	}
	_, found, err = store.SetTopicCorrection(t.Context(), correction)
	require.NoError(t, err)
	require.True(t, found)
	correctionRevision, _ := projectionState(t, dsn, workspaceID, item.ID)
	require.NotEqual(t, clearedRevision, correctionRevision)
	correction.UpdatedAt = now.Add(5 * time.Hour)
	_, found, err = store.SetTopicCorrection(t.Context(), correction)
	require.NoError(t, err)
	require.True(t, found)
	retriedCorrectionRevision, _ := projectionState(t, dsn, workspaceID, item.ID)
	require.Equal(t, correctionRevision, retriedCorrectionRevision)

	work, found, err := store.LoadItemProjection(t.Context(), workspaceID, item.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, correctionRevision, work.Projection.Revision)
	require.Equal(t, item.ID, work.Projection.Item.ID)
	require.Nil(t, work.Projection.Feedback)
	require.Equal(t, []domain.TopicCorrectionProjection{{
		TopicID: topic.ID, Membership: domain.TopicMembershipInclude, CorrectionEpoch: 1,
	}}, work.Projection.TopicCorrections)
	modelIDs := make([]uuid.UUID, len(work.Models))
	for index, model := range work.Models {
		modelIDs[index] = model.ID
	}
	require.ElementsMatch(t, []uuid.UUID{domain.DefaultEmbeddingModelID, target.ID}, modelIDs)

	completed, err := store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{
		WorkspaceID: workspaceID, ItemID: item.ID, ModelID: target.ID, Revision: uuid.New(),
	})
	require.NoError(t, err)
	require.False(t, completed)
	_, pendingAfterStaleCompletion := projectionState(t, dsn, workspaceID, item.ID)
	require.ElementsMatch(t, pendingModels, pendingAfterStaleCompletion)
	completed, err = store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{
		WorkspaceID: workspaceID, ItemID: item.ID, ModelID: target.ID, Revision: correctionRevision,
	})
	require.NoError(t, err)
	require.True(t, completed)
	_, pendingAfterCompletion := projectionState(t, dsn, workspaceID, item.ID)
	require.Equal(t, []uuid.UUID{domain.DefaultEmbeddingModelID}, pendingAfterCompletion)
}

func projectionState(t *testing.T, dsn string, workspaceID, itemID uuid.UUID) (uuid.UUID, []uuid.UUID) {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	var revision uuid.UUID
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT revision FROM item_projection_state
WHERE workspace_id=$1 AND item_id=$2`, workspaceID, itemID).Scan(&revision))
	rows, err := pool.Query(t.Context(), `SELECT embedding_model_id FROM retrieval_projection_pending
WHERE workspace_id=$1 AND item_id=$2 ORDER BY embedding_model_id`, workspaceID, itemID)
	require.NoError(t, err)
	defer rows.Close()
	models := make([]uuid.UUID, 0, 2)
	for rows.Next() {
		var modelID uuid.UUID
		require.NoError(t, rows.Scan(&modelID))
		models = append(models, modelID)
	}
	require.NoError(t, rows.Err())
	return revision, models
}

func enqueueTransitionJob(
	t *testing.T,
	store *repository.Store,
	workspaceID uuid.UUID,
	now time.Time,
) uuid.UUID {
	t.Helper()
	job := domain.Job{
		ID: uuid.New(), WorkspaceID: workspaceID, Kind: "reindex-workspace", Queue: domain.JobQueueMaintenance, ArgumentsVersion: 2,
		Arguments: json.RawMessage(`{}`), Status: domain.JobStatusQueued, MaxAttempts: 3,
		AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
	stored, created, err := store.Jobs().Enqueue(t.Context(), job)
	require.NoError(t, err)
	require.True(t, created)
	return stored.ID
}

func defaultWorkspaceSettings(now time.Time) domain.WorkspaceSettings {
	return domain.WorkspaceSettings{
		Version: 1, EmbeddingModelID: domain.DefaultEmbeddingModelID,
		Generation: domain.GenerationSettings{}, Summary: domain.SummarySettings{
			Language: "same-as-source", MaxInputChars: 48000, MaxSummaryCharacters: 800,
		},
		Instructions: domain.TaskInstructions{
			SummaryChunk: "chunk", SummaryReduce: "reduce", SummaryFinal: "final", DigestRerank: "rerank",
		},
		Retrieval: domain.RetrievalSettings{
			CandidateLimit: 50, RerankLimit: 30, RRFK: 60,
			RecencyHalfLifeDays: 7, RecencyMaxBoost: .1, FeedSourceCap: 3,
			DigestSourceCap: 2, DigestMMRLambda: .75,
		},
		UpdatedAt: now,
	}
}

func cleanupWorkspace(t *testing.T, dsn string, workspaceID uuid.UUID) {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		defer pool.Close()
		_, cleanupErr := pool.Exec(context.Background(), `DELETE FROM workspaces WHERE id=$1`, workspaceID)
		require.NoError(t, cleanupErr)
	})
}

func pointerTime(value time.Time) *time.Time { return &value }
