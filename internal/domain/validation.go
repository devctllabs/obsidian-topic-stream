package domain

import "errors"

// ErrValidation identifies caller-correctable input failures independently of a delivery protocol.
var ErrValidation = errors.New("validation failed")

// ValidationRule identifies a violated invariant; presentation belongs to the caller's adapter.
type ValidationRule string

const (
	ValidationRequired     ValidationRule = "required"
	ValidationLength       ValidationRule = "length"
	ValidationRange        ValidationRule = "range"
	ValidationMinimum      ValidationRule = "minimum"
	ValidationPositive     ValidationRule = "positive"
	ValidationMaximum      ValidationRule = "maximum"
	ValidationChoice       ValidationRule = "choice"
	ValidationFormat       ValidationRule = "format"
	ValidationTimezone     ValidationRule = "timezone"
	ValidationHTTPURL      ValidationRule = "http_url"
	ValidationEndpoint     ValidationRule = "endpoint_unavailable"
	ValidationReady        ValidationRule = "not_ready"
	ValidationRelationship ValidationRule = "relationship"
	ValidationDuplicate    ValidationRule = "duplicate"
	ValidationNotEmpty     ValidationRule = "not_empty"
)

// Violation contains a domain field path and approved constraint facts, never the submitted value.
// Numeric bounds apply to range, length, minimum, and maximum rules; Choices applies to choice.
type Violation struct {
	Field   string
	Rule    ValidationRule
	Minimum float64
	Maximum float64
	Choices []string
}

// ValidationError reports independent violations found before an operation can proceed.
type ValidationError struct {
	Violations []Violation
}

// Error returns a diagnostic summary without turning user-facing copy into a domain contract.
func (e *ValidationError) Error() string { return "validation failed" }

// Unwrap preserves the shared caller-actionable validation category.
func (e *ValidationError) Unwrap() error { return ErrValidation }

// NewValidationError returns nil for valid input, or an error containing all supplied violations.
func NewValidationError(violations ...Violation) error {
	if len(violations) == 0 {
		return nil
	}
	return &ValidationError{Violations: violations}
}

// JoinValidation collects independent validation failures and preserves an unexpected failure unchanged.
func JoinValidation(failures ...error) error {
	var violations []Violation
	for _, failure := range failures {
		if failure == nil {
			continue
		}
		var validation *ValidationError
		if !errors.As(failure, &validation) {
			return failure
		}
		violations = append(violations, validation.Violations...)
	}
	return NewValidationError(violations...)
}

// CheckRequired reports a required field whose presence predicate is false.
func CheckRequired(field string, present bool) error {
	return CheckCondition(field, ValidationRequired, present)
}

// CheckCondition reports a failed predicate without retaining the submitted value.
func CheckCondition(field string, rule ValidationRule, valid bool) error {
	if valid {
		return nil
	}
	return NewValidationError(Violation{Field: field, Rule: rule})
}

// CheckRange validates an inclusive numeric interval.
func CheckRange(field string, value, minimum, maximum float64) error {
	if value >= minimum && value <= maximum {
		return nil
	}
	return NewValidationError(Violation{Field: field, Rule: ValidationRange, Minimum: minimum, Maximum: maximum})
}

// CheckMinimum validates an inclusive lower bound without imposing an upper bound.
func CheckMinimum(field string, value, minimum float64) error {
	if value >= minimum {
		return nil
	}
	return NewValidationError(Violation{Field: field, Rule: ValidationMinimum, Minimum: minimum})
}

// CheckLength validates a caller-measured length, preserving its existing byte or rune semantics.
func CheckLength(field string, length, minimum, maximum int) error {
	if length >= minimum && length <= maximum {
		return nil
	}
	return NewValidationError(Violation{Field: field, Rule: ValidationLength, Minimum: float64(minimum), Maximum: float64(maximum)})
}

// CheckChoice validates a closed selection and retains only the allowed choices.
func CheckChoice(field, value string, choices ...string) error {
	for _, choice := range choices {
		if value == choice {
			return nil
		}
	}
	return NewValidationError(Violation{Field: field, Rule: ValidationChoice, Choices: choices})
}
