package rmw_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/grafana/gcx/internal/providers/instrumentation/rmw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// simpleString is a trivial type used to test the generic Update function
// without depending on App or Cluster types.
type simpleString = string

// errTransient is a sentinel transient error for testing retry behaviour.
var errTransient = errors.New("transient error")

func TestUpdate(t *testing.T) {
	tests := []struct {
		name string
		// getFn returns values in sequence; the last value is returned for all
		// subsequent calls beyond the sequence length.
		getSequence []simpleString
		// getFn errors; nil means no error for that call index.
		getErrors []error
		// mutateFn appends "-mutated" to the input.
		// setErrors[i] is the error returned by the i-th setFn call.
		setErrors  []error
		maxRetries int
		// wantErr is the expected error type / value checked via errors.Is. nil means success.
		wantErr error
		// wantAnyErr checks that any non-nil error is returned (for dynamic errors).
		wantAnyErr   bool
		wantConflict bool
		// wantSetCalls is the expected number of setFn calls.
		wantSetCalls int
		// wantGetCalls is the expected total number of getFn calls.
		wantGetCalls int
	}{
		{
			name:         "success on first attempt",
			getSequence:  []simpleString{"v1", "v1"},
			setErrors:    []error{nil},
			maxRetries:   3,
			wantSetCalls: 1,
			wantGetCalls: 2,
		},
		{
			name:         "success after one transient setFn error",
			getSequence:  []simpleString{"v1", "v1", "v1", "v1"},
			setErrors:    []error{errTransient, nil},
			maxRetries:   3,
			wantSetCalls: 2,
			wantGetCalls: 4,
		},
		{
			name:         "fails after exhausting maxRetries on persistent transient error",
			getSequence:  []simpleString{"v1", "v1", "v1", "v1", "v1", "v1", "v1", "v1"},
			setErrors:    []error{errTransient, errTransient, errTransient, errTransient},
			maxRetries:   3,
			wantErr:      errTransient,
			wantSetCalls: 4,
			wantGetCalls: 8,
		},
		{
			name: "conflict detected — no retry, no setFn call",
			// snapshot1 == "v1", snapshot2 == "v2" → conflict
			getSequence:  []simpleString{"v1", "v2"},
			maxRetries:   3,
			wantConflict: true,
			wantSetCalls: 0,
			wantGetCalls: 2,
		},
		{
			name:         "maxRetries=0 — single attempt, setFn fails → return error",
			getSequence:  []simpleString{"v1", "v1"},
			setErrors:    []error{errTransient},
			maxRetries:   0,
			wantErr:      errTransient,
			wantSetCalls: 1,
			wantGetCalls: 2,
		},
		{
			name:         "getFn error on first call propagates immediately",
			getErrors:    []error{errors.New("get error")},
			maxRetries:   3,
			wantAnyErr:   true,
			wantSetCalls: 0,
			wantGetCalls: 1,
		},
		{
			name: "getFn error on second call propagates immediately",
			// First get succeeds, second get fails.
			getSequence:  []simpleString{"v1"},
			getErrors:    []error{nil, errors.New("second get error")},
			maxRetries:   3,
			wantAnyErr:   true,
			wantSetCalls: 0,
			wantGetCalls: 2,
		},
		{
			name:        "ConflictError from setFn is not retried",
			getSequence: []simpleString{"v1", "v1"},
			setErrors: []error{rmw.ConflictError{
				Command: "clusters enable",
				Diff:    "costmetrics: false → true",
			}},
			maxRetries:   3,
			wantConflict: true,
			wantSetCalls: 1,
			wantGetCalls: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getCalls := 0
			setCalls := 0

			getFn := func(_ context.Context) (simpleString, error) {
				idx := getCalls
				getCalls++
				// Return error if specified for this index.
				if idx < len(tt.getErrors) && tt.getErrors[idx] != nil {
					return "", tt.getErrors[idx]
				}
				// Return value from sequence; repeat last value when exhausted.
				if idx < len(tt.getSequence) {
					return tt.getSequence[idx], nil
				}
				if len(tt.getSequence) > 0 {
					return tt.getSequence[len(tt.getSequence)-1], nil
				}
				return "", nil
			}

			mutateFn := func(s simpleString) simpleString {
				return s + "-mutated"
			}

			setFn := func(_ context.Context, _ simpleString) error {
				idx := setCalls
				setCalls++
				if idx < len(tt.setErrors) {
					return tt.setErrors[idx]
				}
				return nil
			}

			// equalsFn: equal when the two values are identical strings.
			equalsFn := func(a, b simpleString) (bool, string) {
				if a == b {
					return true, ""
				}
				return false, a + " → " + b
			}

			err := rmw.Update(context.Background(), getFn, mutateFn, setFn, equalsFn, tt.maxRetries)

			switch {
			case tt.wantConflict:
				assert.True(t, rmw.IsConflictError(err), "expected ConflictError, got: %v", err)
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
			case tt.wantAnyErr:
				require.Error(t, err, "expected any non-nil error")
			default:
				require.NoError(t, err)
			}

			if tt.wantSetCalls > 0 {
				assert.Equal(t, tt.wantSetCalls, setCalls, "setFn call count")
			}
			if tt.wantGetCalls > 0 {
				assert.Equal(t, tt.wantGetCalls, getCalls, "getFn call count")
			}
		})
	}
}

