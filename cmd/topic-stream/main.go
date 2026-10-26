package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	command "github.com/ethernity/obsidian-topic-stream/cmd/topic-stream/internal"
	"github.com/ethernity/obsidian-topic-stream/cmd/topic-stream/internal/db"
	"github.com/urfave/cli/v3"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := newCommand().Run(ctx, os.Args); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newCommand() *cli.Command {
	root := &cli.Command{
		Name: "topic-stream", Usage: "personal news feed daemon", Version: version,
		Writer: os.Stdout, ErrWriter: os.Stderr,
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Commands: []*cli.Command{
			db.NewCmd(),
			command.NewBenchCommand(command.DefaultBenchDepsFactory{}),
			command.NewReindexCommand(command.DefaultReindexDepsFactory{}),
			command.NewServeCommand(command.DefaultServeDepsFactory{Version: version}),
		},
	}
	return configureUsageErrors(root)
}

func configureUsageErrors(command *cli.Command) *cli.Command {
	command.OnUsageError = func(_ context.Context, _ *cli.Command, err error, _ bool) error { return err }
	for _, child := range command.Commands {
		configureUsageErrors(child)
	}
	return command
}
