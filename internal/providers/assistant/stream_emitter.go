package assistant

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
)

// This file is the single terminal-rendering layer for the A2A streaming
// commands (assistant prompt / assistant dashboard). The A2A protocol layer
// (internal/assistant) is untouched: it keeps producing the same
// assistant.StreamEvent payloads, and this emitter only decides how those
// events and the terminal outcome reach stdout/stderr per consumer mode.
//
// Contract per mode:
//   - modeHuman:      stderr prose progress, stdout prose response block —
//     byte-identical to the pre-emitter output.
//   - modeJSONStream: legacy --json NDJSON events, byte-identical shapes.
//   - modeJSONDoc:    legacy --json --no-stream single pretty JSON document.
//   - modeAgent:      typed, versioned JSONL — every stdout line is one JSON
//     value with a "type" discriminator, ending in a terminal
//     gcx.stream_end line that reports ok plus error details.
//
// On non-completed outcomes the machine modes return an EmittedError after
// the terminal output was written, so the process exits non-zero without the
// top-level reporter appending a second JSON document to stdout.

// Discriminators for the agent-mode stream envelope. The canonical values
// live in internal/output (shared with the other stream-class commands, e.g.
// instrumentation wait); these aliases keep the package's exported names.
const (
	// StreamEventType tags each streamed domain event line.
	StreamEventType = cmdio.StreamEventType
	// StreamEndType tags the terminal success/error line.
	StreamEndType = cmdio.StreamEndType

	streamSchemaVersion = cmdio.StreamSchemaVersion
)

// streamMode selects how prompt/dashboard render the A2A stream and its
// terminal outcome. Resolved exactly once in newStreamEmitter — explicit
// --json flags always win over agent-mode detection.
type streamMode int

const (
	modeHuman      streamMode = iota // default TTY: stderr progress + prose response block
	modeAgent                        // agent mode without explicit --json: typed JSONL
	modeJSONStream                   // --json: legacy NDJSON events
	modeJSONDoc                      // --json --no-stream: single pretty JSON document
)

// streamEmitter renders assistant stream events and the terminal outcome for
// one command invocation. It is the only place the four consumer modes
// branch; runPrompt never inspects agent mode itself.
type streamEmitter struct {
	mode streamMode
	w    io.Writer // stdout
	errW io.Writer // stderr

	// cancel aborts the underlying SSE stream on the first stdout write
	// failure (e.g. a broken pipe): once stdout is gone there is no consumer
	// left, so the stream loop must stop instead of draining events nobody
	// can read. Set by runPrompt; optional.
	cancel func()
	// writeErr records the first stream-event write failure. finish returns
	// it verbatim: an EmittedError may only be returned after the complete
	// result was written successfully (see gcxerrors.EmittedError), and a
	// broken stdout can carry no further terminal output.
	writeErr error
	// saveContextID persists the completed task's context ID for --continue.
	// Defaults to assistant.SaveLastContextID; a test seam.
	saveContextID func(string) error
}

// newStreamEmitter resolves the consumer mode from the explicit flags and
// (only when no explicit machine format was requested) agent-mode detection.
func newStreamEmitter(w, errW io.Writer, opts *promptOpts) *streamEmitter {
	mode := modeHuman
	switch {
	case opts.jsonOut && !opts.noStream:
		mode = modeJSONStream
	case opts.jsonOut:
		mode = modeJSONDoc
	case agent.IsAgentMode():
		mode = modeAgent
	}
	return &streamEmitter{mode: mode, w: w, errW: errW, saveContextID: assistant.SaveLastContextID}
}

// agentStreamEvent is one agent-mode JSONL line: the typed, versioned
// envelope around a domain stream event. The embedded StreamEvent carries
// the existing payload fields verbatim (taskId, contextId, state, text, ...);
// its own "type" tag is shadowed by the envelope discriminator, and the
// domain event kind moves to "event".
type agentStreamEvent struct {
	Type          string `json:"type"`
	SchemaVersion string `json:"schema_version"`
	Event         string `json:"event"`
	//nolint:embeddedstructfieldcheck // declared last so the envelope discriminators serialize first on every line
	assistant.StreamEvent
}

