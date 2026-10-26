package db

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
)

// migrationRuntime maps the infrastructure status to the CLI-owned capability contract.
type migrationRuntime struct{ *postgres.Migrator }

// Status maps observed migration history into the command result.
func (r migrationRuntime) Status(ctx context.Context) (MigrationStatus, error) {
	state, err := r.Migrator.Status(ctx)
	return MigrationStatus{Version: state.Version, Dirty: state.Dirty}, err
}
