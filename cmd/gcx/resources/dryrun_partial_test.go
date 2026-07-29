package resources //nolint:testpackage // exercises the unexported partialBatchFailure helper directly

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
)

// TestPartialBatchFailure pins that the batch commands surface partial
// failures on stderr: EmittedError suppresses reportError's rendering, so
// without the explicit diagnostic the old "Error: N resource(s) failed to
// <op>" stderr line would silently disappear.
func TestPartialBatchFailure(t *testing.T) {
	for _, agentMode := range []bool{false, true} {
		name := "human"
		if agentMode {
			name = "agent"
		}
		t.Run(name, func(t *testing.T) {
			agent.SetFlag(agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			var stderr bytes.Buffer
			err := partialBatchFailure(&stderr, "push", 3, 2)

			var emitted *gcxerrors.EmittedError
			if !errors.As(err, &emitted) {
				t.Fatalf("partialBatchFailure() = %T (%v), want *gcxerrors.EmittedError", err, err)
			}
			if emitted.Code != gcxerrors.ExitPartialFailure {
				t.Fatalf("EmittedError.Code = %d, want %d", emitted.Code, gcxerrors.ExitPartialFailure)
			}
			if !strings.Contains(stderr.String(), "2 resource(s) failed to push") {
				t.Fatalf("stderr = %q, want the failure count diagnostic", stderr.String())
			}
		})
	}
}
