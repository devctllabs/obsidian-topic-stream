package db

import (
	"context"
	"errors"
	"time"

	"github.com/devctllabs/go-libs/lifecycle"
	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	"github.com/urfave/cli/v3"
)

//go:generate go tool mockgen -source=up.go -destination=mocks/up.go -package=mocks

// UpCommandOptions selects all pending migrations or an explicit positive step count.
type UpCommandOptions struct {
	CommonOptions
	Steps int
	// StepsSet distinguishes an explicit step count from the command's default behavior.
	StepsSet bool
}

// UpMigrator observes schema history and applies embedded migrations.
type UpMigrator interface {
	StatusReader
	// Up applies all pending migrations for zero steps, or a positive bounded count.
	// It returns true when the migration engine reports no change.
	Up(ctx context.Context, steps int) (bool, error)
}

// UpDeps transfers an owned migration runtime and cleanup to one command invocation.
type UpDeps struct {
	Migrator UpMigrator
	Shutdown func(ctx context.Context) error
}

// UpDepsFactory acquires command dependencies and transfers their cleanup to the caller.
type UpDepsFactory interface {
	// Build resolves migration configuration and returns an owned migrator.
	Build(ctx context.Context, opts UpCommandOptions) (*UpDeps, error)
}

// DefaultUpDepsFactory opens the configured embedded migration runtime for applying migrations.
type DefaultUpDepsFactory struct{}

// Build acquires the command runtime and transfers its shutdown function to the caller.
func (DefaultUpDepsFactory) Build(ctx context.Context, opts UpCommandOptions) (*UpDeps, error) {
	runtime, err := deps.NewMigrate(ctx, deps.MigrateOptions{URL: opts.urlOverride()})
	if err != nil {
		return nil, err
	}
	return &UpDeps{Migrator: migrationRuntime{runtime}, Shutdown: runtime.Shutdown}, nil
}

type upCommand struct {
	opts    UpCommandOptions
	factory UpDepsFactory
}

// NewUpCommand constructs migration application with command-owned cleanup.
func NewUpCommand(factory UpDepsFactory) *cli.Command {
	x := &upCommand{factory: factory}
	return &cli.Command{
		Name: "up", Usage: "apply pending embedded PostgreSQL migrations",
		Flags:  append(x.opts.flags(), &cli.IntFlag{Name: stepsFlag, Usage: "apply a positive number of steps; omit for all pending migrations", Value: 0, Destination: &x.opts.Steps}),
		Action: x.run,
	}
}

func (c *upCommand) run(ctx context.Context, command *cli.Command) (err error) {
	if err := validateArguments(command); err != nil {
		return err
	}
	c.opts.URLSet, c.opts.StepsSet = command.IsSet(urlFlag), command.IsSet(stepsFlag)
	if c.opts.StepsSet && c.opts.Steps < 1 {
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
	noChange, operationErr := runtime.Migrator.Up(ctx, c.opts.Steps)
	output, err = observe(ctx, runtime.Migrator, operationErr)
	output.NoChange = &noChange
	return err
}
