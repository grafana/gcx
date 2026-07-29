package gcxerrors

import "fmt"

// EmittedError is the sentinel a command returns AFTER it has successfully
// written its complete result document — including any error information —
// to stdout. It is the atomic-stdout-ownership contract: exactly one JSON
// value per finite command in agent mode, even on failure.
//
// The top-level error reporter recognizes this type, exits with Code, and
// writes nothing further to stdout (a second document would corrupt the
// stream for machine consumers). Cause carries the underlying failure for
// logs and diagnostics; it is never re-rendered as output.
//
// Return an EmittedError only when the full result was written without an
// encoder or write error. If encoding failed midway, return the write error
// itself instead — the stream is already broken and the standard error path
// is the honest one.
type EmittedError struct {
	// Code is the process exit code to use (e.g. ExitPartialFailure).
	Code int
	// Cause is the underlying failure, preserved for logs via Unwrap.
	Cause error
}

// NewEmittedError returns an EmittedError carrying the exit code and cause.
func NewEmittedError(code int, cause error) *EmittedError {
	return &EmittedError{Code: code, Cause: cause}
}

func (e *EmittedError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("result already emitted (exit %d)", e.Code)
	}
	return fmt.Sprintf("result already emitted (exit %d): %v", e.Code, e.Cause)
}

// Unwrap exposes the underlying cause to errors.Is/As chains.
func (e *EmittedError) Unwrap() error { return e.Cause }
