package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	postgresmigrations "github.com/ethernity/obsidian-topic-stream/migrations/primary/postgres"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// MigrationStatus describes the observed database schema history.
type MigrationStatus struct {
	Version *uint
	Dirty   bool
}

// Migrator owns a database connection and the immutable embedded migration source.
// Cancellation is checked before each operation; started upstream SQL runs to completion.
type Migrator struct {
	engine   *migrate.Migrate
	database database.Driver
}

// OpenMigrator acquires an embedded migration source and dedicated lock session; the caller owns Shutdown.
func OpenMigrator(ctx context.Context, dsn string) (*Migrator, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, err := iofs.New(postgresmigrations.Files, ".")
	if err != nil {
		return nil, migrationError("open embedded migrations", err)
	}
	driver, err := openMigrationDatabase(ctx, dsn)
	if err != nil {
		return nil, errors.Join(err, source.Close())
	}
	engine, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	if err != nil {
		return nil, migrationError("configure migrations", errors.Join(err, source.Close(), driver.Close()))
	}
	runtime := &Migrator{engine: engine, database: driver}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, runtime.Shutdown(ctx))
	}
	return runtime, nil
}

func openMigrationDatabase(ctx context.Context, dsn string) (_ database.Driver, err error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, migrationError("parse migration DSN", err)
	}
	var connection *pgx.Conn
	db := stdlib.OpenDB(*cfg, stdlib.OptionAfterConnect(func(_ context.Context, conn *pgx.Conn) error {
		connection = conn
		return nil
	}))
	// Migrations use one dedicated lock session. Retain its physical connection because
	// WithInstance does not release a reserved sql.Conn when metadata initialization fails.
	db.SetMaxOpenConns(1)
	defer func() {
		if err == nil {
			return
		}
		var closeErr error
		if connection != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			closeErr = connection.Close(cleanup)
			cancel()
		}
		err = &migrationFailure{message: err.Error(), cause: errors.Join(err, closeErr, db.Close())}
	}()
	if err := db.PingContext(ctx); err != nil {
		return nil, migrationError("connect migration database", err)
	}
	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{
		MigrationsTable: "topic_stream_schema_migrations", MultiStatementEnabled: true,
	})
	if err != nil {
		return nil, migrationError("configure migration database", err)
	}
	return driver, nil
}

// Up applies all pending migrations for zero steps, or a positive bounded count.
// ErrNoChange is successful and reported by the returned boolean.
func (m *Migrator) Up(ctx context.Context, steps int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if steps < 0 {
		return false, errors.New("migration steps must not be negative")
	}
	var err error
	if steps == 0 {
		err = m.engine.Up()
	} else {
		err = m.engine.Steps(steps)
	}
	noChange := errors.Is(err, migrate.ErrNoChange)
	if err != nil && !noChange {
		return false, migrationError("apply migrations", err)
	}
	return noChange, ctx.Err()
}

// Down rolls back a positive count. Insufficient history remains an upstream error.
func (m *Migrator) Down(ctx context.Context, steps int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if steps < 1 {
		return false, errors.New("migration steps must be positive")
	}
	if err := m.engine.Steps(-steps); err != nil {
		return false, migrationError("roll back migrations", err)
	}
	return false, ctx.Err()
}

// Status observes schema history, preserving the dirty flag even when the version is unapplied.
func (m *Migrator) Status(ctx context.Context) (MigrationStatus, error) {
	if err := ctx.Err(); err != nil {
		return MigrationStatus{}, err
	}
	// Migrate.Version drops the dirty flag for NilVersion, which can follow a failed
	// rollback of the first migration. Read the owned driver to retain that state.
	version, dirty, err := m.database.Version()
	if err != nil {
		return MigrationStatus{}, migrationError("read migration version", err)
	}
	if version == database.NilVersion {
		return MigrationStatus{Dirty: dirty}, nil
	}
	applied := uint(version)
	return MigrationStatus{Version: &applied, Dirty: dirty}, nil
}

// Shutdown closes both drivers, even after cancellation. Upstream close waits for active SQL.
func (m *Migrator) Shutdown(context.Context) error {
	sourceErr, databaseErr := m.engine.Close()
	if err := errors.Join(sourceErr, databaseErr); err != nil {
		return migrationError("close migrations", err)
	}
	return nil
}

// migrationFailure prevents upstream errors from printing SQL or connection credentials.
// Its original cause remains available through errors.Is/As.
type migrationFailure struct {
	message string
	cause   error
}

// Error returns the caller-facing failure description.
func (e *migrationFailure) Error() string { return e.message }

// Unwrap preserves the underlying cause for errors.Is and errors.As.
func (e *migrationFailure) Unwrap() error { return e.cause }

func migrationError(operation string, err error) error {
	message := operation + " failed"
	var dirty migrate.ErrDirty
	var short migrate.ErrShortLimit
	switch {
	case errors.As(err, &dirty):
		message += fmt.Sprintf(": dirty database version %d", dirty.Version)
	case errors.As(err, &short):
		message += fmt.Sprintf(": insufficient migrations (%d missing)", short.Short)
	case errors.Is(err, migrate.ErrNoChange):
		message += ": no applied migration"
	case errors.Is(err, migrate.ErrLockTimeout):
		message += ": lock timeout"
	}
	return &migrationFailure{message: message, cause: err}
}
