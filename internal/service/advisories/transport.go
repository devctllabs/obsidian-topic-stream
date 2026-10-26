package advisories

import (
	"context"
	"errors"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

const (
	defaultBufferSize = 256
	maximumRetryDelay = 30 * time.Second
)

// Backend sends and receives best-effort PostgreSQL advisories.
type Backend interface {
	// Publish sends a best-effort advisory; durable state remains authoritative.
	Publish(ctx context.Context, value domain.Advisory) error
	// Listen calls onReady once after subscribing, then accepts advisories sequentially.
	// Callback errors end the attempt; all blocking work must respect ctx cancellation.
	Listen(ctx context.Context, onReady func() error, accept func(value domain.Advisory) error) error
}

// SettingsCache refreshes cached settings after advisories or listener delivery gaps.
type SettingsCache interface {
	// Reload refreshes a Workspace snapshot when its cached version is older than version.
	Reload(ctx context.Context, workspaceID uuid.UUID, version int) error
	// ReloadCached refreshes observed Workspaces, preserving snapshots for failed loads.
	ReloadCached(ctx context.Context) error
}

// EventSink delivers remote advisories and resets subscriptions after delivery gaps.
type EventSink interface {
	// AcceptAdvisory delivers a received advisory without publishing it again.
	AcceptAdvisory(value domain.Advisory)
	// Disconnect closes current subscriptions so clients reconnect and refetch REST state.
	Disconnect()
}

// Config supplies advisory transport, cache recovery, event delivery, and reconnect timing.
type Config struct {
	Backend    Backend
	Settings   SettingsCache
	Events     EventSink
	Queue      *PublishQueue
	RetryDelay time.Duration
}

// PublishQueue is an in-memory, lossy buffer that keeps producers independent of network I/O.
type PublishQueue struct {
	values chan domain.Advisory
}

// NewPublishQueue creates the bounded outbox used for best-effort advisory delivery.
func NewPublishQueue() *PublishQueue {
	return &PublishQueue{values: make(chan domain.Advisory, defaultBufferSize)}
}

// PublishAdvisory is deliberately non-blocking because advisories never replace durable state.
func (q *PublishQueue) PublishAdvisory(value domain.Advisory) {
	select {
	case q.values <- value:
	default:
	}
}

// Transport forwards best-effort advisories and repairs listener gaps by refreshing cached state.
type Transport struct {
	backend    Backend
	settings   SettingsCache
	events     EventSink
	queue      *PublishQueue
	retryDelay time.Duration
}

// New validates advisory transport dependencies and selects the reconnect delay.
func New(config Config) (*Transport, error) {
	if config.Backend == nil || config.Settings == nil || config.Events == nil || config.Queue == nil {
		return nil, errors.New("advisory transport dependencies are required")
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = time.Second
	}
	return &Transport{
		backend: config.Backend, settings: config.Settings, events: config.Events,
		queue: config.Queue, retryDelay: config.RetryDelay,
	}, nil
}

// Run supervises advisory publishing and listening until cancellation, reconnecting after listener
// failures.
func (t *Transport) Run(ctx context.Context) error {
	group, groupContext := errgroup.WithContext(ctx)
	group.Go(func() error { return t.publishLoop(groupContext) })
	group.Go(func() error { return t.listenLoop(groupContext) })
	return group.Wait()
}

func (t *Transport) publishLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case value := <-t.queue.values:
			_ = t.backend.Publish(ctx, value)
		}
	}
}

func (t *Transport) listenLoop(ctx context.Context) error {
	failures := 0
	for {
		_ = t.backend.Listen(ctx, func() error {
			if err := t.settings.ReloadCached(ctx); err != nil {
				return err
			}
			t.events.Disconnect()
			failures = 0
			return nil
		}, func(value domain.Advisory) error {
			return t.accept(ctx, value)
		})
		if ctx.Err() != nil {
			return nil
		}
		t.events.Disconnect()
		failures++
		if !waitForRetry(ctx, retryDelay(t.retryDelay, failures)) {
			return nil
		}
	}
}

func (t *Transport) accept(ctx context.Context, value domain.Advisory) error {
	if value.Kind == domain.AdvisorySettingsChanged {
		return t.settings.Reload(ctx, value.WorkspaceID, value.Version)
	}
	t.events.AcceptAdvisory(value)
	return nil
}

func retryDelay(base time.Duration, failures int) time.Duration {
	delay := min(base, maximumRetryDelay)
	for index := 1; index < failures && delay < maximumRetryDelay; index++ {
		delay = min(delay*2, maximumRetryDelay)
	}
	return delay
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
