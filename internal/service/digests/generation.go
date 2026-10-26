package digests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
)

type digestBranch struct {
	topic        domain.Topic
	relationship domain.DigestTopic
	candidates   []ranking.Candidate
	pipeline     string
	failure      *string
}

type candidateOption struct {
	branch    int
	rank      int
	candidate ranking.Candidate
}

type itemOptions struct {
	itemID  string
	options []candidateOption
}

type issueBuild struct {
	definition  domain.DigestDefinition
	arguments   generationArguments
	branches    []digestBranch
	generatedAt time.Time
}

type digestRanking struct {
	definition  domain.DigestDefinition
	generatedAt time.Time
	settings    *domain.WorkspaceSettings
}

func (s *Service) handleGeneration(ctx context.Context, payload json.RawMessage) error {
	arguments, err := decodeGenerationArguments(payload)
	if err != nil {
		return err
	}
	execution, recovered, err := s.recoverGeneration(ctx, arguments)
	if err != nil || recovered {
		return err
	}
	definition, err := s.generationDefinition(ctx, arguments.DigestID)
	if err != nil {
		return err
	}
	settings, err := s.workspaceSettings(ctx)
	if err != nil {
		return err
	}
	generatedAt := s.now()
	branches := s.rankDigestBranches(ctx, digestRanking{
		definition: definition, generatedAt: generatedAt, settings: &settings,
	})
	consumed, err := s.consumedItemIDs(ctx, definition.ID, branches)
	if err != nil {
		return err
	}
	for index := range branches {
		branches[index].candidates = withoutConsumed(branches[index].candidates, consumed)
	}
	issue, err := s.newDigestIssue(ctx, issueBuild{
		definition: definition, arguments: arguments, branches: branches, generatedAt: generatedAt,
	})
	if err != nil {
		return err
	}
	sections, err := s.buildSections(ctx, branches)
	if err != nil {
		return err
	}
	issue.Sections = sections
	issue.GenerationJobID = execution.JobID
	return s.publishGeneration(ctx, issue, execution)
}

func (s *Service) publishGeneration(ctx context.Context, issue domain.DigestIssue, execution jobs.Execution) error {
	result, err := s.repository.PublishDigestIssue(ctx, domain.DigestPublication{Issue: issue, LeaseOwner: execution.LeaseOwner, Attempt: execution.Attempt})
	if errors.Is(err, domain.ErrDigestPublicationRejected) {
		return jobs.Permanent(err)
	}
	if err != nil {
		return fmt.Errorf("publish digest issue: %w", err)
	}
	if result.Created {
		s.events.NotifyDigestReady(s.workspaceID, result.Issue.ID)
	}
	return nil
}

func (s *Service) recoverGeneration(ctx context.Context, arguments generationArguments) (jobs.Execution, bool, error) {
	execution, valid := jobs.CurrentExecution(ctx)
	if !valid || execution.WorkspaceID != s.workspaceID || execution.JobID == uuid.Nil || execution.LeaseOwner == "" || execution.Attempt < 1 {
		return jobs.Execution{}, false, jobs.Permanent(domain.ErrDigestPublicationRejected)
	}
	issue, found, err := s.repository.GetDigestIssueForJob(ctx, s.workspaceID, execution.JobID)
	if err != nil {
		return jobs.Execution{}, false, fmt.Errorf("recover digest issue: %w", err)
	}
	if found && (issue.DigestID != arguments.DigestID || issue.GenerationJobID != execution.JobID) {
		return jobs.Execution{}, false, jobs.Permanent(domain.ErrDigestPublicationRejected)
	}
	return execution, found, nil
}

func decodeGenerationArguments(payload json.RawMessage) (generationArguments, error) {
	var arguments generationArguments
	if err := json.Unmarshal(payload, &arguments); err != nil {
		return generationArguments{}, jobs.Permanent(errors.New("invalid generate-digest arguments"))
	}
	if arguments.DigestID == uuid.Nil {
		return generationArguments{}, jobs.Permanent(errors.New("invalid generate-digest arguments"))
	}
	return arguments, nil
}

func (s *Service) generationDefinition(ctx context.Context, digestID uuid.UUID) (domain.DigestDefinition, error) {
	definition, err := s.get(ctx, digestID)
	if errors.Is(err, ErrNotFound) {
		return domain.DigestDefinition{}, jobs.Permanent(err)
	}
	return definition, err
}

