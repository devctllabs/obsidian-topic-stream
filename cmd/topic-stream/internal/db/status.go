package db

import (
	"context"
	"errors"
	"time"

	"github.com/devctllabs/go-libs/lifecycle"
	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	"github.com/urfave/cli/v3"
)

//go:generate go tool mockgen -source=status.go -destination=mocks/status.go -package=mocks

// StatusCommandOptions selects the connection and output for migration inspection.
type StatusCommandOptions struct{ CommonOptions }

// StatusDeps transfers an owned schema-history reader and cleanup to one command invocation.
type StatusDeps struct {
	Migrator StatusReader
	Shutdown func(ctx context.Context) error
}

// StatusDepsFactory acquires command dependencies and transfers their cleanup to the caller.
type StatusDepsFactory interface {
	// Build resolves migration configuration and returns an owned status reader.
	Build(ctx context.Context, opts StatusCommandOptions) (*StatusDeps, error)
}

// DefaultStatusDepsFactory opens the configured migration history reader.
type DefaultStatusDepsFactory struct{}

// Build acquires the command runtime and transfers its shutdown function to the caller.
func (DefaultStatusDepsFactory) Build(ctx context.Context, opts StatusCommandOptions) (*StatusDeps, error) {
	runtime, err := deps.NewMigrate(ctx, deps.MigrateOptions{URL: opts.urlOverride()})
	if err != nil {
		return nil, err
	}
	return &StatusDeps{Migrator: migrationRuntime{runtime}, Shutdown: runtime.Shutdown}, nil
}

type statusCommand struct {
	opts    StatusCommandOptions
	factory StatusDepsFactory
}

// NewStatusCommand constructs migration inspection with command-owned cleanup.
func NewStatusCommand(factory StatusDepsFactory) *cli.Command {
	x := &statusCommand{factory: factory}
	return &cli.Command{Name: "status", Usage: "show the schema version and dirty state", Flags: x.opts.flags(), Action: x.run}
}

func (c *statusCommand) run(ctx context.Context, command *cli.Command) (err error) {
	if err := validateArguments(command); err != nil {
		return err
	}
	c.opts.URLSet = command.IsSet(urlFlag)
	runtime, err := c.factory.Build(ctx, c.opts)
	if err != nil {
		return err
	}
	var output result
	defer func() {
		err = errors.Join(err, lifecycle.Shutdown(ctx, 10*time.Second, runtime.Shutdown))
		if err == nil {
			err = output.write(command.Root().Writer, c.opts.JSON)
		}
	}()
	output, err = observe(ctx, runtime.Migrator, nil)
	return err
}
