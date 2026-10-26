package digests_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	jobmocks "github.com/ethernity/obsidian-topic-stream/internal/service/jobs/mocks"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type digestRankerStub struct {
	results  map[uuid.UUID]ranking.TopicResult
	errors   map[uuid.UUID]error
	requests []ranking.TopicRequest
}

// RankTopic returns the fixture's Topic ranking result.
func (s *digestRankerStub) RankTopic(_ context.Context, request ranking.TopicRequest) (ranking.TopicResult, error) {
	s.requests = append(s.requests, request)
	if err := s.errors[request.Topic.ID]; err != nil {
		return ranking.TopicResult{}, err
	}
	return s.results[request.Topic.ID], nil
}

type digestSettingsStub struct {
	value domain.WorkspaceSettings
	calls int
}

// GetWorkspace returns the fixture's Workspace Settings snapshot.
func (s *digestSettingsStub) GetWorkspace(context.Context) (domain.WorkspaceSettings, error) {
	s.calls++
	return s.value, nil
}

func defaultDigestSettings() *digestSettingsStub {
	return &digestSettingsStub{value: domain.WorkspaceSettings{Retrieval: domain.RetrievalSettings{
		CandidateLimit: 50, RRFK: 60,
	}}}
}

func TestCreateCollectsDigestInputViolationsBeforeWriting(t *testing.T) {
	t.Parallel()
	service := newDigestService(t, digests.Config{WorkspaceID: uuid.New(), Repository: mocks.NewMockRepository(gomock.NewController(t)),
		Items: digestItems(t, nil), Topics: digestTopics(t, nil), Ranker: &digestRankerStub{}, Jobs: newJobQueue(t), Events: events.NewForWorkspace(uuid.Nil, nil), Now: time.Now})
	_, err := service.Create(t.Context(), digests.Input{Name: "", Timezone: "Invalid/Zone", Recurrence: domain.DigestRecurrence{Frequency: domain.RecurrenceDaily, Hour: 24, Minute: 60}})
	var validation *domain.ValidationError
	require.ErrorAs(t, err, &validation)
	require.ErrorIs(t, err, digests.ErrInvalid)
	require.Len(t, validation.Violations, 5)
}

func TestCreateDigestPreservesOrderedPerTopicLimitsAndComputesNextRun(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	firstTopicID, secondTopicID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 10, 0, 0, 0, time.UTC)
	repository := mocks.NewMockRepository(gomock.NewController(t))
	var savedDefinition domain.DigestDefinition
	repository.EXPECT().CreateDigest(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, value domain.DigestDefinition) error { savedDefinition = value; return nil })
	repository.EXPECT().GetDigest(gomock.Any(), workspaceID, gomock.Any()).DoAndReturn(func(_ context.Context, _ uuid.UUID, id uuid.UUID) (domain.DigestDefinition, bool, error) {
		require.Equal(t, savedDefinition.ID, id)
		return savedDefinition, true, nil
	}).AnyTimes()
	service := newDigestService(t, digests.Config{
		WorkspaceID: workspaceID, Repository: repository,
		Items: digestItems(t, nil),
		Topics: digestTopics(t, []domain.Topic{
			{ID: firstTopicID, WorkspaceID: workspaceID, Enabled: true},
			{ID: secondTopicID, WorkspaceID: workspaceID, Enabled: true},
		}),
		Ranker: &digestRankerStub{}, Jobs: newJobQueue(t),
		Events: events.NewForWorkspace(uuid.Nil, nil), Now: func() time.Time { return now },
	})

	created, err := service.Create(t.Context(), digests.Input{
		Name: "Weekly research", Topics: []domain.DigestTopic{
			{TopicID: firstTopicID, ItemLimit: 3},
			{TopicID: secondTopicID, ItemLimit: 8},
		},
		Timezone: "UTC", Recurrence: domain.DigestRecurrence{
			Frequency: domain.RecurrenceWeekly, Weekdays: []int{1}, Hour: 12,
		}, Enabled: true,
	})

	require.NoError(t, err)
	require.Equal(t, "Weekly research", created.Name)
	require.Equal(t, []domain.DigestTopic{
		{TopicID: firstTopicID, ItemLimit: 3},
		{TopicID: secondTopicID, ItemLimit: 8},
	}, created.Topics)
	require.Equal(t, 1, created.Version)
	require.NotNil(t, created.NextRunAt)
	require.True(t, time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC).Equal(*created.NextRunAt))
}

