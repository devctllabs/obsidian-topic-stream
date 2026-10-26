package digests

import (
	"errors"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

const scheduleLookaheadDays = 370

// NextRun returns the first real instant after the supplied time that satisfies the local calendar
// rule. During a DST gap it advances to the first valid local minute after the requested wall time.
func NextRun(recurrence domain.DigestRecurrence, timezone string, after time.Time) (time.Time, error) {
	location, err := time.LoadLocation(timezone)
	var timezoneErr error
	if err != nil {
		timezoneErr = domain.NewValidationError(domain.Violation{Field: "timezone", Rule: domain.ValidationTimezone})
	}
	if err := domain.JoinValidation(timezoneErr, validateRecurrence(recurrence)); err != nil {
		return time.Time{}, err
	}
	local := after.In(location)
	year, month, day := local.Date()
	date := time.Date(year, month, day, 12, 0, 0, 0, location)
	for range scheduleLookaheadDays {
		if recurrenceMatchesDate(recurrence, date) {
			candidate, found := firstLocalInstant(date, recurrence.Hour, recurrence.Minute)
			if found && candidate.After(after) {
				return candidate, nil
			}
		}
		date = date.AddDate(0, 0, 1)
	}
	return time.Time{}, errors.New("digest recurrence has no run within the lookahead window")
}

func validateRecurrence(recurrence domain.DigestRecurrence) error {
	validators := map[domain.RecurrenceFrequency]func(domain.DigestRecurrence) error{
		domain.RecurrenceDaily:   func(domain.DigestRecurrence) error { return nil },
		domain.RecurrenceWeekly:  validateWeeklyRecurrence,
		domain.RecurrenceMonthly: func(value domain.DigestRecurrence) error { return validateMonthlyRule(value.Monthly) },
	}
	validate, found := validators[recurrence.Frequency]
	var frequencyErr error
	if !found {
		frequencyErr = domain.CheckChoice("recurrence.frequency", string(recurrence.Frequency), "daily", "weekly", "monthly")
	} else {
		frequencyErr = validate(recurrence)
	}
	return domain.JoinValidation(domain.CheckRange("recurrence.hour", float64(recurrence.Hour), 0, 23),
		domain.CheckRange("recurrence.minute", float64(recurrence.Minute), 0, 59), frequencyErr)
}

func validateWeeklyRecurrence(recurrence domain.DigestRecurrence) error {
	checks := []error{domain.CheckRequired("recurrence.weekdays", len(recurrence.Weekdays) > 0)}
	for _, weekday := range recurrence.Weekdays {
		checks = append(checks, domain.CheckRange("recurrence.weekdays", float64(weekday), 1, 7))
	}
	return domain.JoinValidation(checks...)
}

func validateMonthlyRule(rule *domain.MonthlyRule) error {
	if rule == nil {
		return domain.CheckRequired("recurrence.rule", false)
	}
	switch rule.Type {
	case domain.MonthlyDayOfMonth:
		return domain.CheckRange("recurrence.rule.day", float64(rule.Day), 1, 31)
	case domain.MonthlyNthWeekday:
		return domain.JoinValidation(domain.CheckRange("recurrence.rule.weekday", float64(rule.Weekday), 1, 7),
			domain.CheckRange("recurrence.rule.occurrence", float64(rule.Occurrence), 1, 5))
	case domain.MonthlyLastDay:
		return nil
	default:
		return domain.CheckChoice("recurrence.rule.type", string(rule.Type), "dayOfMonth", "nthWeekday", "lastDay")
	}
}

func recurrenceMatchesDate(recurrence domain.DigestRecurrence, date time.Time) bool {
	switch recurrence.Frequency {
	case domain.RecurrenceDaily:
		return true
	case domain.RecurrenceWeekly:
		return containsWeekday(recurrence.Weekdays, isoWeekday(date.Weekday()))
	case domain.RecurrenceMonthly:
		return monthlyRuleMatches(*recurrence.Monthly, date)
	default:
		return false
	}
}

func monthlyRuleMatches(rule domain.MonthlyRule, date time.Time) bool {
	switch rule.Type {
	case domain.MonthlyDayOfMonth:
		return date.Day() == rule.Day
	case domain.MonthlyNthWeekday:
		return isoWeekday(date.Weekday()) == rule.Weekday && (date.Day()-1)/7+1 == rule.Occurrence
	case domain.MonthlyLastDay:
		return date.Day() == time.Date(date.Year(), date.Month()+1, 0, 12, 0, 0, 0, date.Location()).Day()
	default:
		return false
	}
}

func firstLocalInstant(date time.Time, hour, minute int) (time.Time, bool) {
	year, month, day := date.Date()
	targetMinute := hour*60 + minute
	start := time.Date(year, month, day, 12, 0, 0, 0, date.Location()).Add(-16 * time.Hour)
	for elapsed := time.Duration(0); elapsed <= 32*time.Hour; elapsed += time.Minute {
		candidate := start.Add(elapsed)
		candidateYear, candidateMonth, candidateDay := candidate.In(date.Location()).Date()
		if candidateYear != year || candidateMonth != month || candidateDay != day {
			continue
		}
		local := candidate.In(date.Location())
		if local.Hour()*60+local.Minute() >= targetMinute {
			return candidate, true
		}
	}
	return time.Time{}, false
}

func containsWeekday(weekdays []int, wanted int) bool {
	for _, weekday := range weekdays {
		if weekday == wanted {
			return true
		}
	}
	return false
}

func isoWeekday(weekday time.Weekday) int {
	if weekday == time.Sunday {
		return 7
	}
	return int(weekday)
}
