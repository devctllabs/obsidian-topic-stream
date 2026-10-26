package retrieval

import (
	"context"
	"encoding/json"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
)

func (m *Manager) retryReindex(ctx context.Context, source domain.Job) (domain.Job, error) {
	var arguments reindexArguments
	if source.WorkspaceID != m.workspaceID || source.ArgumentsVersion != reindexArgumentsVersion ||
		json.Unmarshal(source.Arguments, &arguments) != nil {
		return domain.Job{}, jobs.ErrNotRetryable
	}
	job, err := jobs.Prepare(jobs.EnqueueRequest{
		WorkspaceID: m.workspaceID, Definition: reindexJobDefinition, Arguments: arguments,
		IdempotencyKey: reindexKey(arguments.ModelID, arguments.SelectionGeneration),
	}, m.now())
	if err != nil {
		return domain.Job{}, err
	}
	job, created, err := m.catalog.RetryModelTransition(ctx, source, job)
	if err == nil && created && m.progressNotifier != nil {
		m.progressNotifier.NotifyJobChanged(job.WorkspaceID, job.ID)
	}
	return job, err
}
