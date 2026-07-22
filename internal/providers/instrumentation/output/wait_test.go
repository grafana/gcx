package output_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the exact wire shapes of the wait stream lines:
//   - agent mode: typed, versioned JSONL — discriminators first, then the
//     domain payload fields verbatim (agent-mode.md §6.4 stream contract);
//   - human mode: byte-identical to the pre-envelope prose output.

func TestWaitBanner_EmitTo(t *testing.T) {
	banner := output.WaitBanner{
		Event:   "waiting_started",
		Target:  output.Target{Cluster: "prod-eu"},
		Timeout: "5m0s",
	}

	var agentBuf bytes.Buffer
	require.NoError(t, banner.EmitTo(&agentBuf, true))
	// Byte-exact pin: discriminators first, then the payload fields verbatim.
	if want := `{"type":"gcx.stream_event","schema_version":"1","event":"waiting_started","target":{"cluster":"prod-eu"},"timeout":"5m0s"}` + "\n"; agentBuf.String() != want {
		t.Errorf("agent banner line:\n got %q\nwant %q", agentBuf.String(), want)
	}

	var humanBuf bytes.Buffer
	require.NoError(t, banner.EmitTo(&humanBuf, false))
	assert.Equal(t, "Waiting for cluster \"prod-eu\" to reach INSTRUMENTED status (timeout: 5m0s)...\n", humanBuf.String())

	nsBanner := output.WaitBanner{
		Event:   "waiting_started",
		Target:  output.Target{Cluster: "prod-eu", Namespace: "grotshop"},
		Timeout: "5m0s",
	}
	var nsHumanBuf bytes.Buffer
	require.NoError(t, nsBanner.EmitTo(&nsHumanBuf, false))
	assert.Equal(t, "Waiting for namespace \"grotshop\" in cluster \"prod-eu\" (timeout: 5m0s)...\n", nsHumanBuf.String())
}

func TestWaitProgress_EmitTo(t *testing.T) {
	progress := output.WaitProgress{
		Event:     "waiting",
		Target:    output.Target{Cluster: "prod-eu"},
		Status:    "K8S_MONITORING_STATUS_PENDING_INSTRUMENTATION",
		ElapsedMs: 1500,
	}

	var agentBuf bytes.Buffer
	require.NoError(t, progress.EmitTo(&agentBuf, true))
	// Byte-exact pin: discriminators first, then the payload fields verbatim.
	if want := `{"type":"gcx.stream_event","schema_version":"1","event":"waiting","target":{"cluster":"prod-eu"},"status":"K8S_MONITORING_STATUS_PENDING_INSTRUMENTATION","elapsed_ms":1500}` + "\n"; agentBuf.String() != want {
		t.Errorf("agent progress line:\n got %q\nwant %q", agentBuf.String(), want)
	}

	var humanBuf bytes.Buffer
	require.NoError(t, progress.EmitTo(&humanBuf, false))
	assert.Equal(t, "  status: K8S_MONITORING_STATUS_PENDING_INSTRUMENTATION\n", humanBuf.String())

	nsProgress := output.WaitProgress{
		Event:  "waiting",
		Target: output.Target{Cluster: "prod-eu", Namespace: "grotshop"},
		Status: "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION",
	}
	var nsHumanBuf bytes.Buffer
	require.NoError(t, nsProgress.EmitTo(&nsHumanBuf, false))
	assert.Equal(t, "waiting: namespace \"grotshop\" status is INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION...\n", nsHumanBuf.String())
}

