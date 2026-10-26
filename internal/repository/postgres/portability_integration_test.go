//go:build integration

package postgres_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	repository "github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/portability"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

var _ portability.Repository = (*repository.Store)(nil)

func TestWorkspaceArchiveRoundTripDefersFeedbackUntilItemReturns(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	defer store.Close()

	originID, targetID := uuid.New(), uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), originID))
	require.NoError(t, store.EnsureWorkspace(t.Context(), targetID))
	cleanupWorkspace(t, dsn, originID)
	cleanupWorkspace(t, dsn, targetID)
	now := time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)
	source := domain.Source{
		ID: uuid.New(), WorkspaceID: originID, Name: "Feed", Enabled: true, PollIntervalMinutes: 60,
		Config:    domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://example.com/feed"},
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Create(t.Context(), source))
	topic := domain.Topic{
		ID: uuid.New(), WorkspaceID: originID, Name: "Systems", SourceIDs: []uuid.UUID{source.ID},
		Mode: domain.TopicModeOrdinary, Enabled: true, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateTopic(t.Context(), topic))
	digest := domain.DigestDefinition{
		ID: uuid.New(), WorkspaceID: originID, Name: "Weekly",
		Topics: []domain.DigestTopic{{TopicID: topic.ID, ItemLimit: 5}}, Timezone: "UTC",
		Recurrence: domain.DigestRecurrence{Frequency: domain.RecurrenceWeekly, Weekdays: []int{1}, Hour: 9},
		Enabled:    true, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateDigest(t.Context(), digest))
	workspaceSettings := settings.DefaultWorkspaceSettings(now)
	workspaceSettings.Version = 2
	require.NoError(t, store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), time.Now()))
	_, err = store.GetOrCreateWorkspaceSettings(t.Context(), originID, workspaceSettings)
	require.NoError(t, err)
	profile := domain.PreferenceProfile{Prefer: "systems", Revision: 1, Version: 1, UpdatedAt: now}
	_, err = store.GetOrCreatePreferenceProfile(t.Context(), originID, profile)
	require.NoError(t, err)
	item := archiveFeedbackItem(originID, source.ID, now)
	_, _, err = store.UpsertItem(t.Context(), item, domain.SourceEntry{
		SourceID: source.ID, ExternalID: "entry", ItemID: item.ID, URL: item.URL,
		DiscoveredAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	_, found, err := store.SetFeedback(t.Context(), originID, item.ID, domain.Feedback{
		Sentiment: "like", CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	require.True(t, found)

	archive, err := store.ExportWorkspace(t.Context(), originID)
	require.NoError(t, err)
	archive.Settings = workspaceSettings
	archive.Profile = profile
	archive.Sources[0].Enabled = false
	archive.Sources[0].LastSyncedAt = nil
	archive.Digests[0].Enabled = false
	archive.Digests[0].NextRunAt = nil
	deleteWorkspaceNow(t, dsn, originID)
	workspaceArchive := domain.WorkspaceArchive{
		FormatVersion: domain.WorkspaceArchiveFormatV2, ExportedAt: now, Data: archive,
	}
	require.NoError(t, store.ImportWorkspace(t.Context(), targetID, workspaceArchive))

	importedSources, err := store.List(t.Context(), targetID)
	require.NoError(t, err)
	require.Len(t, importedSources, 1)
	require.False(t, importedSources[0].Enabled)
	importedDigests, err := store.ListDigests(t.Context(), targetID)
	require.NoError(t, err)
	require.Len(t, importedDigests, 1)
	require.False(t, importedDigests[0].Enabled)
	feedback, err := store.ListCurrentFeedback(t.Context(), targetID)
	require.NoError(t, err)
	require.Empty(t, feedback)

	item.WorkspaceID = targetID
	item.ID = uuid.New()
	_, _, err = store.UpsertItem(t.Context(), item, domain.SourceEntry{
		SourceID: source.ID, ExternalID: "restored-entry", ItemID: item.ID, URL: item.URL,
		DiscoveredAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	feedback, err = store.ListCurrentFeedback(t.Context(), targetID)
	require.NoError(t, err)
	require.Len(t, feedback, 1)
	require.Equal(t, "like", feedback[0].Feedback.Sentiment)

	require.NoError(t, store.ImportWorkspace(t.Context(), targetID, workspaceArchive), "retrying the same committed import is a no-op")
	workspaceArchive.ExportedAt = workspaceArchive.ExportedAt.Add(time.Second)
	err = store.ImportWorkspace(t.Context(), targetID, workspaceArchive)
	require.ErrorIs(t, err, domain.ErrWorkspaceNotEmpty)
}

func TestWorkspaceImportRollsBackAllStateOnInvalidReference(t *testing.T) {
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	defer store.Close()

	workspaceID := uuid.New()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	cleanupWorkspace(t, dsn, workspaceID)
	now := time.Now().UTC()
	data := domain.WorkspaceArchiveData{
		Settings: settings.DefaultWorkspaceSettings(now),
		Profile:  domain.PreferenceProfile{Revision: 1, Version: 1, UpdatedAt: now},
		Sources: []domain.Source{{
			ID: uuid.New(), Name: "Feed", Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://example.com/feed"},
			PollIntervalMinutes: 60, CreatedAt: now, UpdatedAt: now,
		}},
		Topics: []domain.Topic{{
			ID: uuid.New(), Name: "Broken", SourceIDs: []uuid.UUID{uuid.New()},
			Mode: domain.TopicModeOrdinary, Revision: 1, CreatedAt: now, UpdatedAt: now,
		}},
	}

	err = store.ImportWorkspace(t.Context(), workspaceID, domain.WorkspaceArchive{
		FormatVersion: domain.WorkspaceArchiveFormatV2, ExportedAt: now, Data: data,
	})
	require.Error(t, err)
	require.False(t, errors.Is(err, domain.ErrWorkspaceNotEmpty))
	sources, listErr := store.List(t.Context(), workspaceID)
	require.NoError(t, listErr)
	require.Empty(t, sources)
}

func archiveFeedbackItem(workspaceID, sourceID uuid.UUID, now time.Time) domain.Item {
	return domain.Item{
		ID: uuid.New(), WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{sourceID},
		Kind: domain.ItemKindArticle,
		Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{
			Description: "Portable", Content: "Portable",
		}},
		Title: "Portable", URL: "https://example.com/item", CanonicalURL: "https://example.com/item",
		Excerpt: "Portable", ContentMarkdown: "Portable", Authors: []string{}, DiscoveredAt: now,
	}
}

func deleteWorkspaceNow(t *testing.T, dsn string, workspaceID uuid.UUID) {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(t.Context(), `DELETE FROM workspaces WHERE id=$1`, workspaceID)
	require.NoError(t, err)
}
