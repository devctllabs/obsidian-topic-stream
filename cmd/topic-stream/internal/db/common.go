package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/urfave/cli/v3"
)

const (
	urlFlag   = "url"
	jsonFlag  = "json"
	stepsFlag = "steps"
)

// CommonOptions holds database connection overrides and output selection.
type CommonOptions struct {
	URL string
	// URLSet distinguishes an omitted override from an explicitly supplied empty URL.
	URLSet bool
	JSON   bool
}

func (o *CommonOptions) flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: urlFlag, Usage: "override TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN", Destination: &o.URL},
		&cli.BoolFlag{Name: jsonFlag, Usage: "emit the successful result as JSON", Destination: &o.JSON},
	}
}

func (o CommonOptions) urlOverride() *string {
	if !o.URLSet {
		return nil
	}
	return &o.URL
}

// MigrationStatus reports observed schema history; a nil version means no applied migration.
type MigrationStatus struct {
	Version *uint `json:"version"`
	Dirty   bool  `json:"dirty"`
}

// StatusReader observes migration history without applying or rolling back migrations.
type StatusReader interface {
	// Status reads the current version and dirty state; an unapplied version is nil.
	Status(ctx context.Context) (MigrationStatus, error)
}

type result struct {
	MigrationStatus
	NoChange *bool `json:"noChange,omitempty"`
}

func versionText(version *uint) string {
	if version == nil {
		return "none"
	}
	return strconv.FormatUint(uint64(*version), 10)
}

func (r result) write(destination io.Writer, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(destination).Encode(r)
	}
	text := fmt.Sprintf("version=%s dirty=%t", versionText(r.Version), r.Dirty)
	if r.NoChange != nil {
		text += fmt.Sprintf(" noChange=%t", *r.NoChange)
	}
	_, err := fmt.Fprintln(destination, text)
	return err
}

func observe(ctx context.Context, reader StatusReader, operationErr error) (result, error) {
	state, statusErr := reader.Status(ctx)
	if operationErr != nil {
		if statusErr != nil {
			return result{}, fmt.Errorf("migration failed (version=unknown dirty=unknown): %w", errors.Join(operationErr, statusErr))
		}
		return result{}, fmt.Errorf("migration failed (version=%s dirty=%t): %w", versionText(state.Version), state.Dirty, operationErr)
	}
	if statusErr != nil {
		return result{}, fmt.Errorf("read migration status: %w", statusErr)
	}
	return result{MigrationStatus: state}, nil
}

func validateArguments(command *cli.Command) error {
	if command.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	return nil
}
