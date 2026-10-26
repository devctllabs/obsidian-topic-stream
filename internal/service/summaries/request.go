package summaries

import (
	"context"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/google/uuid"
)

type generationArguments struct {
	ItemID        uuid.UUID `json:"itemId"`
	SourceVersion string    `json:"sourceVersion"`
}

// Request admits explicit work for the resolved server-owned input.
func (s *Service) Request(ctx context.Context, source domain.SummarySource) (domain.Job, error) {
	if source.SourceVersion != "" {
		return domain.Job{}, domain.ErrSummaryUnavailable
	}
	input, _, err := s.readInput(ctx, source)
	if err != nil {
		return domain.Job{}, err
	}
	settings, err := s.config.Settings.GetWorkspace(ctx)
	if err != nil {
		return domain.Job{}, fmt.Errorf("read summary admission settings: %w", err)
	}
	if !input.Supported() || !summaryEnabled(settings) {
		return domain.Job{}, domain.ErrSummaryUnavailable
	}
	_, fingerprint := summaryConfiguration(settings)
	job, err := s.prepareJob(input.SummaryInputKey)
	if err != nil {
		return domain.Job{}, err
	}
	admission, err := s.config.Repository.AdmitSummary(ctx, domain.SummaryAdmission{Input: input, IssueID: source.IssueID, ConfigurationFingerprint: fingerprint, Job: job})
	if err != nil {
		return domain.Job{}, fmt.Errorf("admit item summary: %w", err)
	}
	if admission.Created {
		s.config.Events.NotifyJobChanged(admission.Job.WorkspaceID, admission.Job.ID)
	}
	return admission.Job, nil
}

func (s *Service) prepareJob(input domain.SummaryInputKey) (domain.Job, error) {
	return jobs.Prepare(jobs.EnqueueRequest{WorkspaceID: s.config.WorkspaceID, Definition: summaryJob,
		Arguments:      generationArguments{ItemID: input.ItemID, SourceVersion: input.SourceVersion},
		IdempotencyKey: "summarize-item:" + input.ItemID.String() + ":" + input.SourceVersion}, s.config.Now())
}
