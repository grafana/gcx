//nolint:testpackage // white-box testing: accesses unexported run* functions and types.
package clusters

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDeclaredClient implements declaredStateClient for wait tests.
type fakeDeclaredClient struct {
	Resp *instrumentation.GetK8SInstrumentationResponse
	Err  error
}

func (f *fakeDeclaredClient) GetK8SInstrumentation(_ context.Context, _ string) (*instrumentation.GetK8SInstrumentationResponse, error) {
	return f.Resp, f.Err
}

// declaredCluster returns a fakeDeclaredClient that reports the cluster as declared.
func declaredCluster(name string) *fakeDeclaredClient {
	return &fakeDeclaredClient{
		Resp: &instrumentation.GetK8SInstrumentationResponse{
			Cluster: instrumentation.Cluster{Name: name},
		},
	}
}

// undeclaredCluster returns a fakeDeclaredClient that reports the cluster as not declared
// (empty Cluster.Name — the sentinel value for "never configured").
func undeclaredCluster() *fakeDeclaredClient {
	return &fakeDeclaredClient{
		Resp: &instrumentation.GetK8SInstrumentationResponse{
			Cluster: instrumentation.Cluster{Name: ""},
		},
	}
}

func TestRunWait(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		// declaredClient controls the pre-flight declared-state check.
		declaredClient *fakeDeclaredClient
		// monSequence is the sequence of statuses to return on each poll.
		// The last entry is repeated for all subsequent calls.
		monSequence []instrumentation.InstrumentationStatus
		monErr      error
		timeout     time.Duration
		wantErr     bool
		wantErrMsgs []string
	}{
		{
			name:           "cluster is already INSTRUMENTED on first poll",
			clusterName:    "prod-eu",
			declaredClient: declaredCluster("prod-eu"),
			monSequence: []instrumentation.InstrumentationStatus{
				instrumentation.StatusInstrumented,
			},
			timeout: 30 * time.Second,
		},
		{
			name:           "cluster transitions PENDING → INSTRUMENTED",
			clusterName:    "prod-eu",
			declaredClient: declaredCluster("prod-eu"),
			monSequence: []instrumentation.InstrumentationStatus{
				instrumentation.StatusPendingInstrumentation,
				instrumentation.StatusPendingInstrumentation,
				instrumentation.StatusInstrumented,
			},
			timeout: 30 * time.Second,
		},
		{
			name:           "INSTRUMENTATION_ERROR exits non-zero immediately",
			clusterName:    "prod-eu",
			declaredClient: declaredCluster("prod-eu"),
			monSequence: []instrumentation.InstrumentationStatus{
				instrumentation.StatusError,
			},
			timeout:     30 * time.Second,
			wantErr:     true,
			wantErrMsgs: []string{"INSTRUMENTATION_ERROR"},
		},
		{
			name:           "undeclared cluster → fail-fast with declared error",
			clusterName:    "new-cluster",
			declaredClient: undeclaredCluster(),
			timeout:        30 * time.Second,
			wantErr:        true,
			wantErrMsgs:    []string{"cluster is not declared"},
		},
		{
			name:        "GetK8SInstrumentation error propagates",
			clusterName: "prod-eu",
			declaredClient: &fakeDeclaredClient{
				Err: assert.AnError,
			},
			timeout: 30 * time.Second,
			wantErr: true,
		},
		{
			name:           "timeout returns ErrWaitTimeoutEmitted sentinel",
			clusterName:    "prod-eu",
			declaredClient: declaredCluster("prod-eu"),
			monSequence: []instrumentation.InstrumentationStatus{
				instrumentation.StatusPendingInstrumentation,
			},
			timeout:     1 * time.Millisecond,
			wantErr:     true,
			wantErrMsgs: []string{instrumentation.ErrWaitTimeoutEmitted.Error()},
		},
		{
			name:           "NOT_INSTRUMENTED continues polling (backend may not have observed yet)",
			clusterName:    "fresh-cluster",
			declaredClient: declaredCluster("fresh-cluster"),
			// NOT_INSTRUMENTED is a pending state per spec: backend may not have observed
			// the cluster yet. Continue polling; timeout if no transition to terminal state.
			monSequence: []instrumentation.InstrumentationStatus{
				instrumentation.StatusNotInstrumented,
			},
			timeout:     1 * time.Millisecond, // timeout quickly since we won't reach terminal state
			wantErr:     true,
			wantErrMsgs: []string{instrumentation.ErrWaitTimeoutEmitted.Error()},
		},
		{
			name:           "full proto enum K8S_MONITORING_STATUS_INSTRUMENTED exits with success",
			clusterName:    "prod-eu",
			declaredClient: declaredCluster("prod-eu"),
			// Wire returns full proto enum names, not shorthand constants.
			// Classifier must match "K8S_MONITORING_STATUS_INSTRUMENTED".
			monSequence: []instrumentation.InstrumentationStatus{
				instrumentation.InstrumentationStatus("K8S_MONITORING_STATUS_INSTRUMENTED"),
			},
			timeout: 30 * time.Second,
			wantErr: false,
		},
		{
			name:           "full proto enum K8S_MONITORING_STATUS_ERROR exits non-zero",
			clusterName:    "prod-eu",
			declaredClient: declaredCluster("prod-eu"),
			// Wire returns full proto enum name for error state.
			monSequence: []instrumentation.InstrumentationStatus{
				instrumentation.InstrumentationStatus("K8S_MONITORING_STATUS_ERROR"),
			},
			timeout:     30 * time.Second,
			wantErr:     true,
			wantErrMsgs: []string{"INSTRUMENTATION_ERROR"},
		},
		{
			name:           "full proto enum K8S_MONITORING_STATUS_PENDING_INSTRUMENTATION continues polling",
			clusterName:    "prod-eu",
			declaredClient: declaredCluster("prod-eu"),
			// Wire returns full proto enum name for pending state.
			// Classifier returns WaitPending → continue polling.
			monSequence: []instrumentation.InstrumentationStatus{
				instrumentation.InstrumentationStatus("K8S_MONITORING_STATUS_PENDING_INSTRUMENTATION"),
			},
			timeout:     1 * time.Millisecond, // timeout quickly since we won't reach terminal state
			wantErr:     true,
			wantErrMsgs: []string{instrumentation.ErrWaitTimeoutEmitted.Error()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pollCount := 0
			monSeq := tt.monSequence
			clusterName := tt.clusterName
			monErr := tt.monErr
			monClient := &fakeMonitoringClient{
				RunK8sMonitoringFn: func(_ context.Context) ([]instrumentation.ClusterObservedState, error) {
					if monErr != nil {
						return nil, monErr
					}
					var status instrumentation.InstrumentationStatus
					if pollCount < len(monSeq) {
						status = monSeq[pollCount]
					} else if len(monSeq) > 0 {
						status = monSeq[len(monSeq)-1]
					}
					pollCount++
					return []instrumentation.ClusterObservedState{
						{Name: clusterName, InstrumentationStatus: status},
					}, nil
				},
			}

			opts := &waitOpts{
				Timeout:      tt.timeout,
				pollInterval: 1 * time.Millisecond, // fast polling for tests
			}

			var stdout, stderr bytes.Buffer
			err := runWait(context.Background(), opts, tt.declaredClient, monClient, tt.clusterName, &stdout, &stderr)

			if tt.wantErr {
				require.Error(t, err)
				for _, sub := range tt.wantErrMsgs {
					assert.Contains(t, err.Error(), sub,
						"expected %q in error message, got: %v", sub, err)
				}
				return
			}
			require.NoError(t, err)
			// Success: stdout should have the terminal state message; stderr should have
			// the banner and progress updates (stream split).
			assert.Contains(t, stdout.String(), "INSTRUMENTED",
				"success output on stdout should mention INSTRUMENTED")
			assert.Contains(t, stderr.String(), "Waiting for cluster",
				"stderr should have the wait banner")
		})
	}
}