func TestWaitPollError_EmitTo(t *testing.T) {
	pollErr := output.WaitPollError{
		Event:  "poll_error",
		Target: output.Target{Cluster: "prod-eu"},
		Error:  "transient RPC failure",
	}

	var agentBuf bytes.Buffer
	require.NoError(t, pollErr.EmitTo(&agentBuf, true))
	// Byte-exact pin: discriminators first, then the payload fields verbatim.
	if want := `{"type":"gcx.stream_event","schema_version":"1","event":"poll_error","target":{"cluster":"prod-eu"},"error":"transient RPC failure"}` + "\n"; agentBuf.String() != want {
		t.Errorf("agent poll_error line:\n got %q\nwant %q", agentBuf.String(), want)
	}

	var humanBuf bytes.Buffer
	require.NoError(t, pollErr.EmitTo(&humanBuf, false))
	assert.Equal(t, "  poll error (retrying): transient RPC failure\n", humanBuf.String())
}

func TestWaitResult_Emit(t *testing.T) {
	success := output.WaitResult{
		Outcome:   "success",
		Target:    output.Target{Cluster: "prod-eu"},
		Status:    "K8S_MONITORING_STATUS_INSTRUMENTED",
		ElapsedMs: 2000,
	}

	var agentBuf bytes.Buffer
	require.NoError(t, success.Emit(&agentBuf, true))
	// Byte-exact pin: discriminators first, then the payload fields verbatim.
	if want := `{"type":"gcx.stream_end","schema_version":"1","outcome":"success","target":{"cluster":"prod-eu"},"status":"K8S_MONITORING_STATUS_INSTRUMENTED","elapsed_ms":2000}` + "\n"; agentBuf.String() != want {
		t.Errorf("agent success terminal line:\n got %q\nwant %q", agentBuf.String(), want)
	}

	var humanBuf bytes.Buffer
	require.NoError(t, success.Emit(&humanBuf, false))
	assert.Equal(t, "Cluster \"prod-eu\" reached terminal state K8S_MONITORING_STATUS_INSTRUMENTED.\n", humanBuf.String())

	timeout := output.WaitResult{
		Outcome:   "timeout",
		Target:    output.Target{Cluster: "prod-eu"},
		Status:    "K8S_MONITORING_STATUS_PENDING_INSTRUMENTATION",
		ElapsedMs: 300000,
		Error: &output.WaitError{
			Summary:  "timeout waiting for cluster \"prod-eu\"",
			Details:  "timeout after 5m0s",
			ExitCode: 1,
		},
	}

	var timeoutAgentBuf bytes.Buffer
	require.NoError(t, timeout.Emit(&timeoutAgentBuf, true))
	// Byte-exact pin: discriminators first, then the payload fields verbatim.
	if want := `{"type":"gcx.stream_end","schema_version":"1","outcome":"timeout","target":{"cluster":"prod-eu"},"status":"K8S_MONITORING_STATUS_PENDING_INSTRUMENTATION","elapsed_ms":300000,"error":{"summary":"timeout waiting for cluster \"prod-eu\"","details":"timeout after 5m0s","exitCode":1}}` + "\n"; timeoutAgentBuf.String() != want {
		t.Errorf("agent timeout terminal line:\n got %q\nwant %q", timeoutAgentBuf.String(), want)
	}

	var timeoutHumanBuf bytes.Buffer
	require.NoError(t, timeout.Emit(&timeoutHumanBuf, false))
	assert.Equal(t, "timeout: prod-eu still in K8S_MONITORING_STATUS_PENDING_INSTRUMENTATION\n", timeoutHumanBuf.String())
}

func TestWaitResult_EmitWriteFailurePropagates(t *testing.T) {
	writeErr := errors.New("no space left on device")
	result := output.WaitResultForCluster("timeout", "prod-eu", "PENDING", time.Now())

	err := result.Emit(failWriter{err: writeErr}, true)
	require.ErrorIs(t, err, writeErr, "agent-mode terminal write failure must propagate")

	err = result.Emit(failWriter{err: writeErr}, false)
	require.ErrorIs(t, err, writeErr, "human-mode terminal write failure must propagate")
}

// failWriter fails every write with err.
type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }

// terminalFor builds a WaitTerminal writing to w with the given agent mode.
func terminalFor(w io.Writer, agentMode bool) output.WaitTerminal {
	return output.WaitTerminal{
		Target:    output.Target{Cluster: "prod-eu"},
		ErrPrefix: "clusters wait",
		Start:     time.Now(),
		Stdout:    w,
		AgentMode: agentMode,
	}
}