func TestManualGenerationDoesNotShiftCalendar(t *testing.T) {
	t.Parallel()

	workspaceID, topicID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 10, 0, 0, 0, time.UTC)
	repository := mocks.NewMockRepository(gomock.NewController(t))
	var savedDefinition domain.DigestDefinition
	repository.EXPECT().CreateDigest(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, value domain.DigestDefinition) error { savedDefinition = value; return nil })
	repository.EXPECT().GetDigest(gomock.Any(), workspaceID, gomock.Any()).DoAndReturn(func(_ context.Context, _ uuid.UUID, id uuid.UUID) (domain.DigestDefinition, bool, error) {
		require.Equal(t, savedDefinition.ID, id)
		return savedDefinition, true, nil
	}).AnyTimes()
	service := newDigestService(t, digests.Config{
		WorkspaceID: workspaceID, Repository: repository,
		Items:  digestItems(t, nil),
		Topics: digestTopics(t, []domain.Topic{{ID: topicID, WorkspaceID: workspaceID, Enabled: true}}),
		Ranker: &digestRankerStub{}, Jobs: newJobQueue(t),
		Events: events.NewForWorkspace(uuid.Nil, nil), Now: func() time.Time { return now },
	})
	created, err := service.Create(t.Context(), digests.Input{
		Name: "Daily", Topics: []domain.DigestTopic{{TopicID: topicID, ItemLimit: 5}}, Timezone: "UTC",
		Recurrence: domain.DigestRecurrence{Frequency: domain.RecurrenceDaily, Hour: 12}, Enabled: true,
	})
	require.NoError(t, err)
	originalNextRun := *created.NextRunAt
	repository.EXPECT().AdmitDigestBuild(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request domain.DigestBuildRequest) (domain.DigestBuildAdmission, error) {
		return domain.DigestBuildAdmission{Job: request.Job, Created: true}, nil
	})

	job, err := service.Generate(t.Context(), created.ID, uuid.New())

	require.NoError(t, err)
	require.Equal(t, digests.GenerationJobKind, job.Kind)
	require.True(t, originalNextRun.Equal(*savedDefinition.NextRunAt))

}

