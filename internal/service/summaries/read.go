package summaries

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

// Read returns existing annotation and permission facts without starting any work.
func (s *Service) Read(ctx context.Context, source domain.SummarySource) (domain.ItemSummary, error) {
	input, historical, err := s.readInput(ctx, source)
	if err != nil {
		return domain.ItemSummary{}, err
	}
	settings, err := s.config.Settings.GetWorkspace(ctx)
	if err != nil {
		return domain.ItemSummary{}, fmt.Errorf("read summary settings: %w", err)
	}
	binding, err := s.config.Repository.ReadSummaryBinding(ctx, domain.SummaryBindingQuery{SummaryInputKey: input.SummaryInputKey, IssueID: source.IssueID})
	if err != nil {
		return domain.ItemSummary{}, fmt.Errorf("read summary binding: %w", err)
	}
	state := summaryPermission(input, settings, historical)
	return boundSummaryState(state, binding, input.Supported() && summaryEnabled(settings)), nil
}

func (s *Service) readInput(ctx context.Context, source domain.SummarySource) (domain.SummaryInput, bool, error) {
	item, found, err := s.config.Repository.GetSummarySource(ctx, s.config.WorkspaceID, source)
	if err != nil {
		return domain.SummaryInput{}, false, fmt.Errorf("read summary source: %w", err)
	}
	if !found {
		return domain.SummaryInput{}, false, domain.ErrSummarySourceNotFound
	}
	input := domain.SummaryInputForItem(item)
	if source.SourceVersion == "" || source.SourceVersion == input.SourceVersion {
		return input, false, nil
	}
	key := domain.SummaryInputKey{WorkspaceID: s.config.WorkspaceID, ItemID: source.ItemID, SourceVersion: source.SourceVersion}
	previous, found, err := s.config.Repository.GetSummaryInput(ctx, key)
	if err != nil {
		return domain.SummaryInput{}, false, fmt.Errorf("read historical summary input: %w", err)
	}
	if !found {
		previous = domain.SummaryInput{SummaryInputKey: key, Origin: domain.ContentOriginUnknown}
	}
	return previous, true, nil
}

func summaryEnabled(settings domain.WorkspaceSettings) bool {
	generation := settings.Generation
	return generation.Enabled && generation.Endpoint != nil && generation.SummaryModel != nil && strings.TrimSpace(*generation.Endpoint) != "" && strings.TrimSpace(*generation.SummaryModel) != ""
}

func summaryPermission(input domain.SummaryInput, settings domain.WorkspaceSettings, historical bool) domain.ItemSummary {
	state := domain.ItemSummary{SourceVersion: input.SourceVersion, Supported: input.Supported(), State: domain.ItemSummaryNotRequested}
	reason := ""
	switch {
	case !state.Supported:
		reason = "unsupported_input"
	case !summaryEnabled(settings):
		reason = "generation_disabled"
	case historical:
		reason = "historical_input"
	default:
		state.CanGenerate = true
	}
	if reason != "" {
		state.UnavailableReason = &reason
	}
	return state
}

func boundSummaryState(state domain.ItemSummary, binding domain.SummaryBinding, retryAllowed bool) domain.ItemSummary {
	if binding.Result != nil && binding.Result.SourceVersion == state.SourceVersion {
		state.Result = binding.Result
		state.State = domain.ItemSummaryReady
	}
	if binding.Job == nil {
		return state
	}
	job := *binding.Job
	state.JobID = &job.ID
	if job.Status.Terminal() {
		return terminalSummaryState(state, job, retryAllowed)
	}
	if state.Result == nil {
		state.State = domain.ItemSummaryPending
	}
	return state
}

func terminalSummaryState(state domain.ItemSummary, job domain.Job, retryAllowed bool) domain.ItemSummary {
	if state.Result != nil && state.Result.GenerationJobID == job.ID {
		return state
	}
	if job.Status == domain.JobStatusDead || job.Status == domain.JobStatusCancelled {
		state.Failure = job.Reason
		if state.Failure == nil {
			message := "Summary generation failed."
			state.Failure = &message
		}
		state.Retryable = retryAllowed
	}
	if state.Result == nil {
		state.State = domain.ItemSummaryFailed
		if state.Failure == nil {
			message := "Summary result is unavailable."
			state.Failure = &message
		}
	}
	return state
}
