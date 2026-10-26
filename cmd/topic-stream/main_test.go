package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootHelpDoesNotLoadConfiguration(t *testing.T) {
	t.Setenv("TOPIC_STREAM_SHUTDOWN_TIMEOUT", "invalid")
	for _, args := range [][]string{{"--help"}, {"db", "--help"}, {"db", "up", "--help"}, {"db", "down", "--help"}, {"db", "status", "--help"}} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			app := newCommand()
			var out, errOut bytes.Buffer
			app.Writer, app.ErrWriter = &out, &errOut
			require.NoError(t, app.Run(t.Context(), append([]string{"topic-stream"}, args...)))
			require.Empty(t, errOut.String())
			require.NotEmpty(t, out.String())
			require.Nil(t, app.Command("migrate"))
		})
	}
}

func TestRootReturnsUsageErrorsWithoutPrinting(t *testing.T) {
	for _, args := range [][]string{{"migrate"}, {"db", "unknown"}, {"db", "up", "--unknown"}} {
		t.Run(args[len(args)-1], func(t *testing.T) {
			app := newCommand()
			var out, errOut bytes.Buffer
			app.Writer, app.ErrWriter = &out, &errOut
			require.Error(t, app.Run(t.Context(), append([]string{"topic-stream"}, args...)))
			require.Empty(t, out.String())
			require.Empty(t, errOut.String())
		})
	}
}
