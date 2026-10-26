package command_test

import (
	"bytes"
	"testing"

	command "github.com/ethernity/obsidian-topic-stream/cmd/topic-stream/internal"
	"github.com/ethernity/obsidian-topic-stream/cmd/topic-stream/internal/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.uber.org/mock/gomock"
)

func TestReindexCommandEnqueuesDurableRebuild(t *testing.T) {
	job := domain.Job{ID: uuid.New(), Status: domain.JobStatusQueued}
	ctrl := gomock.NewController(t)
	factory := mocks.NewMockReindexDepsFactory(ctrl)
	reindexer := mocks.NewMockReindexer(ctrl)
	factory.EXPECT().Build(gomock.Any()).Return(&command.ReindexDeps{
		Reindexer: reindexer, Shutdown: cleanShutdown,
	}, nil)
	reindexer.EXPECT().Rebuild(gomock.Any()).Return(job, nil)
	var stdout bytes.Buffer
	root := &cli.Command{
		Name: "topic-stream", Writer: &stdout,
		Commands: []*cli.Command{command.NewReindexCommand(factory)},
	}

	err := root.Run(t.Context(), []string{"topic-stream", "reindex"})

	require.NoError(t, err)
	require.Equal(t, "queued reindex job "+job.ID.String()+"\n", stdout.String())
}
