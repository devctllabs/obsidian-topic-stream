package db

import "github.com/urfave/cli/v3"

// NewCmd constructs the database command group without loading configuration or opening connections.
func NewCmd() *cli.Command {
	return &cli.Command{Name: "db", Usage: "manage the PostgreSQL schema", Commands: []*cli.Command{
		NewUpCommand(DefaultUpDepsFactory{}), NewDownCommand(DefaultDownDepsFactory{}), NewStatusCommand(DefaultStatusDepsFactory{}),
	}}
}
