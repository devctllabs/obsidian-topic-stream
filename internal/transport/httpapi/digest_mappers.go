package httpapi

import (
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
)

func domainDigestInput(input serverhttp.DigestInput) (digests.Input, error) {
	recurrence, err := domainRecurrence(input.Recurrence)
	if err != nil {
		return digests.Input{}, err
	}
	return digests.Input{
		Name: input.Name, Topics: domainDigestTopics(input.Topics), Timezone: input.Timezone,
		Recurrence: recurrence, Enabled: input.Enabled,
	}, nil
}

func domainDigestPatch(input serverhttp.DigestPatch) (digests.Patch, error) {
	patch := digests.Patch{Name: input.Name, Timezone: input.Timezone, Enabled: input.Enabled}
	if input.Topics != nil {
		topics := domainDigestTopics(*input.Topics)
		patch.Topics = &topics
	}
	if input.Recurrence != nil {
		recurrence, err := domainRecurrence(*input.Recurrence)
		if err != nil {
			return digests.Patch{}, err
		}
		patch.Recurrence = &recurrence
	}
	return patch, nil
}

func domainDigestTopics(input []serverhttp.DigestTopicInput) []domain.DigestTopic {
	result := make([]domain.DigestTopic, len(input))
	for index, relationship := range input {
		result[index] = domain.DigestTopic{TopicID: relationship.TopicId, ItemLimit: relationship.ItemLimit}
	}
	return result
}

func domainRecurrence(input serverhttp.Recurrence) (domain.DigestRecurrence, error) {
	kind, err := input.Discriminator()
	if err != nil {
		return domain.DigestRecurrence{}, err
	}
	switch kind {
	case "daily":
		value, err := input.AsDailyRecurrence()
		return domain.DigestRecurrence{Frequency: domain.RecurrenceDaily, Hour: value.Hour, Minute: value.Minute}, err
	case "weekly":
		value, err := input.AsWeeklyRecurrence()
		return domain.DigestRecurrence{
			Frequency: domain.RecurrenceWeekly, Weekdays: append([]int(nil), value.Weekdays...),
			Hour: value.Hour, Minute: value.Minute,
		}, err
	case "monthly":
		return domainMonthlyRecurrence(input)
	default:
		return domain.DigestRecurrence{}, fmt.Errorf("unsupported recurrence frequency %q", kind)
	}
}

func domainMonthlyRecurrence(input serverhttp.Recurrence) (domain.DigestRecurrence, error) {
	value, err := input.AsMonthlyRecurrence()
	if err != nil {
		return domain.DigestRecurrence{}, err
	}
	rule, err := domainMonthlyRule(value.Rule)
	if err != nil {
		return domain.DigestRecurrence{}, err
	}
	return domain.DigestRecurrence{
		Frequency: domain.RecurrenceMonthly, Monthly: &rule, Hour: value.Hour, Minute: value.Minute,
	}, nil
}

func domainMonthlyRule(input serverhttp.MonthlyRecurrence_Rule) (domain.MonthlyRule, error) {
	kind, err := input.Discriminator()
	if err != nil {
		return domain.MonthlyRule{}, err
	}
	switch kind {
	case "dayOfMonth":
		value, err := input.AsMonthlyDayOfMonthRule()
		return domain.MonthlyRule{Type: domain.MonthlyDayOfMonth, Day: value.Day}, err
	case "nthWeekday":
		value, err := input.AsMonthlyNthWeekdayRule()
		return domain.MonthlyRule{
			Type: domain.MonthlyNthWeekday, Weekday: value.Weekday, Occurrence: value.Occurrence,
		}, err
	case "lastDay":
		_, err := input.AsMonthlyLastDayRule()
		return domain.MonthlyRule{Type: domain.MonthlyLastDay}, err
	default:
		return domain.MonthlyRule{}, fmt.Errorf("unsupported monthly recurrence rule %q", kind)
	}
}

func apiDigestDefinition(value domain.DigestDefinition) (serverhttp.DigestDefinition, error) {
	recurrence, err := apiRecurrence(value.Recurrence)
	if err != nil {
		return serverhttp.DigestDefinition{}, err
	}
	return serverhttp.DigestDefinition{
		Id: value.ID, Name: value.Name, Topics: apiDigestTopics(value.Topics), Timezone: value.Timezone,
		Recurrence: recurrence, Enabled: value.Enabled, Version: value.Version,
		NextRunAt: value.NextRunAt, LastSuccessfulAt: value.LastSuccessfulAt,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}, nil
}