// newDigestIssue retries partial branch failures until the final Job attempt, when it may emit a
// degraded issue as long as at least one branch succeeded.
func (s *Service) newDigestIssue(ctx context.Context, input issueBuild) (domain.DigestIssue, error) {
	failures, successes := branchOutcomes(input.branches)
	if failures > 0 && !finalAttempt(ctx) {
		return domain.DigestIssue{}, fmt.Errorf("%d digest topic branches failed", failures)
	}
	if successes == 0 {
		return domain.DigestIssue{}, errors.New("all digest topic branches failed")
	}
	status := domain.DigestIssueComplete
	if failures > 0 {
		status = domain.DigestIssueDegraded
	}
	return domain.DigestIssue{
		ID: uuid.New(), WorkspaceID: s.workspaceID, DigestID: input.definition.ID,
		Title: digestTitle(input.definition, input.generatedAt), Status: status,
		ScheduledFor: input.arguments.ScheduledFor, GeneratedAt: input.generatedAt,
	}, nil
}

func (s *Service) consumedItemIDs(ctx context.Context, digestID uuid.UUID, branches []digestBranch) (map[string]struct{}, error) {
	seen := make(map[uuid.UUID]struct{})
	var candidates []uuid.UUID
	for _, branch := range branches {
		for _, candidate := range branch.candidates {
			id, err := uuid.Parse(candidate.ItemID)
			if err != nil {
				// Invalid IDs cannot have been consumed; itemForDigest validates selected IDs.
				continue
			}
			if _, duplicate := seen[id]; !duplicate {
				seen[id] = struct{}{}
				candidates = append(candidates, id)
			}
		}
	}
	result := make(map[string]struct{})
	const batchSize = 1000
	for start := 0; start < len(candidates); start += batchSize {
		batch := candidates[start:min(start+batchSize, len(candidates))]
		ids, err := s.repository.FindConsumedDigestItemIDs(ctx, s.workspaceID, digestID, batch)
		if err != nil {
			return nil, fmt.Errorf("find consumed digest items: %w", err)
		}
		for _, itemID := range ids {
			result[itemID.String()] = struct{}{}
		}
	}
	return result, nil
}

func (s *Service) rankDigestBranches(
	ctx context.Context,
	input digestRanking,
) []digestBranch {
	result := make([]digestBranch, 0, len(input.definition.Topics))
	publishedAfter := input.generatedAt.Add(-30 * 24 * time.Hour)
	if input.definition.LastSuccessfulAt != nil {
		publishedAfter = *input.definition.LastSuccessfulAt
	}
	for _, relationship := range input.definition.Topics {
		branch := digestBranch{relationship: relationship}
		topic, found, err := s.topics.GetTopic(ctx, s.workspaceID, relationship.TopicID)
		if err != nil || !found || !topic.Enabled {
			branch.failure = failureMessage("load topic", err)
			result = append(result, branch)
			continue
		}
		branch.topic = topic
		ranked, err := s.ranker.RankTopic(ctx, ranking.TopicRequest{
			Topic: topic, Purpose: ranking.PurposeDigest, PublishedAfter: &publishedAfter,
			PublishedBefore: &input.generatedAt, Limit: input.settings.Retrieval.CandidateLimit,
			WorkspaceSettings: input.settings,
		})
		if err != nil {
			branch.failure = failureMessage("rank topic", err)
		} else {
			branch.candidates = ranked.Candidates
			branch.pipeline = ranked.Inputs.PipelineVersion
		}
		result = append(result, branch)
	}
	return result
}

func (s *Service) workspaceSettings(ctx context.Context) (domain.WorkspaceSettings, error) {
	settings, err := s.settings.GetWorkspace(ctx)
	if err != nil {
		return domain.WorkspaceSettings{}, fmt.Errorf("get digest settings: %w", err)
	}
	if settings.Retrieval.CandidateLimit < 1 {
		return domain.WorkspaceSettings{}, errors.New("digest settings are invalid")
	}
	return settings, nil
}

func failureMessage(action string, err error) *string {
	message := action
	if err != nil {
		message += ": " + err.Error()
	}
	return &message
}