func TestSchedulerEnqueuesOneCatchUpAndAdvancesFromCurrentTime(t *testing.T) {
	t.Parallel()

	workspaceID, topicID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 10, 0, 0, 0, time.UTC)
	queue := newJobQueue(t)
	repository := mocks.NewMockRepository(gomock.NewController(t))
	var savedDefinition domain.DigestDefinition
	repository.EXPECT().CreateDigest(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, value domain.DigestDefinition) error { savedDefinition = value; return nil })
	repository.EXPECT().GetDigest(gomock.Any(), workspaceID, gomock.Any()).DoAndReturn(func(_ context.Context, _ uuid.UUID, id uuid.UUID) (domain.DigestDefinition, bool, error) {
		require.Equal(t, savedDefinition.ID, id)
		return savedDefinition, true, nil
	}).AnyTimes()
	service := newDigestService(t, digests.Config{
		WorkspaceID: workspaceID, Repository: repository, Items: digestItems(t, nil),
		Topics: digestTopics(t, []domain.Topic{{ID: topicID, WorkspaceID: workspaceID, Enabled: true}}),
		Ranker: &digestRankerStub{}, Jobs: queue, Events: events.NewForWorkspace(uuid.Nil, nil), Now: func() time.Time { return now },
	})
	definition, err := service.Create(t.Context(), digests.Input{
		Name: "Weekly", Topics: []domain.DigestTopic{{TopicID: topicID, ItemLimit: 5}}, Timezone: "UTC",
		Recurrence: domain.DigestRecurrence{Frequency: domain.RecurrenceWeekly, Weekdays: []int{1}, Hour: 12},
		Enabled:    true,
	})
	require.NoError(t, err)
	require.True(t, time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC).Equal(*definition.NextRunAt))
	now = time.Date(2026, time.October, 10, 10, 0, 0, 0, time.UTC)
	repository.EXPECT().ListDueDigests(gomock.Any(), workspaceID, now).Return([]domain.DigestDefinition{definition}, nil)
	repository.EXPECT().ListDueDigests(gomock.Any(), workspaceID, now).Return(nil, nil)
	repository.EXPECT().AdmitScheduledDigestBuild(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request domain.DigestScheduledBuild) (domain.DigestBuildAdmission, error) {
		require.Equal(t, domain.DigestScheduleAdvance{WorkspaceID: workspaceID, DigestID: definition.ID, Expected: *definition.NextRunAt, NextRunAt: time.Date(2026, time.October, 12, 12, 0, 0, 0, time.UTC), UpdatedAt: now}, request.Advance)
		require.Equal(t, definition.Version, request.ExpectedVersion)
		require.Equal(t, "generate-digest:"+definition.ID.String(), *request.Job.IdempotencyKey)
		var arguments struct {
			ScheduledFor *time.Time `json:"scheduledFor"`
		}
		require.NoError(t, json.Unmarshal(request.Job.Arguments, &arguments))
		require.True(t, definition.NextRunAt.Equal(*arguments.ScheduledFor))
		return domain.DigestBuildAdmission{Job: request.Job, Created: true}, nil
	})

	require.NoError(t, service.PollSchedules(t.Context()))
	require.NoError(t, service.PollSchedules(t.Context()))
	require.Empty(t, queue.requests, "scheduler must not enqueue outside atomic admission")

}

