//go:build integration

package postgres

import (
	"context"
	"encoding/json"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExplainFeedDisplayForTest measures the real refresh query and rolls its writes back.
func ExplainFeedDisplayForTest(ctx context.Context, s *Store, workspaceID uuid.UUID, ids []uuid.UUID) (json.RawMessage, error) {
	tx, err := s.beginFeedChange(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var plan json.RawMessage
	err = tx.QueryRow(ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+refreshFeedDisplaySQL, workspaceID, ids).Scan(&plan)
	return plan, err
}

// OpenWithTracerForTest attaches database timing instrumentation to integration fixtures.
func OpenWithTracerForTest(ctx context.Context, dsn string, tracer pgx.QueryTracer, parameters ...map[string]string) (*Store, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.ConnConfig.Tracer = tracer
	for _, values := range parameters {
		for name, value := range values {
			config.ConnConfig.RuntimeParams[name] = value
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool, dsn: dsn}, nil
}

// RequestFeedPreparationForTest prepares durable due state for integration scenarios.
func RequestFeedPreparationForTest(ctx context.Context, s *Store, request domain.FeedPreparationRequest) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := lockFeedHead(ctx, tx, request.WorkspaceID); err != nil {
		return err
	}
	if err := requestFeedPreparation(ctx, tx, request); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RefreshFeedDisplayForTest completes direct SQL fixtures that bypass publication/correction APIs.
func RefreshFeedDisplayForTest(ctx context.Context, s *Store, workspaceID uuid.UUID, ids []uuid.UUID) error {
	tx, err := s.beginFeedChange(ctx, workspaceID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := refreshFeedDisplay(ctx, tx, workspaceID, ids); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