func withoutConsumed(candidates []ranking.Candidate, consumed map[string]struct{}) []ranking.Candidate {
	result := make([]ranking.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, found := consumed[candidate.ItemID]; !found {
			result = append(result, candidate)
		}
	}
	return result
}

func branchOutcomes(branches []digestBranch) (int, int) {
	var failures int
	for _, branch := range branches {
		if branch.failure != nil {
			failures++
		}
	}
	return failures, len(branches) - failures
}

func finalAttempt(ctx context.Context) bool {
	execution, found := jobs.CurrentExecution(ctx)
	return found && execution.Attempt >= execution.MaxAttempts
}

func (s *Service) buildSections(ctx context.Context, branches []digestBranch) ([]domain.DigestSection, error) {
	selected := assignCandidates(branches)
	sections := make([]domain.DigestSection, len(branches))
	for branchIndex, branch := range branches {
		section := domain.DigestSection{
			TopicID: branch.relationship.TopicID, TopicName: branch.topic.Name,
			TopicRevision: branch.topic.Revision, Failure: branch.failure,
		}
		for _, option := range selected[branchIndex] {
			item, found, err := s.itemForDigest(ctx, option.candidate.ItemID)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			section.Items = append(section.Items, domain.DigestItem{
				Item: item, Summary: digestItemSummary(item), Score: option.candidate.Score,
				Signals: option.candidate.Signals, Pipeline: branch.pipeline,
			})
		}
		sections[branchIndex] = section
	}
	return sections, nil
}

// assignCandidates places an Item in at most one section, preferring its best rank and then the
// earlier Digest Definition branch when ranks tie.
func assignCandidates(branches []digestBranch) [][]candidateOption {
	byItem := make(map[string][]candidateOption)
	for branchIndex, branch := range branches {
		if branch.failure != nil {
			continue
		}
		for rank, candidate := range branch.candidates {
			byItem[candidate.ItemID] = append(byItem[candidate.ItemID], candidateOption{
				branch: branchIndex, rank: rank, candidate: candidate,
			})
		}
	}
	items := sortedItemOptions(byItem)
	selected := make([][]candidateOption, len(branches))
	for _, item := range items {
		for _, option := range item.options {
			if len(selected[option.branch]) < branches[option.branch].relationship.ItemLimit {
				selected[option.branch] = append(selected[option.branch], option)
				break
			}
		}
	}
	for index := range selected {
		sort.Slice(selected[index], func(left, right int) bool {
			return selected[index][left].rank < selected[index][right].rank
		})
	}
	return selected
}

func sortedItemOptions(byItem map[string][]candidateOption) []itemOptions {
	result := make([]itemOptions, 0, len(byItem))
	for itemID, options := range byItem {
		sort.Slice(options, func(left, right int) bool {
			if options[left].rank == options[right].rank {
				return options[left].branch < options[right].branch
			}
			return options[left].rank < options[right].rank
		})
		result = append(result, itemOptions{itemID: itemID, options: options})
	}
	sort.Slice(result, func(left, right int) bool {
		leftRank, rightRank := result[left].options[0].rank, result[right].options[0].rank
		if leftRank == rightRank {
			return result[left].itemID < result[right].itemID
		}
		return leftRank < rightRank
	})
	return result
}

func (s *Service) itemForDigest(ctx context.Context, rawID string) (domain.Item, bool, error) {
	itemID, err := uuid.Parse(rawID)
	if err != nil {
		return domain.Item{}, false, fmt.Errorf("ranked digest item has invalid ID: %w", err)
	}
	item, found, err := s.items.GetItem(ctx, s.workspaceID, itemID)
	if err != nil {
		return domain.Item{}, false, fmt.Errorf("load digest item: %w", err)
	}
	return item, found, nil
}

func digestItemSummary(item domain.Item) string {
	if summary := strings.TrimSpace(item.Excerpt); summary != "" {
		return summary
	}
	return strings.TrimSpace(item.Title)
}

func digestTitle(definition domain.DigestDefinition, generatedAt time.Time) string {
	location, err := time.LoadLocation(definition.Timezone)
	if err != nil {
		location = time.UTC
	}
	return fmt.Sprintf("%s — %s", definition.Name, generatedAt.In(location).Format("2006-01-02"))
}