func TestGenerationCreatesImmutableSectionsDeduplicatesAndNotifies(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	firstTopicID, secondTopicID, sourceID := uuid.New(), uuid.New(), uuid.New()
	shared := digestItem(workspaceID, sourceID, "Shared")
	firstOnly := digestItem(workspaceID, sourceID, "First")
	secondOnly := digestItem(workspaceID, sourceID, "Second")
	secondBackfill := digestItem(workspaceID, sourceID, "Second backfill")
	consumed := digestItem(workspaceID, sourceID, "Already used")
	now := time.Date(2026, time.September, 28, 10, 0, 0, 0, time.UTC)
	queue := newJobQueue(t)
	eventService := events.NewForWorkspace(uuid.Nil, nil)
	eventContext, stopEvents := context.WithCancel(t.Context())
	defer stopEvents()
	notifications := eventService.Subscribe(eventContext)
	repository := mocks.NewMockRepository(gomock.NewController(t))
	var savedDefinition domain.DigestDefinition
	repository.EXPECT().CreateDigest(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, value domain.DigestDefinition) error { savedDefinition = value; return nil })
	repository.EXPECT().GetDigest(gomock.Any(), workspaceID, gomock.Any()).DoAndReturn(func(_ context.Context, _ uuid.UUID, id uuid.UUID) (domain.DigestDefinition, bool, error) {
		require.Equal(t, savedDefinition.ID, id)
		return savedDefinition, true, nil
	}).AnyTimes()
	repository.EXPECT().FindConsumedDigestItemIDs(gomock.Any(), workspaceID, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ uuid.UUID, candidates []uuid.UUID) ([]uuid.UUID, error) {
			require.ElementsMatch(t, []uuid.UUID{consumed.ID, shared.ID, firstOnly.ID, secondOnly.ID, secondBackfill.ID}, candidates)
			return []uuid.UUID{consumed.ID}, nil
		})
	writtenIssues := make(chan domain.DigestIssue, 1)
	repository.EXPECT().PublishDigestIssue(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, command domain.DigestPublication) (domain.DigestPublicationResult, error) {
		issue := command.Issue
		require.NotEmpty(t, command.LeaseOwner)
		require.Positive(t, command.Attempt)
		require.NotEqual(t, uuid.Nil, issue.GenerationJobID)
		writtenIssues <- issue
		return domain.DigestPublicationResult{Issue: issue, Created: true}, nil
	})
	ranker := &digestRankerStub{results: map[uuid.UUID]ranking.TopicResult{
		firstTopicID: {
			Candidates: []ranking.Candidate{{ItemID: consumed.ID.String()}, {ItemID: shared.ID.String()}, {ItemID: firstOnly.ID.String()}},
			Inputs:     ranking.RankingInputs{PipelineVersion: ranking.PipelineVersion},
		},
		secondTopicID: {
			Candidates: []ranking.Candidate{
				{ItemID: consumed.ID.String()},
				{ItemID: secondOnly.ID.String()}, {ItemID: shared.ID.String()}, {ItemID: secondBackfill.ID.String()},
			},
			Inputs: ranking.RankingInputs{PipelineVersion: ranking.PipelineVersion},
		},
	}}
	settings := defaultDigestSettings()
	service := newDigestService(t, digests.Config{
		WorkspaceID: workspaceID, Repository: repository,
		Items: digestItems(t, []domain.Item{shared, firstOnly, secondOnly, secondBackfill}),
		Topics: digestTopics(t, []domain.Topic{
			{ID: firstTopicID, WorkspaceID: workspaceID, Name: "First topic", Enabled: true, Revision: 2},
			{ID: secondTopicID, WorkspaceID: workspaceID, Name: "Second topic", Enabled: true, Revision: 3},
		}),
		Ranker: ranker, Settings: settings,
		Jobs: queue, Events: eventService, Now: func() time.Time { return now },
	})
	definition, err := service.Create(t.Context(), digests.Input{
		Name: "Research", Topics: []domain.DigestTopic{
			{TopicID: firstTopicID, ItemLimit: 2}, {TopicID: secondTopicID, ItemLimit: 2},
		},
		Timezone: "UTC", Recurrence: domain.DigestRecurrence{Frequency: domain.RecurrenceDaily, Hour: 12}, Enabled: true,
	})
	require.NoError(t, err)
	err = queue.seedDigest(t, workspaceID, definition.ID)
	require.NoError(t, err)
	require.NoError(t, queue.run(t, 0))
	issues := []domain.DigestIssue{testutil.Receive(t, writtenIssues)}
	require.Len(t, issues, 1)
	require.Equal(t, domain.DigestIssueComplete, issues[0].Status)
	require.Equal(t, []uuid.UUID{shared.ID, firstOnly.ID}, digestItemIDs(issues[0].Sections[0]))
	require.Equal(t, []uuid.UUID{secondOnly.ID, secondBackfill.ID}, digestItemIDs(issues[0].Sections[1]))
	require.Equal(t, 2, issues[0].Sections[0].TopicRevision)
	require.Equal(t, 1, settings.calls)
	require.Len(t, ranker.requests, 2)
	for _, request := range ranker.requests {
		require.Equal(t, 50, request.Limit)
		require.Same(t, ranker.requests[0].WorkspaceSettings, request.WorkspaceSettings)
	}
	select {
	case notification := <-notifications:
		require.Equal(t, events.TypeDigestReady, notification.Type)
		require.Equal(t, events.DigestReady{IssueID: issues[0].ID}, notification.Data)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for digestReady")
	}
}