func newAgentStreamEvent(e assistant.StreamEvent) agentStreamEvent {
	return agentStreamEvent{
		Type:          StreamEventType,
		SchemaVersion: streamSchemaVersion,
		Event:         e.Type,
		StreamEvent:   e,
	}
}

// streamEndEvent is the terminal agent-mode JSONL line. OK reports the
// outcome; Error is present exactly when OK is false.
type streamEndEvent struct {
	Type          string          `json:"type"`
	SchemaVersion string          `json:"schema_version"`
	OK            bool            `json:"ok"`
	Error         *streamEndError `json:"error,omitempty"`
}

// streamEndError mirrors the fused-error vocabulary agents already parse
// from gcx.error envelopes (summary, exitCode), plus the domain reason.
type streamEndError struct {
	Reason   string `json:"reason"` // timeout | failed | canceled | unknown
	Summary  string `json:"summary"`
	ExitCode int    `json:"exitCode"`
}

// onEvent returns the per-event callback for StreamOptions.OnEvent, or nil
// when the mode does not stream events to stdout.
func (e *streamEmitter) onEvent() func(assistant.StreamEvent) {
	switch e.mode {
	case modeJSONStream:
		return func(ev assistant.StreamEvent) { e.writeEventLine(ev) }
	case modeAgent:
		return func(ev assistant.StreamEvent) { e.writeEventLine(newAgentStreamEvent(ev)) }
	default:
		return nil
	}
}

// writeEventLine writes one non-terminal stream-event line. The first write
// failure is recorded and aborts the stream via cancel — a broken pipe means
// the consumer is gone, so continuing the loop would only discard events —
// and every subsequent write is skipped so nothing more is attempted on the
// dead stream. finish surfaces the recorded error as the command error.
func (e *streamEmitter) writeEventLine(v any) {
	if e.writeErr != nil {
		return
	}
	if err := jsonLine(e.w, v); err != nil {
		e.writeErr = err
		if e.cancel != nil {
			e.cancel()
		}
	}
}

// notice surfaces the resumable-context notice as advisory stderr: prose for
// humans, a typed note record in agent mode. The legacy --json modes keep
// suppressing it, as they always have.
func (e *streamEmitter) notice(text string) {
	if text == "" {
		return
	}
	switch e.mode { //nolint:exhaustive // json modes intentionally silent
	case modeHuman:
		cmdio.Info(e.errW, "%s", text)
	case modeAgent:
		cmdio.EmitNote(e.errW, text)
	}
}

// approvalHandler returns the tool-approval handler for the mode: interactive
// prompting for humans, an explicit non-blocking auto-decline in agent mode,
// and nil (the SSE layer's silent auto-deny) for the legacy --json modes.
func (e *streamEmitter) approvalHandler(logger assistant.Logger) assistant.ApprovalHandler { //nolint:ireturn
	switch e.mode { //nolint:exhaustive
	case modeHuman:
		return &assistant.InteractiveApprovalHandler{Logger: logger}
	case modeAgent:
		return agentDenyApprovalHandler{errW: e.errW}
	default:
		return nil
	}
}

// agentDenyApprovalHandler declines every tool-approval request without
// touching stdin. Agent mode must never block on an interactive prompt, and
// an approval that would mutate must never be auto-approved silently — so the
// decline is explicit and a typed warning tells the agent how a human can
// approve. The stream itself still carries the "approval" event on stdout.
type agentDenyApprovalHandler struct {
	errW io.Writer
}

func (h agentDenyApprovalHandler) HandleApproval(req assistant.ApprovalRequest) bool {
	cmdio.EmitWarn(h.errW, fmt.Sprintf(
		"approval for tool %q auto-declined: gcx never auto-approves assistant tool actions in agent mode; run the command interactively (without agent mode) to approve",
		req.ToolName))
	return false
}

