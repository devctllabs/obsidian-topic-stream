package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type advisoryPublisherSpy struct {
	values []domain.Advisory
}

func TestWorkspaceNotificationsForwardOtherWorkspacesWithoutPublishingLocally(t *testing.T) {
	t.Parallel()

	for _, kind := range []domain.AdvisoryKind{
		domain.AdvisoryFeedInvalidated, domain.AdvisoryDigestReady, domain.AdvisoryJobChanged,
	} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			workspaceID, otherWorkspaceID, resourceID := uuid.New(), uuid.New(), uuid.New()
			publisher := &advisoryPublisherSpy{}
			service := events.NewForWorkspace(workspaceID, publisher)
			subscription := service.Subscribe(t.Context())
			switch kind {
			case domain.AdvisoryFeedInvalidated:
				service.InvalidateFeed(otherWorkspaceID, resourceID)
			case domain.AdvisoryDigestReady:
				service.NotifyDigestReady(otherWorkspaceID, resourceID)
			case domain.AdvisoryJobChanged:
				service.NotifyJobChanged(otherWorkspaceID, resourceID)
			}

			require.Len(t, publisher.values, 1)
			require.Equal(t, otherWorkspaceID, publisher.values[0].WorkspaceID)
			require.Equal(t, resourceID, publisher.values[0].ResourceID)
			require.Equal(t, kind, publisher.values[0].Kind)
			select {
			case <-subscription:
				t.Fatal("another workspace's local notification must not reach this SSE subscription")
			default:
			}
		})
	}
}

// PublishAdvisory records advisories for assertions without cross-process delivery.
func (s *advisoryPublisherSpy) PublishAdvisory(value domain.Advisory) {
	s.values = append(s.values, value)
}

func TestWorkspaceAdvisoryPublishesLocallyAndToTransportWithoutEchoingOrigin(t *testing.T) {
	t.Parallel()

	workspaceID, jobID := uuid.New(), uuid.New()
	publisher := &advisoryPublisherSpy{}
	service := events.NewForWorkspace(workspaceID, publisher)
	subscription := service.Subscribe(t.Context())

	service.NotifyJobChanged(workspaceID, jobID)

	local := testutil.Receive(t, subscription)
	require.Equal(t, events.Event{Type: events.TypeJobChanged, Data: events.JobChanged{JobID: jobID}}, local)
	require.Len(t, publisher.values, 1)
	require.Equal(t, domain.AdvisoryJobChanged, publisher.values[0].Kind)
	require.Equal(t, workspaceID, publisher.values[0].WorkspaceID)
	require.Equal(t, jobID, publisher.values[0].ResourceID)
	require.NotEqual(t, uuid.Nil, publisher.values[0].Origin)

	service.AcceptAdvisory(publisher.values[0])
	select {
	case <-subscription:
		t.Fatal("the transport echo from this process must not duplicate the local event")
	default:
	}

	service.AcceptAdvisory(domain.Advisory{
		Kind: domain.AdvisoryJobChanged, WorkspaceID: uuid.New(), ResourceID: uuid.New(), Origin: uuid.New(),
	})
	select {
	case <-subscription:
		t.Fatal("an advisory from another workspace must not reach this SSE service")
	default:
	}
}

func TestPublishBroadcastsAdvisoryEventsWithoutReplay(t *testing.T) {
	t.Parallel()

	service := events.NewForWorkspace(uuid.Nil, nil)
	workspaceID := uuid.New()
	service.InvalidateFeed(workspaceID, uuid.New())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	subscription := service.Subscribe(ctx)

	select {
	case <-subscription:
		t.Fatal("events published before subscription must not be replayed")
	default:
	}

	itemID := uuid.New()
	service.InvalidateFeed(workspaceID, itemID)

	select {
	case event := <-subscription:
		require.Equal(t, events.TypeFeedInvalidated, event.Type)
		require.Equal(t, events.FeedInvalidated{ItemID: itemID}, event.Data)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestNotifyJobChangedCarriesOnlyTheAuthoritativeJobID(t *testing.T) {
	t.Parallel()

	service := events.NewForWorkspace(uuid.Nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	subscription := service.Subscribe(ctx)
	jobID := uuid.New()

	service.NotifyJobChanged(uuid.New(), jobID)

	event := testutil.Receive(t, subscription)
	require.Equal(t, events.TypeJobChanged, event.Type)
	require.Equal(t, events.JobChanged{JobID: jobID}, event.Data)
}

func TestSlowSubscriberDoesNotBlockAdvisoryPublishing(t *testing.T) {
	t.Parallel()

	service := events.NewForWorkspace(uuid.Nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_ = service.Subscribe(ctx)

	require.NotPanics(t, func() {
		for range 100 {
			service.InvalidateFeed(uuid.New(), uuid.New())
		}
	})
}

func TestDisconnectClosesCurrentSubscriptionsWithoutAffectingNewOnes(t *testing.T) {
	t.Parallel()

	service := events.NewForWorkspace(uuid.Nil, nil)
	current := service.Subscribe(t.Context())

	service.Disconnect()

	_, open := <-current
	require.False(t, open)
	next := service.Subscribe(t.Context())
	service.NotifyJobChanged(uuid.New(), uuid.New())
	_, open = <-next
	require.True(t, open)
}
