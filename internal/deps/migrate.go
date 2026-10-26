package deps

import (
	"context"
	"errors"
	"os"
	"strings"

	configlib "github.com/devctllabs/go-libs/config"
	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/jackc/pgx/v5"
)

// MigrateOptions supplies an optional DSN override; a non-nil empty value is explicitly invalid.
type MigrateOptions struct{ URL *string }

// NewMigrate resolves only PostgreSQL configuration and acquires an owned migrator.
func NewMigrate(ctx context.Context, opts MigrateOptions) (*postgres.Migrator, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var cfg generated.DBPrimaryConfig
	loader := configlib.Chain(configlib.Defaults(), configlib.Optional(configlib.DotEnv(configlib.Path(".env"))), configlib.OSEnv())
	if err := loader.Load(ctx, &cfg); err != nil {
		return nil, &migrationConfigError{message: "load migration configuration", cause: err}
	}
	// The env decoder skips empty strings; restore explicit process values before validation.
	if value, present := os.LookupEnv("TOPIC_STREAM_DB_PRIMARY_KIND"); present {
		cfg.Kind = value
	}
	if value, present := os.LookupEnv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN"); present {
		cfg.PostgresDSN = value
	}
	if opts.URL != nil {
		cfg.PostgresDSN = *opts.URL
	}
	if cfg.Kind != "postgres" {
		return nil, errors.New("only postgres database kind is supported")
	}
	if strings.TrimSpace(cfg.PostgresDSN) == "" {
		return nil, errors.New("postgres DSN is required")
	}
	if _, err := pgx.ParseConfig(cfg.PostgresDSN); err != nil {
		return nil, &migrationConfigError{message: "invalid postgres DSN", cause: err}
	}
	return postgres.OpenMigrator(ctx, cfg.PostgresDSN)
}

// Hide configuration input while preserving the driver cause for errors.Is/As.
type migrationConfigError struct {
	message string
	cause   error
}

// Error returns the caller-facing failure description.
func (e *migrationConfigError) Error() string { return e.message }

// Unwrap preserves the underlying cause for errors.Is and errors.As.
func (e *migrationConfigError) Unwrap() error { return e.cause }