func TestGenerationPublishesDegradedPartialOnlyAfterRetries(t *testing.T) {
	t.Parallel()

	workspaceID, firstTopicID, secondTopicID, sourceID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	item := digestItem(workspaceID, sourceID, "Available branch")
	queue := newJobQueue(t)
	repository := mocks.NewMockRepository(gomock.NewController(t))
	var savedDefinition domain.DigestDefinition
	repository.EXPECT().CreateDigest(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, value domain.DigestDefinition) error { savedDefinition = value; return nil })
	repository.EXPECT().GetDigest(gomock.Any(), workspaceID, gomock.Any()).DoAndReturn(func(_ context.Context, _ uuid.UUID, id uuid.UUID) (domain.DigestDefinition, bool, error) {
		require.Equal(t, savedDefinition.ID, id)
		return savedDefinition, true, nil
	}).AnyTimes()
	repository.EXPECT().FindConsumedDigestItemIDs(gomock.Any(), workspaceID, gomock.Any(), []uuid.UUID{item.ID}).Return(nil, nil).Times(3)
	writtenIssues := make(chan domain.DigestIssue, 1)
	repository.EXPECT().PublishDigestIssue(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, command domain.DigestPublication) (domain.DigestPublicationResult, error) {
		issue := command.Issue
		require.NotEmpty(t, command.LeaseOwner)
		require.Positive(t, command.Attempt)
		writtenIssues <- issue
		return domain.DigestPublicationResult{Issue: issue, Created: true}, nil
	})
	service := newDigestService(t, digests.Config{
		WorkspaceID: workspaceID, Repository: repository, Items: digestItems(t, []domain.Item{item}),
		Topics: digestTopics(t, []domain.Topic{
			{ID: firstTopicID, WorkspaceID: workspaceID, Name: "Working", Enabled: true, Revision: 1},
			{ID: secondTopicID, WorkspaceID: workspaceID, Name: "Failing", Enabled: true, Revision: 1},
		}),
		Ranker: &digestRankerStub{
			results: map[uuid.UUID]ranking.TopicResult{
				firstTopicID: {Candidates: []ranking.Candidate{{ItemID: item.ID.String()}}},
			},
			errors: map[uuid.UUID]error{secondTopicID: errors.New("retrieval unavailable")},
		},
		Jobs: queue, Events: events.NewForWorkspace(uuid.Nil, nil), Now: time.Now,
	})
	definition, err := service.Create(t.Context(), digests.Input{
		Name: "Partial", Topics: []domain.DigestTopic{
			{TopicID: firstTopicID, ItemLimit: 5}, {TopicID: secondTopicID, ItemLimit: 5},
		},
		Timezone: "UTC", Recurrence: domain.DigestRecurrence{Frequency: domain.RecurrenceDaily, Hour: 12}, Enabled: true,
	})
	require.NoError(t, err)
	err = queue.seedDigest(t, workspaceID, definition.ID)
	require.NoError(t, err)
	for attempt := 1; attempt <= 3; attempt++ {
		finish := runDigestAttempt(t, queue, attempt)
		expected := domain.JobStatusRetryWait
		if attempt == 3 {
			expected = domain.JobStatusSucceeded
		}
		require.Equal(t, expected, finish.Status)
		if attempt < 3 {
			require.Empty(t, writtenIssues)
		}
	}
	issues := []domain.DigestIssue{testutil.Receive(t, writtenIssues)}
	require.Len(t, issues, 1)
	require.Equal(t, domain.DigestIssueDegraded, issues[0].Status)
	require.Nil(t, issues[0].Sections[0].Failure)
	require.Contains(t, *issues[0].Sections[1].Failure, "retrieval unavailable")
}

type digestConsumptionScenario struct {
	name        string
	branches    int
	lookupFails bool
}

func TestGenerationBoundsConsumptionLookupsAndStopsOnLookupFailure(t *testing.T) {
	for _, scenario := range []digestConsumptionScenario{{"empty candidates", 0, false}, {"many consumed candidates", 11, false}, {"lookup failure", 1, true}} {
		t.Run(scenario.name, func(t *testing.T) { runDigestConsumptionScenario(t, scenario) })
	}
}

