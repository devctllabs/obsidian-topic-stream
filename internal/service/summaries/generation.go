package summaries

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/google/uuid"
)

const summaryAttemptTimeout = 3 * time.Minute
const summaryCallTimeout = time.Minute

type summaryAttempt struct {
	input         domain.SummaryInput
	configuration domain.SummaryConfiguration
	fingerprint   string
}

func (s *Service) handle(ctx context.Context, payload json.RawMessage) error {
	arguments, err := decodeGenerationArguments(payload)
	if err != nil {
		return jobs.Permanent(err)
	}
	execution, valid := jobs.CurrentExecution(ctx)
	if !valid || execution.WorkspaceID != s.config.WorkspaceID || execution.JobID == uuid.Nil || execution.LeaseOwner == "" || execution.Attempt < 1 {
		return jobs.Permanent(domain.ErrSummaryPublicationRejected)
	}
	recovered, err := s.recoverSummary(ctx, execution, arguments)
	if err != nil || recovered {
		return err
	}
	return s.executeSummary(ctx, execution, arguments)
}

func decodeGenerationArguments(payload json.RawMessage) (generationArguments, error) {
	var arguments generationArguments
	if err := json.Unmarshal(payload, &arguments); err != nil {
		return arguments, errors.New("invalid summarize-item arguments")
	}
	decoded, err := hex.DecodeString(arguments.SourceVersion)
	if arguments.ItemID == uuid.Nil || err != nil || len(decoded) != sha256Bytes {
		return arguments, errors.New("invalid summarize-item input identity")
	}
	return arguments, nil
}

const sha256Bytes = 32

func (s *Service) recoverSummary(ctx context.Context, execution jobs.Execution, arguments generationArguments) (bool, error) {
	result, found, err := s.config.Repository.GetSummaryResultForJob(ctx, s.config.WorkspaceID, execution.JobID)
	if err != nil {
		return false, fmt.Errorf("recover summary result: %w", err)
	}
	if found && (result.GenerationJobID != execution.JobID || result.SourceVersion != arguments.SourceVersion) {
		return false, jobs.Permanent(domain.ErrSummaryPublicationRejected)
	}
	return found, nil
}

func (s *Service) executeSummary(ctx context.Context, execution jobs.Execution, arguments generationArguments) error {
	attempt, err := s.prepareAttempt(ctx, arguments)
	if err != nil {
		return err
	}
	text, reusedFrom, err := s.generateOrReuse(ctx, attempt)
	if err != nil {
		return err
	}
	annotation := annotationForAttempt(attempt, execution.JobID, text, s.config.Now())
	err = s.config.Repository.PublishSummary(ctx, domain.SummaryPublication{Input: attempt.input.SummaryInputKey, Annotation: annotation, Configuration: attempt.configuration, ConfigurationFingerprint: attempt.fingerprint, ReusedFrom: reusedFrom, LeaseOwner: execution.LeaseOwner, Attempt: execution.Attempt})
	if errors.Is(err, domain.ErrSummaryPublicationRejected) {
		return jobs.Permanent(err)
	}
	if err != nil {
		return fmt.Errorf("publish item summary: %w", err)
	}
	return nil
}

func (s *Service) prepareAttempt(ctx context.Context, arguments generationArguments) (summaryAttempt, error) {
	settings, err := s.config.Settings.GetWorkspace(ctx)
	if err != nil {
		return summaryAttempt{}, fmt.Errorf("capture summary attempt settings: %w", err)
	}
	if !summaryEnabled(settings) {
		return summaryAttempt{}, jobs.Permanent(domain.ErrSummaryUnavailable)
	}
	key := domain.SummaryInputKey{WorkspaceID: s.config.WorkspaceID, ItemID: arguments.ItemID, SourceVersion: arguments.SourceVersion}
	input, found, err := s.config.Repository.GetSummaryInput(ctx, key)
	if err != nil {
		return summaryAttempt{}, fmt.Errorf("read captured summary input: %w", err)
	}
	if !found || !input.Supported() {
		return summaryAttempt{}, jobs.Permanent(domain.ErrSummaryUnavailable)
	}
	configuration, fingerprint := summaryConfiguration(settings)
	return summaryAttempt{input: input, configuration: configuration, fingerprint: fingerprint}, nil
}

func (s *Service) generateOrReuse(ctx context.Context, attempt summaryAttempt) (string, *uuid.UUID, error) {
	cached, found, err := s.config.Repository.FindReadySummary(ctx, attempt.input.SummaryInputKey, attempt.fingerprint)
	if err != nil {
		return "", nil, fmt.Errorf("find reusable summary: %w", err)
	}
	if found {
		if cached.SourceVersion != attempt.input.SourceVersion {
			return "", nil, jobs.Permanent(domain.ErrSummaryPublicationRejected)
		}
		return cached.Text, &cached.ID, nil
	}
	ctx, cancel := context.WithTimeout(ctx, summaryAttemptTimeout)
	defer cancel()
	text, err := s.generateText(ctx, attempt)
	return text, nil, err
}

func annotationForAttempt(attempt summaryAttempt, jobID uuid.UUID, text string, at time.Time) domain.AISummary {
	configuration := attempt.configuration
	return domain.AISummary{ID: uuid.New(), GenerationJobID: jobID, SourceVersion: attempt.input.SourceVersion, Text: text, Model: configuration.Model, Language: configuration.Language, MaxInputChars: configuration.MaxInputChars, MaxSummaryCharacters: configuration.MaxSummaryCharacters, InstructionsVersion: configuration.InstructionsVersion, AlgorithmVersion: configuration.AlgorithmVersion, CreatedAt: at}
}

func (s *Service) call(ctx context.Context, prompt Prompt) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, summaryCallTimeout)
	defer cancel()
	text, err := s.config.Generator.Summarize(callCtx, prompt)
	if err != nil {
		return "", fmt.Errorf("generate summary text: %w", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("summary provider returned empty text")
	}
	if utf8.RuneCountInString(text) > prompt.MaxOutputCharacters {
		return "", errors.New("summary output exceeded the configured limit")
	}
	return text, nil
}
