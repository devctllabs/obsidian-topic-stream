package advisories_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/advisories"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	settingsmocks "github.com/ethernity/obsidian-topic-stream/internal/service/settings/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func startTransport(t *testing.T, config advisories.Config) (context.CancelFunc, <-chan error) {
	t.Helper()
	config.Queue = advisories.NewPublishQueue()
	if config.RetryDelay == 0 {
		config.RetryDelay = time.Millisecond
	}
	transport, err := advisories.New(config)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- transport.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, testutil.Receive(t, done))
	})
	return cancel, done
}

func waitForSignal(ctx context.Context, signal <-chan struct{}) error {
	select {
	case <-signal:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func requireDisconnected(t *testing.T, subscription <-chan events.Event) {
	t.Helper()
	select {
	case _, open := <-subscription:
		require.False(t, open, "the SSE subscription must be closed")
	default:
		t.Fatal("the SSE subscription is still open")
	}
}

func TestTransportRecoversSettingsAndSSEAfterSubscriptionIsReady(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	repository := settingsmocks.NewMockCacheRepository(gomock.NewController(t))
	originalValue := settings.DefaultWorkspaceSettings(time.Now())
	updatedValue := originalValue
	updatedValue.Version++
	updatedValue.Retrieval.CandidateLimit++
	missedValue := updatedValue
	missedValue.Version++
	missedValue.Retrieval.CandidateLimit++
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), workspaceID, gomock.Any()).Return(originalValue, nil)
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), workspaceID, gomock.Any()).Return(updatedValue, nil)
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), workspaceID, gomock.Any()).Return(missedValue, nil)
	cache := settings.NewCache(repository, time.Now)
	original, err := cache.Snapshot(t.Context(), workspaceID)
	require.NoError(t, err)
	updated := original
	updated.Version++
	updated.Retrieval.CandidateLimit++
	sink := events.NewForWorkspace(workspaceID, nil)
	initialSubscriber := sink.Subscribe(t.Context())
	ready := make(chan int, 2)
	lost := make(chan struct{})
	reconnecting := make(chan struct{})
	resume := make(chan struct{})
	attempt := 0
	backend := &advisoryBackendStub{listen: func(
		ctx context.Context, onReady func() error, _ func(domain.Advisory) error,
	) error {
		attempt++
		if attempt == 2 {
			close(reconnecting)
			if err := waitForSignal(ctx, resume); err != nil {
				return err
			}
		}
		if err := onReady(); err != nil {
			return err
		}
		ready <- attempt
		if attempt == 1 {
			if err := waitForSignal(ctx, lost); err != nil {
				return err
			}
			return errors.New("listener connection lost")
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	startTransport(t, advisories.Config{Backend: backend, Settings: cache, Events: sink})

	require.Equal(t, 1, testutil.Receive(t, ready))
	observed, err := cache.Snapshot(t.Context(), workspaceID)
	require.NoError(t, err)
	require.Equal(t, updated, observed, "startup must repair settings loaded before LISTEN")
	requireDisconnected(t, initialSubscriber)

	connectedSubscriber := sink.Subscribe(t.Context())
	close(lost)
	testutil.Receive(t, reconnecting)
	requireDisconnected(t, connectedSubscriber)
	duringGap := sink.Subscribe(t.Context())
	missed := updated
	missed.Version++
	missed.Retrieval.CandidateLimit++
	close(resume)

	require.Equal(t, 2, testutil.Receive(t, ready))
	observed, err = cache.Snapshot(t.Context(), workspaceID)
	require.NoError(t, err)
	require.Equal(t, missed, observed, "recovery must read changes made before resubscription")
	requireDisconnected(t, duringGap)
	next := sink.Subscribe(t.Context())
	jobID := uuid.New()
	sink.NotifyJobChanged(workspaceID, jobID)
	require.Equal(t, events.JobChanged{JobID: jobID}, testutil.Receive(t, next).Data)
}

func TestTransportRetriesFailedSettingsInvalidationWithoutAnotherNotification(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	repository := settingsmocks.NewMockCacheRepository(gomock.NewController(t))
	originalValue := settings.DefaultWorkspaceSettings(time.Now())
	updatedValue := originalValue
	updatedValue.Version++
	updatedValue.Retrieval.CandidateLimit++
	failureValue := errors.New("temporary settings read failure")
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), workspaceID, gomock.Any()).Return(originalValue, nil).Times(2)
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), workspaceID, gomock.Any()).Return(domain.WorkspaceSettings{}, failureValue)
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), workspaceID, gomock.Any()).Return(updatedValue, nil)
	cache := settings.NewCache(repository, time.Now)
	original, err := cache.Snapshot(t.Context(), workspaceID)
	require.NoError(t, err)
	updated := original
	updated.Version++
	updated.Retrieval.CandidateLimit++
	failure := failureValue
	processed := make(chan error, 1)
	recovered := make(chan struct{})
	attempt := 0
	backend := &advisoryBackendStub{listen: func(
		ctx context.Context, onReady func() error, accept func(domain.Advisory) error,
	) error {
		attempt++
		if err := onReady(); err != nil {
			return err
		}
		if attempt == 1 {
			err := accept(domain.Advisory{
				Kind: domain.AdvisorySettingsChanged, WorkspaceID: workspaceID, Version: updated.Version,
			})
			processed <- err
			if err != nil {
				return err
			}
		} else {
			close(recovered)
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	startTransport(t, advisories.Config{Backend: backend, Settings: cache, Events: events.NewForWorkspace(workspaceID, nil)})

	require.ErrorIs(t, testutil.Receive(t, processed), failure)
	testutil.Receive(t, recovered)
	observed, err := cache.Snapshot(t.Context(), workspaceID)
	require.NoError(t, err)
	require.Equal(t, updated, observed)
}

func TestTransportRetriesFailedRecoveryKeepingLastSnapshot(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	repository := settingsmocks.NewMockCacheRepository(gomock.NewController(t))
	originalValue := settings.DefaultWorkspaceSettings(time.Now())
	updatedValue := originalValue
	updatedValue.Version++
	failureValue := errors.New("temporary recovery failure")
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), workspaceID, gomock.Any()).Return(originalValue, nil)
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), workspaceID, gomock.Any()).Return(domain.WorkspaceSettings{}, failureValue).Times(2)
	repository.EXPECT().GetOrCreateWorkspaceSettings(gomock.Any(), workspaceID, gomock.Any()).Return(updatedValue, nil)
	cache := settings.NewCache(repository, time.Now)
	original, err := cache.Snapshot(t.Context(), workspaceID)
	require.NoError(t, err)
	updated := original
	updated.Version++
	failure := failureValue
	permits := make(chan struct{}, 1)
	results := make(chan error, 1)
	attempt := 0
	backend := &advisoryBackendStub{listen: func(
		ctx context.Context, onReady func() error, _ func(domain.Advisory) error,
	) error {
		if err := waitForSignal(ctx, permits); err != nil {
			return err
		}
		attempt++
		err := onReady()
		results <- err
		if err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	startTransport(t, advisories.Config{Backend: backend, Settings: cache, Events: events.NewForWorkspace(workspaceID, nil)})

	for range 2 {
		permits <- struct{}{}
		require.ErrorIs(t, testutil.Receive(t, results), failure)
		observed, err := cache.Snapshot(t.Context(), workspaceID)
		require.NoError(t, err)
		require.Equal(t, original, observed)
	}
	permits <- struct{}{}
	require.NoError(t, testutil.Receive(t, results))
	observed, err := cache.Snapshot(t.Context(), workspaceID)
	require.NoError(t, err)
	require.Equal(t, updated, observed)
}

func TestTransportResetsBackoffAfterSuccessfulRecovery(t *testing.T) {
	t.Parallel()

	attempts := make(chan int, 1)
	ready := make(chan struct{})
	lost := make(chan struct{})
	attempt := 0
	backend := &advisoryBackendStub{listen: func(
		ctx context.Context, onReady func() error, _ func(domain.Advisory) error,
	) error {
		attempt++
		attempts <- attempt
		if attempt <= 3 {
			return errors.New("listener unavailable")
		}
		if err := onReady(); err != nil {
			return err
		}
		if attempt == 4 {
			close(ready)
			if err := waitForSignal(ctx, lost); err != nil {
				return err
			}
			return errors.New("listener lost after recovery")
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	startTransport(t, advisories.Config{
		Backend: backend, Settings: settings.NewCache(settingsmocks.NewMockCacheRepository(gomock.NewController(t)), time.Now),
		Events: events.NewForWorkspace(uuid.Nil, nil), RetryDelay: 100 * time.Millisecond,
	})
	for expected := 1; expected <= 4; expected++ {
		require.Equal(t, expected, testutil.Receive(t, attempts))
	}
	testutil.Receive(t, ready)
	close(lost)

	// Recovery must restore the 100 ms delay instead of retaining the previous 800 ms backoff.
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case attempt := <-attempts:
		require.Equal(t, 5, attempt)
	case <-timer.C:
		t.Fatal("successful recovery did not reset listener backoff")
	}
}

func TestTransportCancellationInterruptsRetry(t *testing.T) {
	t.Parallel()

	sink := &eventSinkSpy{disconnected: make(chan struct{}, 1)}
	backend := &advisoryBackendStub{listen: func(
		context.Context, func() error, func(domain.Advisory) error,
	) error {
		return errors.New("listener unavailable")
	}}
	cancel, done := startTransport(t, advisories.Config{
		Backend: backend, Settings: settings.NewCache(settingsmocks.NewMockCacheRepository(gomock.NewController(t)), time.Now),
		Events: sink, RetryDelay: 30 * time.Second,
	})
	testutil.Receive(t, sink.disconnected)
	cancel()
	require.NoError(t, testutil.Receive(t, done))
}
