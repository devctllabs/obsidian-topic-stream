package events

import (
	"context"
	"sync"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

// Type identifies a product event used to invalidate client REST state.
type Type string

const (
	TypeFeedInvalidated Type = "feedInvalidated"
	TypeDigestReady     Type = "digestReady"
	TypeJobChanged      Type = "jobChanged"
)

// Event carries a typed advisory payload for live subscribers without replay.
type Event struct {
	Type Type
	Data any
}

// FeedInvalidated identifies the Item whose change can affect Feed views.
type FeedInvalidated struct {
	ItemID uuid.UUID `json:"itemId"`
}

// DigestReady identifies a saved immutable Digest Issue.
type DigestReady struct {
	IssueID uuid.UUID `json:"issueId"`
}

// JobChanged identifies a Job whose authoritative state should be refetched.
type JobChanged struct {
	JobID uuid.UUID `json:"jobId"`
}

// AdvisoryPublisher accepts best-effort cross-process invalidations.
type AdvisoryPublisher interface {
	// PublishAdvisory publishes a best-effort advisory without replacing durable state.
	PublishAdvisory(advisory domain.Advisory)
}

// Service broadcasts advisory process-local events. Delivery is best effort; durable product state
// remains authoritative when a slow subscriber misses an event.
type Service struct {
	workspaceID uuid.UUID
	origin      uuid.UUID
	publisher   AdvisoryPublisher
	mu          sync.Mutex
	nextID      uint64
	subscribers map[uint64]chan Event
}

// NewForWorkspace creates a Workspace-scoped broadcaster with a unique process origin.
func NewForWorkspace(workspaceID uuid.UUID, publisher AdvisoryPublisher) *Service {
	return &Service{
		workspaceID: workspaceID, origin: uuid.New(), publisher: publisher,
		subscribers: make(map[uint64]chan Event),
	}
}

// Subscribe closes its channel after ctx is cancelled.
func (s *Service) Subscribe(ctx context.Context) <-chan Event {
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	channel := make(chan Event, 8)
	s.subscribers[id] = channel
	s.mu.Unlock()
	go s.unsubscribeOnDone(ctx, id, channel)
	return channel
}

// Publish never blocks on a subscriber and drops the event for subscribers whose buffer is full.
func (s *Service) Publish(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, subscriber := range s.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

// Disconnect forces current no-replay SSE clients to reconnect and refetch authoritative REST
// state after an advisory listener delivery gap.
func (s *Service) Disconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, subscriber := range s.subscribers {
		delete(s.subscribers, id)
		close(subscriber)
	}
}

// InvalidateFeed announces a change affecting the Workspace's Feed views.
func (s *Service) InvalidateFeed(workspaceID, itemID uuid.UUID) {
	s.publishWorkspace(workspaceID, itemID, domain.AdvisoryFeedInvalidated)
}

// NotifyDigestReady announces a saved Digest Issue to the Workspace's subscribers.
func (s *Service) NotifyDigestReady(workspaceID, issueID uuid.UUID) {
	s.publishWorkspace(workspaceID, issueID, domain.AdvisoryDigestReady)
}

// NotifyJobChanged announces a durable Job state change to its Workspace.
func (s *Service) NotifyJobChanged(workspaceID, jobID uuid.UUID) {
	s.publishWorkspace(workspaceID, jobID, domain.AdvisoryJobChanged)
}

func (s *Service) publishWorkspace(workspaceID, resourceID uuid.UUID, kind domain.AdvisoryKind) {
	advisory := domain.Advisory{
		Kind: kind, WorkspaceID: workspaceID, ResourceID: resourceID, Origin: s.origin,
	}
	s.publishLocal(advisory)
	if s.publisher != nil {
		s.publisher.PublishAdvisory(advisory)
	}
}

// AcceptAdvisory broadcasts an advisory received from another process without publishing it again.
func (s *Service) AcceptAdvisory(advisory domain.Advisory) {
	if advisory.Origin == s.origin {
		return
	}
	s.publishLocal(advisory)
}

func (s *Service) publishLocal(advisory domain.Advisory) {
	if s.workspaceID != uuid.Nil && advisory.WorkspaceID != s.workspaceID {
		return
	}
	event, found := eventFromAdvisory(advisory)
	if found {
		s.Publish(event)
	}
}

func eventFromAdvisory(advisory domain.Advisory) (Event, bool) {
	switch advisory.Kind {
	case domain.AdvisoryFeedInvalidated:
		return Event{Type: TypeFeedInvalidated, Data: FeedInvalidated{ItemID: advisory.ResourceID}}, true
	case domain.AdvisoryDigestReady:
		return Event{Type: TypeDigestReady, Data: DigestReady{IssueID: advisory.ResourceID}}, true
	case domain.AdvisoryJobChanged:
		return Event{Type: TypeJobChanged, Data: JobChanged{JobID: advisory.ResourceID}}, true
	default:
		return Event{}, false
	}
}

func (s *Service) unsubscribeOnDone(ctx context.Context, id uint64, channel chan Event) {
	<-ctx.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, found := s.subscribers[id]; found && current == channel {
		delete(s.subscribers, id)
		close(channel)
	}
}