// finish renders the terminal outcome of the stream and returns the error
// that carries the process exit code. Machine modes that already wrote their
// terminal output return an EmittedError so nothing more lands on stdout.
// When any stdout write failed — a streamed event line or the terminal line
// itself — finish returns the write error instead: the EmittedError sentinel
// may only report a complete, successfully written result.
func (e *streamEmitter) finish(result assistant.StreamResult, timeoutSeconds int) error {
	// A completed task's context ID persists regardless of stdout health:
	// --continue must keep working after a broken pipe — the conversation
	// happened whether or not the consumer read the tail of the stream.
	if result.Completed && result.ContextID != "" && e.saveContextID != nil {
		_ = e.saveContextID(result.ContextID)
	}
	if e.writeErr != nil {
		// The event stream broke mid-flight (recorded by writeEventLine).
		// stdout is unusable, so no terminal line is attempted; the write
		// error is the honest outcome and carries the non-zero exit.
		return e.writeErr
	}
	switch {
	case result.Completed:
		return e.finishCompleted(result)
	case result.TimedOut:
		return e.finishTimedOut(result, timeoutSeconds)
	case result.Failed:
		return e.finishFailed(result)
	case result.Canceled:
		return e.finishCanceled(result)
	default:
		return e.finishUnknown(result)
	}
}

func (e *streamEmitter) finishCompleted(result assistant.StreamResult) error {
	switch e.mode {
	case modeJSONDoc:
		// A failed terminal write surfaces as the command error: the
		// consumer never received the document, so exit 0 would lie.
		return jsonPretty(e.w, promptResult{
			TaskID:    result.TaskID,
			ContextID: result.ContextID,
			Status:    "completed",
			Response:  result.Response,
		})
	case modeAgent:
		// Same contract for the terminal gcx.stream_end line.
		return e.end(nil)
	case modeHuman:
		cmdio.Success(e.errW, "Completed!")
		fmt.Fprintln(e.w)
		fmt.Fprintln(e.w, "--- Response ---")
		fmt.Fprintln(e.w)
		fmt.Fprintln(e.w, result.Response)
		fmt.Fprintln(e.w)
		fmt.Fprintln(e.w, "----------------")
	case modeJSONStream:
		// Events were already streamed; the final completed status event is
		// the terminal signal in the legacy NDJSON shape.
	}
	return nil
}

func (e *streamEmitter) finishTimedOut(result assistant.StreamResult, timeoutSeconds int) error {
	err := fmt.Errorf("request timed out after %ds", timeoutSeconds)
	switch e.mode {
	case modeJSONStream:
		return emittedAfter(jsonLine(e.w, assistant.StreamEvent{
			Type:    "error",
			Error:   err.Error(),
			Timeout: timeoutSeconds,
		}), err)
	case modeJSONDoc:
		return emittedAfter(jsonPretty(e.w, promptResult{
			TaskID:    result.TaskID,
			ContextID: result.ContextID,
			Status:    "timeout",
			Timeout:   timeoutSeconds,
		}), err)
	case modeAgent:
		return emittedAfter(e.end(&streamEndError{Reason: "timeout", Summary: err.Error(), ExitCode: gcxerrors.ExitGeneralError}), err)
	case modeHuman:
	}
	cmdio.Warning(e.errW, "Request timed out after %ds. Task may still be processing.", timeoutSeconds)
	if result.TaskID != "" {
		cmdio.Info(e.errW, "Task ID: %s", result.TaskID)
	}
	return err
}