func runDigestConsumptionScenario(t *testing.T, scenario digestConsumptionScenario) {
	t.Helper()
	workspaceID := uuid.New()
	definition, topics, ranker, expected := digestConsumptionCandidates(workspaceID, scenario.branches)
	repository := mocks.NewMockRepository(gomock.NewController(t))
	repository.EXPECT().GetDigest(gomock.Any(), workspaceID, definition.ID).Return(definition, true, nil).AnyTimes()
	lookupError := errors.New("consumption unavailable")
	seen := make(map[uuid.UUID]bool)
	if scenario.branches > 0 {
		repository.EXPECT().FindConsumedDigestItemIDs(gomock.Any(), workspaceID, definition.ID, gomock.Any()).DoAndReturn(
			func(_ context.Context, _, _ uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
				require.LessOrEqual(t, len(ids), 1000)
				recordConsumedCandidates(t, ids, expected, seen)
				if scenario.lookupFails {
					return nil, lookupError
				}
				return ids, nil
			}).Times((len(expected) + 999) / 1000)
	}
	if !scenario.lookupFails {
		repository.EXPECT().PublishDigestIssue(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, command domain.DigestPublication) (domain.DigestPublicationResult, error) {
			issue := command.Issue
			require.NotEmpty(t, command.LeaseOwner)
			require.Positive(t, command.Attempt)

			require.Len(t, issue.Sections, len(topics))
			for _, section := range issue.Sections {
				require.Empty(t, section.Items)
			}
			return domain.DigestPublicationResult{Issue: issue, Created: true}, nil
		})
	}
	queue := newJobQueue(t)
	settings := defaultDigestSettings()
	settings.value.Retrieval.CandidateLimit = 100
	newDigestService(t, digests.Config{WorkspaceID: workspaceID, Repository: repository, Topics: digestTopics(t, topics),
		Items: digestItems(t, nil), Ranker: ranker, Jobs: queue, Events: events.NewForWorkspace(uuid.Nil, nil), Settings: settings, Now: time.Now})
	err := queue.seedDigest(t, workspaceID, definition.ID)
	require.NoError(t, err)
	err = queue.run(t, 0)
	if scenario.lookupFails {
		require.ErrorIs(t, err, lookupError)
	} else {
		require.NoError(t, err)
		require.Len(t, seen, len(expected))
	}
	require.Len(t, ranker.requests, len(topics), "consumption filtering never refills ranking branches")
}

func digestConsumptionCandidates(workspaceID uuid.UUID, branches int) (domain.DigestDefinition, []domain.Topic, *digestRankerStub, map[uuid.UUID]bool) {
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: workspaceID, Name: "History", Timezone: "UTC", Enabled: true}
	var topics []domain.Topic
	ranker := &digestRankerStub{results: make(map[uuid.UUID]ranking.TopicResult)}
	expected := make(map[uuid.UUID]bool)
	for range max(1, branches) {
		topic := domain.Topic{ID: uuid.New(), WorkspaceID: workspaceID, Enabled: true}
		topics = append(topics, topic)
		definition.Topics = append(definition.Topics, domain.DigestTopic{TopicID: topic.ID, ItemLimit: 1})
		var candidates []ranking.Candidate
		if branches > 0 {
			for range 100 {
				id := uuid.New()
				expected[id] = true
				candidates = append(candidates, ranking.Candidate{ItemID: id.String()})
			}
		}
		ranker.results[topic.ID] = ranking.TopicResult{Candidates: candidates}
	}
	return definition, topics, ranker, expected
}

func recordConsumedCandidates(t *testing.T, ids []uuid.UUID, expected, seen map[uuid.UUID]bool) {
	t.Helper()
	for _, id := range ids {
		require.True(t, expected[id])
		require.False(t, seen[id])
		seen[id] = true
	}
}

