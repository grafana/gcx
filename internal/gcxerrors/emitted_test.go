package gcxerrors

import (
	"errors"
	"fmt"
	"testing"
)

func TestEmittedError_UnwrapAndAs(t *testing.T) {
	cause := errors.New("3 resources failed")
	sentinel := NewEmittedError(ExitPartialFailure, cause)
	wrapped := fmt.Errorf("push: %w", sentinel)

	var emitted *EmittedError
	if !errors.As(wrapped, &emitted) {
		t.Fatal("errors.As failed to find EmittedError through wrapping")
	}
	if emitted.Code != ExitPartialFailure {
		t.Fatalf("Code = %d, want %d", emitted.Code, ExitPartialFailure)
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("errors.Is failed to find the cause through EmittedError.Unwrap")
	}
	// Pointer identity through wrapping: the pattern wait call sites use.
	if !errors.Is(wrapped, sentinel) {
		t.Fatal("errors.Is failed to match the sentinel instance itself")
	}
}

func TestEmittedError_ErrorString(t *testing.T) {
	if got := NewEmittedError(4, errors.New("boom")).Error(); got != "result already emitted (exit 4): boom" {
		t.Fatalf("Error() = %q", got)
	}
	if got := NewEmittedError(1, nil).Error(); got != "result already emitted (exit 1)" {
		t.Fatalf("Error() with nil cause = %q", got)
	}
}
