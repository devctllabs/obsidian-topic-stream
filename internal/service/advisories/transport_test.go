package advisories_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/advisories"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type advisoryBackendStub struct {
	listen     func(context.Context, func() error, func(domain.Advisory) error) error
	mu         sync.Mutex
	listens    int
	first      []domain.Advisory
	published  chan domain.Advisory
	secondCall chan struct{}
}

// Publish records the advisory on the fixture's publication channel.
func (s *advisoryBackendStub) Publish(_ context.Context, value domain.Advisory) error {
	s.published <- value
	return nil
}

// Listen runs the configured listener behavior or simulates the first delivery gap and recovery.
func (s *advisoryBackendStub) Listen(ctx context.Context, onReady func() error, accept func(domain.Advisory) error) error {
	if s.listen != nil {
		return s.listen(ctx, onReady, accept)
	}
	if err := onReady(); err != nil {
		return err
	}
	s.mu.Lock()
	s.listens++
	call := s.listens
	s.mu.Unlock()
	if call == 1 {
		for _, value := range s.first {
			if err := accept(value); err != nil {
				return err
			}
		}
		return errors.New("listener connection lost")
	}
	close(s.secondCall)
	<-ctx.Done()
	return ctx.Err()
}

type cacheSpy struct {
	reloads   chan domain.Advisory
	reloadAll chan struct{}
}

// Reload records a Workspace/version cache invalidation for assertions.
func (s *cacheSpy) Reload(_ context.Context, workspaceID uuid.UUID, version int) error {
	s.reloads <- domain.Advisory{WorkspaceID: workspaceID, Version: version}
	return nil
}

// ReloadCached signals a full cache recovery request for assertions.
func (s *cacheSpy) ReloadCached(context.Context) error {
	s.reloadAll <- struct{}{}
	return nil
}

type eventSinkSpy struct {
	accepted     chan domain.Advisory
	disconnected chan struct{}
}

// AcceptAdvisory records a remotely accepted advisory for assertions.
func (s *eventSinkSpy) AcceptAdvisory(value domain.Advisory) { s.accepted <- value }

// Disconnect signals that live subscribers were disconnected.
func (s *eventSinkSpy) Disconnect() { s.disconnected <- struct{}{} }

func TestTransportRoutesAdvisoriesAndRecoversListenerGaps(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	settingsChanged := domain.Advisory{
		Kind: domain.AdvisorySettingsChanged, WorkspaceID: workspaceID, Version: 3,
	}
	remoteJobChanged := domain.Advisory{
		Kind: domain.AdvisoryJobChanged, WorkspaceID: workspaceID, ResourceID: uuid.New(), Origin: uuid.New(),
	}
	backend := &advisoryBackendStub{
		first:     []domain.Advisory{settingsChanged, remoteJobChanged},
		published: make(chan domain.Advisory, 1), secondCall: make(chan struct{}),
	}
	cache := &cacheSpy{reloads: make(chan domain.Advisory, 1), reloadAll: make(chan struct{}, 1)}
	events := &eventSinkSpy{accepted: make(chan domain.Advisory, 1), disconnected: make(chan struct{}, 1)}
	queue := advisories.NewPublishQueue()
	transport, err := advisories.New(advisories.Config{
		Backend: backend, Settings: cache, Events: events, Queue: queue, RetryDelay: time.Millisecond,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- transport.Run(ctx) }()

	testutil.Receive(t, cache.reloadAll)
	testutil.Receive(t, events.disconnected)
	reloaded := testutil.Receive(t, cache.reloads)
	require.Equal(t, workspaceID, reloaded.WorkspaceID)
	require.Equal(t, 3, reloaded.Version)
	require.Equal(t, remoteJobChanged, testutil.Receive(t, events.accepted))
	testutil.Receive(t, events.disconnected)
	testutil.Receive(t, cache.reloadAll)
	testutil.Receive(t, events.disconnected)
	testutil.Receive(t, backend.secondCall)

	jobChanged := domain.Advisory{
		Kind: domain.AdvisoryJobChanged, WorkspaceID: workspaceID, ResourceID: uuid.New(), Origin: uuid.New(),
	}
	queue.PublishAdvisory(jobChanged)
	require.Equal(t, jobChanged, testutil.Receive(t, backend.published))

	cancel()
	require.NoError(t, testutil.Receive(t, done))
}