func TestRunWait_ProgressOnStderr(t *testing.T) {
	// Verifies that all progress (banner + per-poll status) routes to stderr;
	// stdout contains only the final WaitResult.
	client := declaredCluster("prod-eu")
	monClient := &fakeMonitoringClient{
		RunK8sMonitoringFn: func(_ context.Context) ([]instrumentation.ClusterObservedState, error) {
			return []instrumentation.ClusterObservedState{
				{Name: "prod-eu", InstrumentationStatus: instrumentation.StatusInstrumented},
			}, nil
		},
	}
	opts := &waitOpts{
		Timeout:      30 * time.Second,
		pollInterval: 1 * time.Millisecond,
		agentMode:    false,
	}

	var stdout, stderr bytes.Buffer
	err := runWait(context.Background(), opts, client, monClient, "prod-eu", &stdout, &stderr)
	require.NoError(t, err)

	// stdout: final success message only (non-agent mode uses plain text).
	assert.Contains(t, stdout.String(), "prod-eu", "stdout should mention cluster")
	assert.Contains(t, stdout.String(), "INSTRUMENTED", "stdout should mention status")
	// No progress text should bleed into stdout.
	assert.NotContains(t, stdout.String(), "Waiting for cluster", "banner must not appear on stdout")
	assert.NotContains(t, stdout.String(), "status:", "per-poll status must not appear on stdout")

	// stderr: banner + per-poll progress.
	assert.Contains(t, stderr.String(), "Waiting for cluster", "banner must be on stderr")
	assert.Contains(t, stderr.String(), "status:", "per-poll status must be on stderr")
}

