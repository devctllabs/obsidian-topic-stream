package feed

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

var (
	ErrInvalidPageToken = errors.New("invalid feed page token")
	ErrTopicNotFound    = errors.New("feed topic not found")
)

// Page aliases the domain page of saved Feed Publications.
type Page = domain.FeedPage

//go:generate go tool mockgen -source=service.go -destination=mocks/topics.gen.go -package=mocks

// Topics reads Topics within a Workspace for selection and validation.
type Topics interface {
	// GetTopic resolves a Topic within the Workspace; missing or disabled Topics cannot select Feed views.
	GetTopic(ctx context.Context, workspaceID, topicID uuid.UUID) (domain.Topic, bool, error)
}

// Service reads shared publication history and explicitly admits prepared candidates.
type Service struct {
	admission      Admission
	settings       WorkspaceSettings
	ranker         PersonalRanker
	events         FeedInvalidator
	selectionStart func(int) int
	gateVersion    string
	history        History
	topics         Topics
	workspaceID    uuid.UUID
	now            func() time.Time
}

// Config supplies dependencies and operating settings for the feed service.
type Config struct {
	Admission Admission
	Settings  WorkspaceSettings
	Ranker    PersonalRanker
	Events    FeedInvalidator
	// SelectionStart chooses the first ready Topic list for balanced selection; nil uses random rotation.
	SelectionStart func(int) int
	GateVersion    string
	History        History
	Topics         Topics
	WorkspaceID    uuid.UUID
	Now            func() time.Time
}

// New binds Feed dependencies, defaulting the clock to time.Now and Topic rotation to rand.IntN.
func New(config Config) Service {
	if config.SelectionStart == nil {
		config.SelectionStart = rand.IntN
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return Service{admission: config.Admission, settings: config.Settings, ranker: config.Ranker, events: config.Events, selectionStart: config.SelectionStart, gateVersion: config.GateVersion, history: config.History, topics: config.Topics, workspaceID: config.WorkspaceID, now: config.Now}
}

func (s Service) feedTopic(ctx context.Context, topicID uuid.UUID) (domain.Topic, error) {
	if s.topics == nil {
		return domain.Topic{}, ErrTopicNotFound
	}
	topic, found, err := s.topics.GetTopic(ctx, s.workspaceID, topicID)
	if err != nil {
		return domain.Topic{}, fmt.Errorf("get feed topic: %w", err)
	}
	if !found || !topic.Enabled {
		return domain.Topic{}, ErrTopicNotFound
	}
	return topic, nil
}

func encodeToken(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeToken(token string, destination any) error {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return err
	}
	return json.Unmarshal(decoded, destination)
}
