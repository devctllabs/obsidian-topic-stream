package digests

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
)

func (s *Service) retryGeneration(ctx context.Context, source domain.Job) (domain.Job, error) {
	arguments, err := decodeGenerationArguments(source.Arguments)
	if err != nil {
		return domain.Job{}, jobs.ErrNotRetryable
	}
	job, err := s.prepareGeneration(arguments, s.now())
	if err != nil {
		return domain.Job{}, err
	}
	admission, err := s.repository.AdmitDigestBuild(ctx, domain.DigestBuildRequest{
		WorkspaceID: s.workspaceID, DigestID: arguments.DigestID, Kind: domain.DigestBuildRetry, RequestID: source.ID, Job: job,
	})
	if err != nil {
		return domain.Job{}, fmt.Errorf("admit digest retry: %w", err)
	}
	if admission.Created {
		s.events.NotifyJobChanged(admission.Job.WorkspaceID, admission.Job.ID)
	}
	return admission.Job, nil
}