func (e *streamEmitter) finishFailed(result assistant.StreamResult) error {
	err := fmt.Errorf("request failed: %s", result.ErrorMessage)
	switch e.mode {
	case modeJSONStream:
		if !result.ErrorEventEmitted {
			return emittedAfter(jsonLine(e.w, assistant.StreamEvent{
				Type:      "error",
				TaskID:    result.TaskID,
				ContextID: result.ContextID,
				Error:     result.ErrorMessage,
			}), err)
		}
		return emittedFailure(err)
	case modeJSONDoc:
		return emittedAfter(jsonPretty(e.w, promptResult{
			TaskID:    result.TaskID,
			ContextID: result.ContextID,
			Status:    "failed",
			Error:     result.ErrorMessage,
		}), err)
	case modeAgent:
		return emittedAfter(e.end(&streamEndError{Reason: "failed", Summary: err.Error(), ExitCode: gcxerrors.ExitGeneralError}), err)
	case modeHuman:
	}
	cmdio.Error(e.errW, "Request failed: %s", result.ErrorMessage)
	return err
}

func (e *streamEmitter) finishCanceled(result assistant.StreamResult) error {
	err := errors.New("request was canceled")
	switch e.mode {
	case modeJSONStream:
		// The canceled status event was already streamed by OnEvent; nothing
		// more belongs on stdout.
		return emittedFailure(err)
	case modeJSONDoc:
		return emittedAfter(jsonPretty(e.w, promptResult{
			TaskID:    result.TaskID,
			ContextID: result.ContextID,
			Status:    "canceled",
		}), err)
	case modeAgent:
		// Cancellation carries ExitCancelled, matching the repo-wide
		// convention (context cancellation, declined confirmation prompts) —
		// not the general error code. The legacy --json modes above keep
		// exit 1, the code they have always produced.
		if writeErr := e.end(&streamEndError{Reason: "canceled", Summary: err.Error(), ExitCode: gcxerrors.ExitCancelled}); writeErr != nil {
			return writeErr
		}
		return gcxerrors.NewEmittedError(gcxerrors.ExitCancelled, err)
	case modeHuman:
	}
	cmdio.Warning(e.errW, "Request was canceled")
	return err
}

func (e *streamEmitter) finishUnknown(result assistant.StreamResult) error {
	err := errors.New("request ended in unknown state")
	switch e.mode {
	case modeJSONStream:
		return emittedAfter(jsonLine(e.w, assistant.StreamEvent{Type: "error", Error: "stream ended unexpectedly"}), err)
	case modeJSONDoc:
		return emittedAfter(jsonPretty(e.w, promptResult{
			TaskID:    result.TaskID,
			ContextID: result.ContextID,
			Status:    "unknown",
		}), err)
	case modeAgent:
		return emittedAfter(e.end(&streamEndError{Reason: "unknown", Summary: "stream ended unexpectedly", ExitCode: gcxerrors.ExitGeneralError}), err)
	case modeHuman:
	}
	cmdio.Warning(e.errW, "Request ended unexpectedly. The stream closed without a completion signal.")
	if result.TaskID != "" {
		cmdio.Info(e.errW, "Task ID: %s", result.TaskID)
	}
	return err
}

// end writes the terminal gcx.stream_end line. endErr == nil means success.
// The write error is returned so a terminal line that never reached stdout
// can neither exit 0 nor be misreported as already emitted.
func (e *streamEmitter) end(endErr *streamEndError) error {
	return jsonLine(e.w, streamEndEvent{
		Type:          StreamEndType,
		SchemaVersion: streamSchemaVersion,
		OK:            endErr == nil,
		Error:         endErr,
	})
}

// emittedFailure wraps a terminal stream failure whose in-band output is
// already complete. The exit code stays ExitGeneralError — the same code
// these plain errors have always produced — but the top-level reporter now
// writes nothing more to stdout.
func emittedFailure(cause error) error {
	return gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError, cause)
}

// emittedAfter guards the EmittedError contract on failure finishes: the
// sentinel is returned only when the terminal write succeeded. A failed
// write returns the write error itself — the result never reached stdout, so
// claiming it was emitted would suppress the top-level error report.
func emittedAfter(writeErr, cause error) error {
	if writeErr != nil {
		return writeErr
	}
	return emittedFailure(cause)
}
