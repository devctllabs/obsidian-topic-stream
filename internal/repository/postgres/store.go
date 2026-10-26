package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store owns the PostgreSQL pool used for canonical Workspace state; callers must close it.
type Store struct {
	pool *pgxpool.Pool
	dsn  string
}

func found(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return false, err
}

// Open configures and verifies a PostgreSQL pool; the caller must Close a successful result.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("configure Postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to Postgres: %w", err)
	}
	return &Store{pool: pool, dsn: dsn}, nil
}

// Close releases the PostgreSQL pool and its connections.
func (s *Store) Close() {
	s.pool.Close()
}

// Ping checks that the pool can reach PostgreSQL within ctx.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// EnsureWorkspace creates the Workspace identity when absent without applying schema migrations.
func (s *Store) EnsureWorkspace(ctx context.Context, workspaceID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO workspaces (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, workspaceID)
	if err != nil {
		return fmt.Errorf("ensure workspace: %w", err)
	}
	return nil
}
