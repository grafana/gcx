package gcxerrors_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
)

func TestEmittedError_UnwrapAndAs(t *testing.T) {
	cause := errors.New("3 resources failed")
	sentinel := gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, cause)
	wrapped := fmt.Errorf("push: %w", sentinel)

	var emitted *gcxerrors.EmittedError
	if !errors.As(wrapped, &emitted) {
		t.Fatal("errors.As failed to find EmittedError through wrapping")
	}
	if emitted.Code != gcxerrors.ExitPartialFailure {
		t.Fatalf("Code = %d, want %d", emitted.Code, gcxerrors.ExitPartialFailure)
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
	if got := gcxerrors.NewEmittedError(4, errors.New("boom")).Error(); got != "result already emitted (exit 4): boom" {
		t.Fatalf("Error() = %q", got)
	}
	if got := gcxerrors.NewEmittedError(1, nil).Error(); got != "result already emitted (exit 1)" {
		t.Fatalf("Error() with nil cause = %q", got)
	}
}
