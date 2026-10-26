package domain

import (
	"errors"

	"github.com/google/uuid"
)

var (
	ErrSummarySourceNotFound      = errors.New("summary source not found")
	ErrSummaryUnavailable         = errors.New("summary generation is unavailable")
	ErrSummaryPublicationRejected = errors.New("summary generation execution cannot publish")
	ErrDigestPublicationRejected  = errors.New("digest generation execution cannot publish")
	ErrJobNotFound                = errors.New("job not found")
	ErrJobNotRetryable            = errors.New("job is not retryable")
	ErrDigestNotFound             = errors.New("digest not found")
	ErrNotImplemented             = errors.New("domain operation is not implemented")
	ErrWorkspaceNotEmpty          = errors.New("workspace is not empty")
	ErrConflict                   = errors.New("domain conflict")
	ErrModelSyncStalled           = errors.New("model indexing made no progress before its deadline")
	ErrRetrievalUnavailable       = errors.New("retrieval index initialization is incomplete")
)

// ProjectionPendingError reports a committed canonical write whose derived delivery still needs repair.
type ProjectionPendingError struct {
	// Revision identifies the committed projection that remains pending; callers must not assume the write
	// rolled back.
	Revision uuid.UUID
	Cause    error
}

// Error returns the caller-facing failure description.
func (e *ProjectionPendingError) Error() string {
	return "retrieval projection is pending"
}

// Unwrap preserves the underlying cause for errors.Is and errors.As.
func (e *ProjectionPendingError) Unwrap() error {
	return e.Cause
}
