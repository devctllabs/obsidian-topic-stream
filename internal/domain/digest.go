package domain

import (
	"time"

	"github.com/google/uuid"
)

// RecurrenceFrequency selects a daily, weekly, or monthly local calendar schedule.
type RecurrenceFrequency string

// MonthlyRuleType selects a calendar-day or weekday-based monthly recurrence.
type MonthlyRuleType string

const (
	RecurrenceDaily   RecurrenceFrequency = "daily"
	RecurrenceWeekly  RecurrenceFrequency = "weekly"
	RecurrenceMonthly RecurrenceFrequency = "monthly"
)

const (
	MonthlyDayOfMonth MonthlyRuleType = "dayOfMonth"
	MonthlyNthWeekday MonthlyRuleType = "nthWeekday"
	MonthlyLastDay    MonthlyRuleType = "lastDay"
)

// DigestRecurrence describes a wall-clock schedule in the Digest Definition's timezone.
type DigestRecurrence struct {
	Frequency RecurrenceFrequency `json:"frequency"`
	// Hour is local wall-clock time in the Digest Definition's timezone, from 0 through 23.
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
	// Weekdays uses ISO numbering: Monday is 1 and Sunday is 7.
	Weekdays []int `json:"weekdays,omitempty"`
	// Monthly is required only for monthly recurrence.
	Monthly *MonthlyRule `json:"monthly,omitempty"`
}

// MonthlyRule specifies the day selection for a monthly Digest recurrence.
type MonthlyRule struct {
	Type MonthlyRuleType `json:"type"`
	// Day selects 1 through 31 for dayOfMonth; months without that day have no matching date.
	Day int `json:"day,omitempty"`
	// Weekday uses ISO numbering, Monday 1 through Sunday 7, for nthWeekday.
	Weekday int `json:"weekday,omitempty"`
	// Occurrence selects the first through fifth matching weekday; absent occurrences are skipped.
	Occurrence int `json:"occurrence,omitempty"`
}

// DigestTopic selects one ordered Topic section and its maximum Item count.
type DigestTopic struct {
	TopicID   uuid.UUID `json:"topicId"`
	ItemLimit int       `json:"itemLimit"`
}

// DigestDefinition configures ordered Topic sections and optional scheduled generation.
type DigestDefinition struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Name        string
	Topics      []DigestTopic
	Timezone    string
	Recurrence  DigestRecurrence
	Enabled     bool
	// Version fences concurrent edits independently of Topic semantic revisions.
	Version int
	// NextRunAt is nil when automatic generation is disabled.
	NextRunAt        *time.Time
	LastSuccessfulAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// DigestDelete removes a Digest Definition using optimistic version checking.
type DigestDelete struct {
	WorkspaceID     uuid.UUID
	DigestID        uuid.UUID
	ExpectedVersion int
	DeletedAt       time.Time
}

// DigestDeletion reports the committed deletion and Jobs whose cancellation state changed.
type DigestDeletion struct {
	Deleted bool
	JobIDs  []uuid.UUID
}

// DigestBuildRequestKind separates manual identities from Retry source Job identities.
type DigestBuildRequestKind string

const (
	DigestBuildManual DigestBuildRequestKind = "manual"
	DigestBuildRetry  DigestBuildRequestKind = "retry"
)

// DigestBuildRequest binds a permanent request identity to one generation Job.
type DigestBuildRequest struct {
	WorkspaceID uuid.UUID
	DigestID    uuid.UUID
	RequestID   uuid.UUID
	Kind        DigestBuildRequestKind
	Job         Job
}

// DigestBuildAdmission reports committed work and whether a new Job needs announcing.
type DigestBuildAdmission struct {
	Job     Job
	Created bool
}

// DigestScheduleAdvance compares a scheduled instant before moving the next run.
type DigestScheduleAdvance struct {
	WorkspaceID uuid.UUID
	DigestID    uuid.UUID
	Expected    time.Time
	NextRunAt   time.Time
	UpdatedAt   time.Time
}

// DigestScheduledBuild admits one observed recurrence and advances its calendar atomically.
type DigestScheduledBuild struct {
	Advance         DigestScheduleAdvance
	ExpectedVersion int
	Job             Job
}

// DigestIssueStatus distinguishes complete generation from issues with failed sections.
type DigestIssueStatus string

const (
	DigestIssueComplete DigestIssueStatus = "complete"
	DigestIssueDegraded DigestIssueStatus = "degraded"
)

// DigestIssue is an immutable record of one completed generation attempt.
type DigestIssue struct {
	GenerationJobID uuid.UUID
	ID              uuid.UUID
	WorkspaceID     uuid.UUID
	DigestID        uuid.UUID
	Title           string
	Status          DigestIssueStatus
	// ScheduledFor is nil for an on-demand issue.
	ScheduledFor *time.Time
	GeneratedAt  time.Time
	Sections     []DigestSection
}

// DigestPublication submits an immutable snapshot with its actual worker attempt fence.
type DigestPublication struct {
	Issue      DigestIssue
	LeaseOwner string
	Attempt    int
}

// DigestPublicationResult distinguishes a new commit from recovery of the same Job's result.
type DigestPublicationResult struct {
	Issue   DigestIssue
	Created bool
}

// DigestIssueSummary describes a saved Issue without loading its immutable content.
type DigestIssueSummary struct {
	GenerationJobID uuid.UUID
	ID              uuid.UUID
	WorkspaceID     uuid.UUID
	DigestID        uuid.UUID
	Title           string
	Status          DigestIssueStatus
	ScheduledFor    *time.Time
	GeneratedAt     time.Time
}

// DigestIssueCursor identifies the last summary in descending history order.
type DigestIssueCursor struct {
	GeneratedAt time.Time
	ID          uuid.UUID
}

// DigestIssueList selects a bounded summary page within a Workspace.
type DigestIssueList struct {
	WorkspaceID uuid.UUID
	DigestID    *uuid.UUID
	Cursor      *DigestIssueCursor
	Limit       int
}

// DigestSection freezes a Topic revision and its ranked Items or generation failure.
type DigestSection struct {
	TopicID       uuid.UUID
	TopicName     string
	TopicRevision int
	Items         []DigestItem
	// Failure records a failed section without discarding successful sections in the same issue.
	Failure *string
}

// DigestItem freezes canonical Item content and its ranking explanation in an issue.
type DigestItem struct {
	// AISummary is a read-time annotation stored separately from the immutable Issue snapshot.
	AISummary *ItemSummary `json:"-"`
	Item      Item
	Summary   string
	Score     float64
	Signals   map[string]float64
	Pipeline  string
}
