package output

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
)

// Agent-mode wait lines follow the shared stream envelope contract
// (docs/design/agent-mode.md §6.4, same vocabulary as the assistant A2A
// stream): every JSONL line carries `type` + `schema_version` discriminators.
// Progress-side lines (banner, per-poll status, poll errors) are tagged
// cmdio.StreamEventType on stderr; the fused terminal WaitResult is tagged
// cmdio.StreamEndType on stdout and is the exactly-once terminal event.
// The domain payload fields inside the envelope are unchanged.

// emitStreamLine marshals v as one NDJSON line.
func emitStreamLine(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("wait stream line: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// WaitBanner is the one-time start banner emitted to stderr before polling begins.
// In agent mode it is written as a typed NDJSON stream event; in non-agent mode
// it writes the human-readable "Waiting for …" message.
type WaitBanner struct {
	Event   string `json:"event"` // always "waiting_started"
	Target  Target `json:"target"`
	Timeout string `json:"timeout,omitempty"` // e.g. "5m0s"
}

// agentWaitBanner is the agent-mode envelope around WaitBanner.
type agentWaitBanner struct {
	Type          string `json:"type"`
	SchemaVersion string `json:"schema_version"`
	//nolint:embeddedstructfieldcheck // declared last so the envelope discriminators serialize first on every line
	WaitBanner
}

// EmitTo writes the WaitBanner to w. agentMode true → typed NDJSON line; false → human text.
func (b WaitBanner) EmitTo(w io.Writer, agentMode bool) error {
	if agentMode {
		return emitStreamLine(w, agentWaitBanner{
			Type:          cmdio.StreamEventType,
			SchemaVersion: cmdio.StreamSchemaVersion,
			WaitBanner:    b,
		})
	}
	if b.Target.Namespace != "" {
		_, err := fmt.Fprintf(w, "Waiting for namespace %q in cluster %q (timeout: %s)...\n",
			b.Target.Namespace, b.Target.Cluster, b.Timeout)
		return err
	}
	_, err := fmt.Fprintf(w, "Waiting for cluster %q to reach INSTRUMENTED status (timeout: %s)...\n",
		b.Target.Cluster, b.Timeout)
	return err
}

// WaitProgress is the per-poll progress event emitted to stderr during wait
// commands. In agent mode it is written as a typed NDJSON stream event; in
// non-agent mode it writes the existing human-readable progress text.
type WaitProgress struct {
	Event     string `json:"event"` // always "waiting"
	Target    Target `json:"target"`
	Status    string `json:"status"`
	ElapsedMs int64  `json:"elapsed_ms,omitempty"`
}

// agentWaitProgress is the agent-mode envelope around WaitProgress.
type agentWaitProgress struct {
	Type          string `json:"type"`
	SchemaVersion string `json:"schema_version"`
	//nolint:embeddedstructfieldcheck // declared last so the envelope discriminators serialize first on every line
	WaitProgress
}

// EmitTo writes the WaitProgress to w. agentMode true → typed NDJSON line on
// stderr; false → human-readable progress text.
func (p WaitProgress) EmitTo(w io.Writer, agentMode bool) error {
	if agentMode {
		return emitStreamLine(w, agentWaitProgress{
			Type:          cmdio.StreamEventType,
			SchemaVersion: cmdio.StreamSchemaVersion,
			WaitProgress:  p,
		})
	}
	if p.Target.Namespace != "" {
		_, err := fmt.Fprintf(w, "waiting: namespace %q status is %s...\n", p.Target.Namespace, p.Status)
		return err
	}
	_, err := fmt.Fprintf(w, "  status: %s\n", p.Status)
	return err
}

// WaitPollError is the per-poll transient error event emitted to stderr when a
// poll RPC fails and the wait loop retries. In agent mode it is a typed NDJSON
// stream event so the stderr stream stays line-parseable; in non-agent mode it
// writes the existing human-readable retry text.
type WaitPollError struct {
	Event  string `json:"event"` // always "poll_error"
	Target Target `json:"target"`
	Error  string `json:"error"`
}

// agentWaitPollError is the agent-mode envelope around WaitPollError.
type agentWaitPollError struct {
	Type          string `json:"type"`
	SchemaVersion string `json:"schema_version"`
	//nolint:embeddedstructfieldcheck // declared last so the envelope discriminators serialize first on every line
	WaitPollError
}

// EmitTo writes the WaitPollError to w. agentMode true → typed NDJSON line;
// false → the human retry text.
func (p WaitPollError) EmitTo(w io.Writer, agentMode bool) error {
	if agentMode {
		return emitStreamLine(w, agentWaitPollError{
			Type:          cmdio.StreamEventType,
			SchemaVersion: cmdio.StreamSchemaVersion,
			WaitPollError: p,
		})
	}
	_, err := fmt.Fprintf(w, "  poll error (retrying): %s\n", p.Error)
	return err
}

// WaitError carries error details inside the fused terminal envelope.
// Populated when Outcome is "timeout", "error", or "canceled".
type WaitError struct {
	Summary  string `json:"summary"`
	Details  string `json:"details"`
	ExitCode int    `json:"exitCode"`
}

// WaitResult is the terminal envelope emitted by wait commands. In agent mode
// it is written as the typed gcx.stream_end JSONL line — the exactly-once
// terminal event of the wait stream — with Error populated for fused
// timeout/error/canceled outcomes. In non-agent mode, a one-line human summary
// is written instead (only for the success/timeout outcomes the human flow has
// always printed).
type WaitResult struct {
	Outcome   string     `json:"outcome"` // "success", "timeout", "error", or "canceled"
	Target    Target     `json:"target"`
	Status    string     `json:"status,omitempty"` // last observed proto enum value
	ElapsedMs int64      `json:"elapsed_ms,omitempty"`
	Error     *WaitError `json:"error,omitempty"` // populated on timeout/error/canceled
}

// agentWaitResult is the agent-mode terminal envelope around WaitResult.
type agentWaitResult struct {
	Type          string `json:"type"`
	SchemaVersion string `json:"schema_version"`
	//nolint:embeddedstructfieldcheck // declared last so the envelope discriminators serialize first on every line
	WaitResult
}

// Emit writes the WaitResult to w. agentMode true → typed gcx.stream_end
// JSONL line; false → one-line human summary.
func (r WaitResult) Emit(w io.Writer, agentMode bool) error {
	if agentMode {
		return emitStreamLine(w, agentWaitResult{
			Type:          cmdio.StreamEndType,
			SchemaVersion: cmdio.StreamSchemaVersion,
			WaitResult:    r,
		})
	}
	switch r.Outcome {
	case "success":
		if r.Target.Namespace != "" {
			_, err := fmt.Fprintf(w, "namespace %q in cluster %q: status is %s\n", r.Target.Namespace, r.Target.Cluster, r.Status)
			return err
		}
		_, err := fmt.Fprintf(w, "Cluster %q reached terminal state %s.\n", r.Target.Cluster, r.Status)
		return err
	case "timeout":
		if r.Target.Namespace != "" {
			_, err := fmt.Fprintf(w, "timeout: namespace %q in cluster %q still in %s\n", r.Target.Namespace, r.Target.Cluster, r.Status)
			return err
		}
		_, err := fmt.Fprintf(w, "timeout: %s still in %s\n", r.Target.Cluster, r.Status)
		return err
	default:
		_, err := fmt.Fprintf(w, "error at %s\n", r.Target.Cluster)
		return err
	}
}

// WaitResultForCluster builds a WaitResult for cluster-level waits (no namespace).
func WaitResultForCluster(outcome, clusterName, status string, start time.Time) WaitResult {
	return WaitResult{
		Outcome:   outcome,
		Target:    Target{Cluster: clusterName},
		Status:    status,
		ElapsedMs: time.Since(start).Milliseconds(),
	}
}

// WaitTerminal fuses the terminal failure outcomes (timeout, error status,
// cancellation) shared by the cluster-level (`clusters wait`) and
// namespace-level (`clusters apps wait`) wait commands. It is parameterized by
// target and error prefix so both commands emit their exact wire and human
// lines through one implementation.
type WaitTerminal struct {
	// Target identifies what is being waited on (cluster, or cluster+namespace).
	Target Target
	// ErrPrefix is the command prefix wrapped around returned errors,
	// e.g. "clusters wait" or "apps wait".
	ErrPrefix string
	// Start anchors ElapsedMs in the emitted terminal envelope.
	Start time.Time
	// Stdout receives the terminal WaitResult line.
	Stdout io.Writer
	// AgentMode selects the typed gcx.stream_end JSONL line over human text
	// for the timeout outcome. The error/canceled finishers are agent-mode
	// only and always emit the typed line.
	AgentMode bool
}

// result builds the terminal WaitResult envelope for the given outcome.
func (t WaitTerminal) result(outcome, status string, waitErr *WaitError) WaitResult {
	return WaitResult{
		Outcome:   outcome,
		Target:    t.Target,
		Status:    status,
		ElapsedMs: time.Since(t.Start).Milliseconds(),
		Error:     waitErr,
	}
}

// FinishTimeout emits the fused terminal timeout WaitResult. Only when that
// write lands intact may the ErrWaitTimeoutEmitted sentinel be returned (it
// suppresses the secondary error envelope); if the write itself failed the
// result was NOT emitted, so the write error must surface instead.
func (t WaitTerminal) FinishTimeout(status, summary, details string) error {
	result := t.result("timeout", status, &WaitError{
		Summary:  summary,
		Details:  details,
		ExitCode: 1,
	})
	if emitErr := result.Emit(t.Stdout, t.AgentMode); emitErr != nil {
		return fmt.Errorf("%s: emit timeout result: %w", t.ErrPrefix, emitErr)
	}
	return fmt.Errorf("%s: %w", t.ErrPrefix, instrumentation.ErrWaitTimeoutEmitted)
}

// FinishErrorStatus (agent mode only) writes the terminal error stream_end
// line for a target that reached an error status, then returns an
// EmittedError carrying the general error exit code so the reporter appends
// nothing more to stdout. On write failure the write error surfaces instead
// of the sentinel.
func (t WaitTerminal) FinishErrorStatus(cause error, status, summary, details string) error {
	result := t.result("error", status, &WaitError{
		Summary:  summary,
		Details:  details,
		ExitCode: gcxerrors.ExitGeneralError,
	})
	if emitErr := result.Emit(t.Stdout, true); emitErr != nil {
		return fmt.Errorf("%s: emit error result: %w", t.ErrPrefix, emitErr)
	}
	return fmt.Errorf("%s: %w", t.ErrPrefix, gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError, cause))
}

// FinishCanceled (agent mode only) writes the terminal canceled stream_end
// line, then returns an EmittedError carrying the same exit code the
// human-mode cancellation path resolves to (ExitCancelled for
// context.Canceled, general error otherwise). On write failure the write
// error surfaces instead of the sentinel.
func (t WaitTerminal) FinishCanceled(cause error, status, summary string) error {
	code := gcxerrors.ExitGeneralError
	if errors.Is(cause, context.Canceled) {
		code = gcxerrors.ExitCancelled
	}
	result := t.result("canceled", status, &WaitError{
		Summary:  summary,
		Details:  cause.Error(),
		ExitCode: code,
	})
	if emitErr := result.Emit(t.Stdout, true); emitErr != nil {
		return fmt.Errorf("%s: emit canceled result: %w", t.ErrPrefix, emitErr)
	}
	return fmt.Errorf("%s: %w", t.ErrPrefix, gcxerrors.NewEmittedError(code, cause))
}