func TestRunWait_AgentModeProgressNDJSON(t *testing.T) {
	// Verifies agent mode: per-poll progress events are NDJSON lines on stderr.
	client := declaredCluster("prod-eu")
	pollCall := 0
	monClient := &fakeMonitoringClient{
		RunK8sMonitoringFn: func(_ context.Context) ([]instrumentation.ClusterObservedState, error) {
			pollCall++
			// First poll: pending; second poll: instrumented.
			status := instrumentation.StatusPendingInstrumentation
			if pollCall >= 2 {
				status = instrumentation.StatusInstrumented
			}
			return []instrumentation.ClusterObservedState{
				{Name: "prod-eu", InstrumentationStatus: status},
			}, nil
		},
	}
	opts := &waitOpts{
		Timeout:      30 * time.Second,
		pollInterval: 1 * time.Millisecond,
		agentMode:    true,
	}

	var stdout, stderr bytes.Buffer
	err := runWait(context.Background(), opts, client, monClient, "prod-eu", &stdout, &stderr)
	require.NoError(t, err)

	// stderr: NDJSON progress events.
	stderrStr := stderr.String()
	assert.Contains(t, stderrStr, `"event":"waiting"`, "progress NDJSON should have event:waiting")
	assert.Contains(t, stderrStr, `"cluster":"prod-eu"`, "progress NDJSON should have cluster name")

	// stdout: final success JSON envelope.
	stdoutStr := stdout.String()
	assert.Contains(t, stdoutStr, `"outcome":"success"`, "stdout should have outcome:success")
	assert.Contains(t, stdoutStr, `"status":"INSTRUMENTED"`, "stdout should have status")
}

func TestRunWait_TimeoutEmitsFusedEnvelope(t *testing.T) {
	// Verifies that timeout emits fused WaitResult with Error field to stdout
	// and returns ErrWaitTimeoutEmitted (not a plain timeout string).
	client := declaredCluster("prod-eu")
	monClient := &fakeMonitoringClient{
		RunK8sMonitoringFn: func(_ context.Context) ([]instrumentation.ClusterObservedState, error) {
			return []instrumentation.ClusterObservedState{
				{Name: "prod-eu", InstrumentationStatus: instrumentation.StatusPendingInstrumentation},
			}, nil
		},
	}
	opts := &waitOpts{
		Timeout:      1 * time.Millisecond,
		pollInterval: 1 * time.Millisecond,
		agentMode:    true,
	}

	var stdout, stderr bytes.Buffer
	err := runWait(context.Background(), opts, client, monClient, "prod-eu", &stdout, &stderr)

	// Must return the sentinel error.
	require.ErrorIs(t, err, instrumentation.ErrWaitTimeoutEmitted,
		"timeout must return ErrWaitTimeoutEmitted sentinel")

	// stdout must have exactly one JSON doc with outcome:timeout and error field.
	stdoutStr := stdout.String()
	assert.Contains(t, stdoutStr, `"outcome":"timeout"`, "stdout should have outcome:timeout")
	assert.Contains(t, stdoutStr, `"error"`, "stdout should have error field")
}

func TestWaitOpts_Validate(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		wantErr bool
	}{
		{name: "positive timeout is valid", timeout: 5 * time.Minute},
		{name: "zero timeout is invalid", timeout: 0, wantErr: true},
		{name: "negative timeout is invalid", timeout: -1 * time.Second, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &waitOpts{Timeout: tt.timeout}
			err := opts.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
