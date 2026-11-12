package port

import (
	"errors"
	"time"
)

// BackendErrorKind is the common vocabulary every backend failure is read into, whichever adapter
// or plugin produced it. It is what the harness decides recovery on — not the wording of a message.
type BackendErrorKind string

const (
	KindRateLimit       BackendErrorKind = "rate_limit"       // 429 that clears in seconds or minutes
	KindOverloaded      BackendErrorKind = "overloaded"       // 5xx / Anthropic 529: the backend is busy
	KindNetwork         BackendErrorKind = "network"          // the connection or the backend process went away
	KindTimeout         BackendErrorKind = "timeout"          // no answer within the deadline
	KindAuth            BackendErrorKind = "auth"             // 401: credentials
	KindPermission      BackendErrorKind = "permission"       // 403: not allowed
	KindInvalidRequest  BackendErrorKind = "invalid_request"  // 400/404/422: the request itself is wrong
	KindQuota           BackendErrorKind = "quota"            // a 429 that does not clear on a turn's timescale
	KindContextOverflow BackendErrorKind = "context_overflow" // too large; compaction's to fix, not a retry's
	KindUnknown         BackendErrorKind = "unknown"
)

// Retryable reports whether asking again, unchanged and later, can succeed.
func (k BackendErrorKind) Retryable() bool {
	switch k {
	case KindRateLimit, KindOverloaded, KindNetwork, KindTimeout:
		return true
	}
	return false
}

// BackendError carries what a backend said about a failure, structured, alongside the failure
// itself. Error() is the cause's text unchanged, so every reader that still matches on wording
// keeps working; a reader that wants the facts takes them with AsBackendError.
type BackendError struct {
	Kind       BackendErrorKind
	Status     int           // HTTP-ish status when one was given; 0 otherwise
	RetryAfter time.Duration // the wait the backend named, uncapped; 0 when none
	RequestID  string        // the backend's request id, for a support ticket; never request content
	Cause      error
}

func (e *BackendError) Error() string {
	if e.Cause == nil {
		return "backend error (" + string(e.Kind) + ")"
	}
	return e.Cause.Error()
}

func (e *BackendError) Unwrap() error { return e.Cause }

// AsBackendError finds a BackendError anywhere in err's chain.
func AsBackendError(err error) (*BackendError, bool) {
	var be *BackendError
	if errors.As(err, &be) {
		return be, true
	}
	return nil, false
}
