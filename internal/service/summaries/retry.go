package summaries

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
)

func (s *Service) retryGeneration(ctx context.Context, source domain.Job) (domain.Job, error) {
	input, err := s.retryInput(ctx, source)
	if err != nil {
		return domain.Job{}, err
	}
	settings, err := s.config.Settings.GetWorkspace(ctx)
	if err != nil {
		return domain.Job{}, fmt.Errorf("read summary retry settings: %w", err)
	}
	if !summaryEnabled(settings) {
		return domain.Job{}, domain.ErrSummaryUnavailable
	}
	_, fingerprint := summaryConfiguration(settings)
	job, err := s.prepareJob(input.SummaryInputKey)
	if err != nil {
		return domain.Job{}, err
	}
	admission, err := s.config.Repository.AdmitSummary(ctx, domain.SummaryAdmission{Input: input, RetrySourceJobID: &source.ID, ConfigurationFingerprint: fingerprint, Job: job})
	if err != nil {
		return domain.Job{}, fmt.Errorf("admit summary retry: %w", err)
	}
	if admission.Created {
		s.config.Events.NotifyJobChanged(admission.Job.WorkspaceID, admission.Job.ID)
	}
	return admission.Job, nil
}

func (s *Service) retryInput(ctx context.Context, source domain.Job) (domain.SummaryInput, error) {
	arguments, err := decodeGenerationArguments(source.Arguments)
	if err != nil || source.WorkspaceID != s.config.WorkspaceID {
		return domain.SummaryInput{}, jobs.ErrNotRetryable
	}
	key := domain.SummaryInputKey{WorkspaceID: s.config.WorkspaceID, ItemID: arguments.ItemID, SourceVersion: arguments.SourceVersion}
	input, found, err := s.config.Repository.GetSummaryInput(ctx, key)
	if err != nil {
		return domain.SummaryInput{}, fmt.Errorf("read summary retry input: %w", err)
	}
	if !found || !input.Supported() {
		return domain.SummaryInput{}, jobs.ErrNotRetryable
	}
	return input, nil
}
