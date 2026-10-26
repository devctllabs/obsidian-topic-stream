package db

import (
	"context"
	"errors"
	"time"

	"github.com/devctllabs/go-libs/lifecycle"
	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	"github.com/urfave/cli/v3"
)

//go:generate go tool mockgen -source=down.go -destination=mocks/down.go -package=mocks

// DownCommandOptions selects a bounded rollback and its connection and output options.
type DownCommandOptions struct {
	CommonOptions
	Steps int
	// StepsSet distinguishes an explicit step count from the command's default behavior.
	StepsSet bool
}

// DownMigrator observes schema history and rolls back a bounded migration count.
type DownMigrator interface {
	StatusReader
	// Down rolls back a positive count. Empty or insufficient history returns an error.
	Down(ctx context.Context, steps int) (bool, error)
}

// DownDeps transfers an owned rollback runtime and cleanup to one command invocation.
type DownDeps struct {
	Migrator DownMigrator
	Shutdown func(ctx context.Context) error
}

// DownDepsFactory acquires command dependencies and transfers their cleanup to the caller.
type DownDepsFactory interface {
	// Build resolves migration configuration and returns an owned migrator.
	Build(ctx context.Context, opts DownCommandOptions) (*DownDeps, error)
}

// DefaultDownDepsFactory opens the configured embedded migration runtime for rollback.
type DefaultDownDepsFactory struct{}

// Build acquires the command runtime and transfers its shutdown function to the caller.
func (DefaultDownDepsFactory) Build(ctx context.Context, opts DownCommandOptions) (*DownDeps, error) {
	runtime, err := deps.NewMigrate(ctx, deps.MigrateOptions{URL: opts.urlOverride()})
	if err != nil {
		return nil, err
	}
	return &DownDeps{Migrator: migrationRuntime{runtime}, Shutdown: runtime.Shutdown}, nil
}

type downCommand struct {
	opts    DownCommandOptions
	factory DownDepsFactory
}

// NewDownCommand constructs bounded rollback with command-owned cleanup.
func NewDownCommand(factory DownDepsFactory) *cli.Command {
	x := &downCommand{factory: factory}
	return &cli.Command{
		Name: "down", Usage: "roll back embedded PostgreSQL migrations (removes schema and data)",
		Flags:  append(x.opts.flags(), &cli.IntFlag{Name: stepsFlag, Usage: "roll back a positive number of steps", Value: 1, Destination: &x.opts.Steps}),
		Action: x.run,
	}
}

func (c *downCommand) run(ctx context.Context, command *cli.Command) (err error) {
	if err := validateArguments(command); err != nil {
		return err
	}
	c.opts.URLSet, c.opts.StepsSet = command.IsSet(urlFlag), command.IsSet(stepsFlag)
	if c.opts.Steps < 1 {
		return errors.New("steps must be positive")
	}
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
	noChange, operationErr := runtime.Migrator.Down(ctx, c.opts.Steps)
	output, err = observe(ctx, runtime.Migrator, operationErr)
	output.NoChange = &noChange
	return err
}
