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

// Discriminators for the agent-mode stream envelope.
const (
	// StreamEventType tags each streamed domain event line.
	StreamEventType = "gcx.stream_event"
	// StreamEndType tags the terminal success/error line.
	StreamEndType = "gcx.stream_end"

	streamSchemaVersion = "1"
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
	return &streamEmitter{mode: mode, w: w, errW: errW}
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
		return func(ev assistant.StreamEvent) { jsonLine(e.w, ev) }
	case modeAgent:
		return func(ev assistant.StreamEvent) { jsonLine(e.w, newAgentStreamEvent(ev)) }
	default:
		return nil
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
func (e *streamEmitter) finish(result assistant.StreamResult, timeoutSeconds int) error {
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
	if result.ContextID != "" {
		_ = assistant.SaveLastContextID(result.ContextID)
	}
	switch e.mode {
	case modeJSONDoc:
		jsonPretty(e.w, promptResult{
			TaskID:    result.TaskID,
			ContextID: result.ContextID,
			Status:    "completed",
			Response:  result.Response,
		})
	case modeAgent:
		e.end(nil)
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
		jsonLine(e.w, assistant.StreamEvent{
			Type:    "error",
			Error:   err.Error(),
			Timeout: timeoutSeconds,
		})
		return emittedFailure(err)
	case modeJSONDoc:
		jsonPretty(e.w, promptResult{
			TaskID:    result.TaskID,
			ContextID: result.ContextID,
			Status:    "timeout",
			Timeout:   timeoutSeconds,
		})
		return emittedFailure(err)
	case modeAgent:
		e.end(&streamEndError{Reason: "timeout", Summary: err.Error(), ExitCode: gcxerrors.ExitGeneralError})
		return emittedFailure(err)
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
			jsonLine(e.w, assistant.StreamEvent{
				Type:      "error",
				TaskID:    result.TaskID,
				ContextID: result.ContextID,
				Error:     result.ErrorMessage,
			})
		}
		return emittedFailure(err)
	case modeJSONDoc:
		jsonPretty(e.w, promptResult{
			TaskID:    result.TaskID,
			ContextID: result.ContextID,
			Status:    "failed",
			Error:     result.ErrorMessage,
		})
		return emittedFailure(err)
	case modeAgent:
		e.end(&streamEndError{Reason: "failed", Summary: err.Error(), ExitCode: gcxerrors.ExitGeneralError})
		return emittedFailure(err)
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
		jsonPretty(e.w, promptResult{
			TaskID:    result.TaskID,
			ContextID: result.ContextID,
			Status:    "canceled",
		})
		return emittedFailure(err)
	case modeAgent:
		e.end(&streamEndError{Reason: "canceled", Summary: err.Error(), ExitCode: gcxerrors.ExitGeneralError})
		return emittedFailure(err)
	case modeHuman:
	}
	cmdio.Warning(e.errW, "Request was canceled")
	return err
}

func (e *streamEmitter) finishUnknown(result assistant.StreamResult) error {
	err := errors.New("request ended in unknown state")
	switch e.mode {
	case modeJSONStream:
		jsonLine(e.w, assistant.StreamEvent{Type: "error", Error: "stream ended unexpectedly"})
		return emittedFailure(err)
	case modeJSONDoc:
		jsonPretty(e.w, promptResult{
			TaskID:    result.TaskID,
			ContextID: result.ContextID,
			Status:    "unknown",
		})
		return emittedFailure(err)
	case modeAgent:
		e.end(&streamEndError{Reason: "unknown", Summary: "stream ended unexpectedly", ExitCode: gcxerrors.ExitGeneralError})
		return emittedFailure(err)
	case modeHuman:
	}
	cmdio.Warning(e.errW, "Request ended unexpectedly. The stream closed without a completion signal.")
	if result.TaskID != "" {
		cmdio.Info(e.errW, "Task ID: %s", result.TaskID)
	}
	return err
}

// end writes the terminal gcx.stream_end line. endErr == nil means success.
func (e *streamEmitter) end(endErr *streamEndError) {
	jsonLine(e.w, streamEndEvent{
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
