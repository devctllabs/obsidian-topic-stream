package digests_test

import (
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
	"github.com/stretchr/testify/require"
)

func TestNextRunSupportsDailyWeeklyAndMonthlyRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		recurrence domain.DigestRecurrence
		after      string
		want       string
	}{
		{
			name: "daily", recurrence: domain.DigestRecurrence{Frequency: domain.RecurrenceDaily, Hour: 12},
			after: "2026-09-27T13:00:00Z", want: "2026-09-28T12:00:00Z",
		},
		{
			name: "weekly day subset", recurrence: domain.DigestRecurrence{
				Frequency: domain.RecurrenceWeekly, Weekdays: []int{1, 4}, Hour: 9, Minute: 15,
			},
			after: "2026-09-28T10:00:00Z", want: "2026-10-01T09:15:00Z",
		},
		{
			name: "monthly day", recurrence: domain.DigestRecurrence{
				Frequency: domain.RecurrenceMonthly, Hour: 8,
				Monthly: &domain.MonthlyRule{Type: domain.MonthlyDayOfMonth, Day: 15},
			},
			after: "2026-09-16T00:00:00Z", want: "2026-10-15T08:00:00Z",
		},
		{
			name: "monthly nth weekday", recurrence: domain.DigestRecurrence{
				Frequency: domain.RecurrenceMonthly, Hour: 8,
				Monthly: &domain.MonthlyRule{Type: domain.MonthlyNthWeekday, Weekday: 1, Occurrence: 2},
			},
			after: "2026-09-01T00:00:00Z", want: "2026-09-14T08:00:00Z",
		},
		{
			name: "monthly last day", recurrence: domain.DigestRecurrence{
				Frequency: domain.RecurrenceMonthly, Hour: 8,
				Monthly: &domain.MonthlyRule{Type: domain.MonthlyLastDay},
			},
			after: "2026-02-01T00:00:00Z", want: "2026-02-28T08:00:00Z",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			after, err := time.Parse(time.RFC3339, test.after)
			require.NoError(t, err)
			want, err := time.Parse(time.RFC3339, test.want)
			require.NoError(t, err)

			got, err := digests.NextRun(test.recurrence, "UTC", after)

			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

func TestNextRunUsesFirstValidInstantForMissingDSTTime(t *testing.T) {
	t.Parallel()

	after := time.Date(2026, time.March, 7, 12, 0, 0, 0, time.UTC)
	next, err := digests.NextRun(domain.DigestRecurrence{
		Frequency: domain.RecurrenceDaily, Hour: 2, Minute: 30,
	}, "America/New_York", after)

	require.NoError(t, err)
	require.True(t, time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC).Equal(next), next)
}

func TestNextRunExecutesRepeatedDSTTimeOnlyOnce(t *testing.T) {
	t.Parallel()

	firstOccurrence := time.Date(2026, time.November, 1, 5, 30, 0, 0, time.UTC)
	next, err := digests.NextRun(domain.DigestRecurrence{
		Frequency: domain.RecurrenceDaily, Hour: 1, Minute: 30,
	}, "America/New_York", firstOccurrence)

	require.NoError(t, err)
	require.True(t, time.Date(2026, time.November, 2, 6, 30, 0, 0, time.UTC).Equal(next), next)
}