// TestConflictError_ErrorFormat verifies the conflict-message format exactly.
func TestConflictError_ErrorFormat(t *testing.T) {
	tests := []struct {
		name    string
		ce      rmw.ConflictError
		wantMsg string
	}{
		{
			name: "namespace-level format",
			ce: rmw.ConflictError{
				Command:   "apps enable",
				Namespace: "default",
				Diff:      "tracing: true → false",
			},
			wantMsg: `apps enable: cannot write — namespace "default" was modified concurrently (tracing: true → false). Re-fetch and retry.`,
		},
		{
			name: "cluster-level format",
			ce: rmw.ConflictError{
				Command: "clusters enable",
				Diff:    "costmetrics: false → true",
			},
			wantMsg: `clusters enable: cannot write — cluster configuration was modified concurrently (costmetrics: false → true). Re-fetch and retry.`,
		},
		{
			name: "namespace-level with multi-field diff",
			ce: rmw.ConflictError{
				Command:   "apps enable",
				Namespace: "kube-system",
				Diff:      "tracing: true → false, logging: nil → true",
			},
			wantMsg: `apps enable: cannot write — namespace "kube-system" was modified concurrently (tracing: true → false, logging: nil → true). Re-fetch and retry.`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantMsg, tt.ce.Error())
		})
	}
}

// TestUpdate_ConcurrentModification verifies the full conflict scenario:
// two sequential getFn calls return different App values → Update returns ConflictError.
func TestUpdate_ConcurrentModification(t *testing.T) {
	// Simulate: operator 1 reads v1, then operator 2 changes the backend so
	// the re-check reads v2 — conflict detected, write must not proceed.
	callCount := 0
	getFn := func(_ context.Context) (simpleString, error) {
		callCount++
		if callCount == 1 {
			return "snapshot-v1", nil
		}
		return "snapshot-v2", nil // concurrent modification
	}

	mutateFn := func(s simpleString) simpleString { return s + "-enabled" }

	setCalled := false
	setFn := func(_ context.Context, _ simpleString) error {
		setCalled = true
		return nil
	}

	equalsFn := func(a, b simpleString) (bool, string) {
		if a == b {
			return true, ""
		}
		return false, "snapshot changed"
	}

	err := rmw.Update(context.Background(), getFn, mutateFn, setFn, equalsFn, 3)

	require.Error(t, err)
	assert.True(t, rmw.IsConflictError(err), "expected ConflictError")
	assert.False(t, setCalled, "setFn must not be called when conflict is detected")

	// Verify that augmenting with command/namespace produces the canonical conflict-error format.
	var ce rmw.ConflictError
	require.ErrorAs(t, err, &ce)
	ce.Command = "apps enable"
	ce.Namespace = "default"
	assert.Equal(t,
		`apps enable: cannot write — namespace "default" was modified concurrently (snapshot changed). Re-fetch and retry.`,
		ce.Error(),
	)
}

// TestIsConflictError verifies the helper function.
func TestIsConflictError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil is not a ConflictError",
			err:  nil,
			want: false,
		},
		{
			name: "ConflictError is detected",
			err:  rmw.ConflictError{Command: "apps enable", Diff: "x"},
			want: true,
		},
		{
			name: "wrapped ConflictError is detected",
			err:  fmt.Errorf("outer: %w", rmw.ConflictError{Diff: "x"}),
			want: true,
		},
		{
			name: "unrelated error is not a ConflictError",
			err:  errors.New("something else"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, rmw.IsConflictError(tt.err))
		})
	}
}