func apiDigestTopics(input []domain.DigestTopic) []serverhttp.DigestTopicInput {
	result := make([]serverhttp.DigestTopicInput, len(input))
	for index, relationship := range input {
		result[index] = serverhttp.DigestTopicInput{TopicId: relationship.TopicID, ItemLimit: relationship.ItemLimit}
	}
	return result
}

func apiRecurrence(value domain.DigestRecurrence) (serverhttp.Recurrence, error) {
	var result serverhttp.Recurrence
	switch value.Frequency {
	case domain.RecurrenceDaily:
		err := result.FromDailyRecurrence(serverhttp.DailyRecurrence{Hour: value.Hour, Minute: value.Minute})
		return result, err
	case domain.RecurrenceWeekly:
		err := result.FromWeeklyRecurrence(serverhttp.WeeklyRecurrence{
			Weekdays: append([]int(nil), value.Weekdays...), Hour: value.Hour, Minute: value.Minute,
		})
		return result, err
	case domain.RecurrenceMonthly:
		return apiMonthlyRecurrence(value)
	default:
		return result, fmt.Errorf("unsupported recurrence frequency %q", value.Frequency)
	}
}

func apiMonthlyRecurrence(value domain.DigestRecurrence) (serverhttp.Recurrence, error) {
	if value.Monthly == nil {
		return serverhttp.Recurrence{}, fmt.Errorf("monthly recurrence rule is required")
	}
	rule, err := apiMonthlyRule(*value.Monthly)
	if err != nil {
		return serverhttp.Recurrence{}, err
	}
	var result serverhttp.Recurrence
	err = result.FromMonthlyRecurrence(serverhttp.MonthlyRecurrence{
		Hour: value.Hour, Minute: value.Minute, Rule: rule,
	})
	return result, err
}

func apiMonthlyRule(value domain.MonthlyRule) (serverhttp.MonthlyRecurrence_Rule, error) {
	var result serverhttp.MonthlyRecurrence_Rule
	var err error
	switch value.Type {
	case domain.MonthlyDayOfMonth:
		err = result.FromMonthlyDayOfMonthRule(serverhttp.MonthlyDayOfMonthRule{Day: value.Day})
	case domain.MonthlyNthWeekday:
		err = result.FromMonthlyNthWeekdayRule(serverhttp.MonthlyNthWeekdayRule{
			Weekday: value.Weekday, Occurrence: value.Occurrence,
		})
	case domain.MonthlyLastDay:
		err = result.FromMonthlyLastDayRule(serverhttp.MonthlyLastDayRule{})
	default:
		err = fmt.Errorf("unsupported monthly recurrence rule %q", value.Type)
	}
	return result, err
}

func apiDigestIssue(value domain.DigestIssue) (serverhttp.DigestIssue, error) {
	sections := make([]serverhttp.DigestSection, len(value.Sections))
	for index, section := range value.Sections {
		mapped, err := apiDigestSection(section)
		if err != nil {
			return serverhttp.DigestIssue{}, err
		}
		sections[index] = mapped
	}
	return serverhttp.DigestIssue{
		Id: value.ID, DigestId: value.DigestID, GenerationJobId: value.GenerationJobID, Title: value.Title,
		Status: serverhttp.DigestIssueStatus(value.Status), ScheduledFor: value.ScheduledFor,
		GeneratedAt: value.GeneratedAt, Sections: sections,
	}, nil
}

func apiDigestSection(value domain.DigestSection) (serverhttp.DigestSection, error) {
	items := make([]serverhttp.DigestItem, len(value.Items))
	for index, digestItem := range value.Items {
		item, err := apiItem(digestItem.Item)
		if err != nil {
			return serverhttp.DigestSection{}, err
		}
		items[index] = serverhttp.DigestItem{
			Item: item, Summary: digestItem.Summary,
			Explanation: serverhttp.FeedExplanation{
				Summary: digestItem.Summary, Signals: float32Signals(digestItem.Signals),
				PipelineVersion: digestItem.Pipeline,
			},
		}
		if digestItem.AISummary != nil {
			annotation := apiItemSummary(*digestItem.AISummary)
			items[index].AiSummary = &annotation
		}
	}
	return serverhttp.DigestSection{
		TopicId: value.TopicID, TopicName: value.TopicName, TopicRevision: value.TopicRevision,
		Items: items, Failure: value.Failure,
	}, nil
}

func float32Signals(input map[string]float64) map[string]float32 {
	result := make(map[string]float32, len(input))
	for name, value := range input {
		result[name] = float32(value)
	}
	return result
}
