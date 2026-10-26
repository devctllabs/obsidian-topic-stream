package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AdvisoryBackend exchanges best-effort invalidations through PostgreSQL LISTEN/NOTIFY.
type AdvisoryBackend struct {
	store *Store
}

// NewAdvisoryBackend binds the Store's pool and DSN; Listen acquires its own dedicated connection.
func NewAdvisoryBackend(store *Store) *AdvisoryBackend {
	return &AdvisoryBackend{store: store}
}

// Publish sends a typed advisory with PostgreSQL NOTIFY; durable state remains authoritative.
func (b *AdvisoryBackend) Publish(ctx context.Context, value domain.Advisory) error {
	payload, err := encodeAdvisory(value)
	if err != nil {
		return err
	}
	if _, err := b.store.pool.Exec(ctx, `SELECT pg_notify('topic_stream_advisories',$1)`, payload); err != nil {
		return fmt.Errorf("publish Postgres advisory: %w", err)
	}
	return nil
}

// Listen owns a dedicated connection and calls onReady after LISTEN commits. Callbacks run
// sequentially; either callback's error ends the attempt and closes its connection.
func (b *AdvisoryBackend) Listen(ctx context.Context, onReady func() error, accept func(value domain.Advisory) error) (result error) {
	connection, err := pgx.Connect(ctx, b.store.dsn)
	if err != nil {
		return fmt.Errorf("connect Postgres advisory listener: %w", err)
	}
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := connection.Close(cleanupContext); err != nil {
			result = errors.Join(result, fmt.Errorf("close Postgres advisory listener: %w", err))
		}
	}()
	if _, err := connection.Exec(ctx, `LISTEN topic_stream_advisories`); err != nil {
		return fmt.Errorf("listen for Postgres advisories: %w", err)
	}
	if err := onReady(); err != nil {
		return fmt.Errorf("initialize Postgres advisory subscription: %w", err)
	}
	for {
		notification, err := connection.WaitForNotification(ctx)
		if err != nil {
			return fmt.Errorf("wait for Postgres advisory: %w", err)
		}
		value, err := decodeAdvisory(notification.Payload)
		if err != nil {
			continue
		}
		if err := accept(value); err != nil {
			return fmt.Errorf("accept Postgres advisory: %w", err)
		}
	}
}

func encodeAdvisory(value domain.Advisory) (string, error) {
	if err := validateAdvisory(value); err != nil {
		return "", err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode Postgres advisory: %w", err)
	}
	return string(payload), nil
}

func decodeAdvisory(payload string) (domain.Advisory, error) {
	var value domain.Advisory
	if err := json.Unmarshal([]byte(payload), &value); err != nil {
		return domain.Advisory{}, fmt.Errorf("decode Postgres advisory: %w", err)
	}
	if err := validateAdvisory(value); err != nil {
		return domain.Advisory{}, err
	}
	return value, nil
}

func validateAdvisory(value domain.Advisory) error {
	if value.WorkspaceID == uuid.Nil {
		return errors.New("postgres advisory workspace is required")
	}
	switch value.Kind {
	case domain.AdvisorySettingsChanged:
		if value.Version < 1 {
			return errors.New("settings advisory version is required")
		}
	case domain.AdvisoryFeedInvalidated, domain.AdvisoryDigestReady, domain.AdvisoryJobChanged:
		if value.ResourceID == uuid.Nil {
			return errors.New("product advisory resource is required")
		}
	default:
		return errors.New("postgres advisory kind is invalid")
	}
	return nil
}