func TestWaitTerminal_FinishTimeout(t *testing.T) {
	var buf bytes.Buffer
	err := terminalFor(&buf, true).FinishTimeout("PENDING", "timeout waiting for cluster \"prod-eu\"", "timeout after 5m0s")

	require.ErrorIs(t, err, instrumentation.ErrWaitTimeoutEmitted,
		"timeout must return the ErrWaitTimeoutEmitted sentinel once the write landed")
	assert.Contains(t, err.Error(), "clusters wait:", "returned error must carry the command prefix")

	line := buf.String()
	assert.Contains(t, line, `"type":"gcx.stream_end"`)
	assert.Contains(t, line, `"outcome":"timeout"`)
	assert.Contains(t, line, `"exitCode":1`)

	// Human mode keeps the plain timeout summary line.
	var humanBuf bytes.Buffer
	err = terminalFor(&humanBuf, false).FinishTimeout("PENDING", "summary", "details")
	require.ErrorIs(t, err, instrumentation.ErrWaitTimeoutEmitted)
	assert.Equal(t, "timeout: prod-eu still in PENDING\n", humanBuf.String())

	// Write failure: the sentinel must NOT be returned — the write error surfaces.
	writeErr := errors.New("no space left on device")
	err = terminalFor(failWriter{err: writeErr}, true).FinishTimeout("PENDING", "summary", "details")
	require.NotErrorIs(t, err, instrumentation.ErrWaitTimeoutEmitted)
	require.ErrorIs(t, err, writeErr)
}

func TestWaitTerminal_FinishErrorStatus(t *testing.T) {
	cause := errors.New("cluster reached INSTRUMENTATION_ERROR status")

	var buf bytes.Buffer
	err := terminalFor(&buf, true).FinishErrorStatus(cause, "K8S_MONITORING_STATUS_ERROR", cause.Error(), "details")

	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted, "error status must return an EmittedError after the terminal write")
	assert.Equal(t, gcxerrors.ExitGeneralError, emitted.Code)
	require.ErrorIs(t, err, cause, "the original cause must stay in the chain")

	line := buf.String()
	assert.Contains(t, line, `"type":"gcx.stream_end"`)
	assert.Contains(t, line, `"outcome":"error"`)
	assert.Contains(t, line, `"status":"K8S_MONITORING_STATUS_ERROR"`)

	// Write failure surfaces instead of the sentinel.
	writeErr := errors.New("no space left on device")
	err = terminalFor(failWriter{err: writeErr}, true).FinishErrorStatus(cause, "S", "summary", "details")
	require.NotErrorAs(t, err, &emitted)
	require.ErrorIs(t, err, writeErr)
}

func TestWaitTerminal_FinishCanceled(t *testing.T) {
	// context.Canceled resolves to the cancellation exit code.
	var buf bytes.Buffer
	err := terminalFor(&buf, true).FinishCanceled(context.Canceled, "PENDING", "wait canceled before reaching a terminal status")

	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	assert.Equal(t, gcxerrors.ExitCancelled, emitted.Code)
	assert.Contains(t, buf.String(), `"outcome":"canceled"`)

	// Any other cause resolves to the general error exit code.
	var otherBuf bytes.Buffer
	err = terminalFor(&otherBuf, true).FinishCanceled(errors.New("boom"), "PENDING", "summary")
	require.ErrorAs(t, err, &emitted)
	assert.Equal(t, gcxerrors.ExitGeneralError, emitted.Code)

	// Write failure surfaces instead of the sentinel.
	writeErr := errors.New("no space left on device")
	err = terminalFor(failWriter{err: writeErr}, true).FinishCanceled(context.Canceled, "PENDING", "summary")
	require.NotErrorAs(t, err, &emitted)
	require.ErrorIs(t, err, writeErr)
}