func digestItem(workspaceID, sourceID uuid.UUID, title string) domain.Item {
	return domain.Item{
		ID: uuid.New(), WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{sourceID},
		Kind: domain.ItemKindArticle, Details: domain.ItemDetails{
			Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{},
		},
		Title: title, Excerpt: title + " summary", ReadingState: domain.ReadingStateUnread,
		Visibility: domain.ItemVisibilityVisible,
	}
}

func digestItemIDs(section domain.DigestSection) []uuid.UUID {
	result := make([]uuid.UUID, len(section.Items))
	for index, item := range section.Items {
		result[index] = item.Item.ID
	}
	return result
}

func newDigestService(t *testing.T, config digests.Config) *digests.Service {
	t.Helper()
	if repository, ok := config.Repository.(*mocks.MockRepository); ok {
		repository.EXPECT().GetDigestIssueForJob(gomock.Any(), gomock.Any(), gomock.Any()).Return(domain.DigestIssue{}, false, nil).AnyTimes()
	}
	if config.Settings == nil {
		config.Settings = defaultDigestSettings()
	}
	if config.Summaries == nil {
		config.Summaries = mocks.NewMockItemSummaries(gomock.NewController(t))
	}
	service, err := digests.New(config)
	require.NoError(t, err)
	return service
}

func digestTopics(t *testing.T, topics []domain.Topic) *mocks.MockTopics {
	t.Helper()
	repository := mocks.NewMockTopics(gomock.NewController(t))
	for _, topic := range topics {
		repository.EXPECT().GetTopic(gomock.Any(), topic.WorkspaceID, topic.ID).Return(topic, true, nil).AnyTimes()
	}
	return repository
}
func digestItems(t *testing.T, items []domain.Item) *mocks.MockItems {
	t.Helper()
	repository := mocks.NewMockItems(gomock.NewController(t))
	for _, item := range items {
		repository.EXPECT().GetItem(gomock.Any(), item.WorkspaceID, item.ID).Return(item, true, nil).AnyTimes()
	}
	return repository
}
func runDigestAttempt(t *testing.T, queue *jobQueueFixture, attempt int) domain.JobFinish {
	t.Helper()
	finish, _ := executeDigestAttempt(t, queue, 0, attempt)
	return finish
}

// executeDigestAttempt runs the real worker so tests receive authentic execution fencing context.
func executeDigestAttempt(t *testing.T, queue *jobQueueFixture, index, attempt int) (domain.JobFinish, error) {
	t.Helper()
	request := queue.requests[index]
	payload, err := json.Marshal(request.Arguments)
	require.NoError(t, err)
	job := domain.Job{ID: queue.jobIDs[index], WorkspaceID: request.WorkspaceID, Kind: request.Definition.Kind, Queue: request.Definition.Queue, ArgumentsVersion: request.Definition.ArgumentsVersion, Arguments: payload, Attempt: attempt, MaxAttempts: 3}
	repository := jobmocks.NewMockRepository(gomock.NewController(t))
	var claimed atomic.Bool
	repository.EXPECT().Claim(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request domain.JobClaimRequest) (domain.JobClaim, error) {
		if request.Queue == job.Queue && claimed.CompareAndSwap(false, true) {
			return domain.JobClaim{Job: job}, nil
		}
		return domain.JobClaim{}, nil
	}).AnyTimes()
	repository.EXPECT().Heartbeat(gomock.Any(), gomock.Any()).Return(false, true, nil).AnyTimes()
	finished := make(chan domain.JobFinish, 1)
	repository.EXPECT().Finish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, finish domain.JobFinish) (bool, error) { finished <- finish; return true, nil })
	worker := jobs.New(repository, time.Now, jobs.Config{PollInterval: time.Millisecond})
	handlerErrors := make(chan error, 1)
	worker.RegisterDefinition(request.Definition, func(ctx context.Context, payload json.RawMessage) error {
		err := queue.handlers[request.Definition.Kind](ctx, payload)
		handlerErrors <- err
		return err
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, testutil.Receive(t, done)) })
	finish := testutil.Receive(t, finished)
	cancel()
	return finish, <-handlerErrors
}
