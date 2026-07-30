package assistant

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
)

// PromptRequest is the input to RunPrompt for other command trees embedding
// Grafana Assistant. It intentionally mirrors the subset of `gcx assistant
// prompt` flags that non-interactive callers actually need — no interactive
// approval, no NDJSON streaming, no --no-stream toggle.
type PromptRequest struct {
	// Message is the prompt text sent to the Assistant.
	Message string

	// AgentID selects the target agent. Empty means the default CLI agent.
	AgentID string

	// ContextID resumes an existing conversation. Empty starts a new one.
	// Mutually exclusive with Continue.
	ContextID string

	// Continue loads the last saved context ID from disk (matches
	// `gcx assistant prompt --continue`). Mutually exclusive with ContextID.
	Continue bool

	// TimeoutSeconds bounds the streaming response. ≤0 defaults to 300s.
	TimeoutSeconds int

	// ApprovalHandler decides whether Assistant may run tool calls that
	// require user approval. Nil means REJECT every approval request
	// (fail-closed default — safe for shared use). Callers that need
	// non-interactive auto-approval should pass AlwaysApprove{} explicitly
	// and only after auditing which tools Assistant might invoke on their
	// prompt, since approvals granted here can mutate cloud or filesystem
	// state without prompting.
	ApprovalHandler assistant.ApprovalHandler

	// PersistContextID controls whether a successful response's ContextID
	// overwrites the last-context-ID on disk (which powers
	// `gcx assistant prompt --continue`). Default false so embed callers
	// don't silently hijack an unrelated conversation the user is building
	// in a separate `gcx assistant prompt` session.
	PersistContextID bool
}

// PromptResponse is the successful result of RunPrompt.
type PromptResponse struct {
	// Response is the full assembled response text.
	Response string

	// ContextID is the conversation ID for chained follow-up calls.
	ContextID string

	// TaskID is the Assistant-side task identifier.
	TaskID string
}

// RunPrompt sends a single message to Grafana Assistant and returns the full
// response. It handles auth resolution via ConfigLoader, context-ID lookup
// for Continue, and streaming completion.
//
// Defaults are fail-closed for shared-helper safety:
//
//   - Tool-call approvals: nil ApprovalHandler → every request is DENIED.
//     Pass AlwaysApprove{} on PromptRequest to opt into non-interactive
//     auto-approval, or supply your own handler.
//   - Context-ID persistence: PersistContextID defaults to false, so a
//     successful call does NOT overwrite the last-context-ID that powers
//     `gcx assistant prompt --continue`. Set to true only when the caller
//     actually wants continuation semantics.
//
// Callers are responsible for enforcing Grafana Cloud via RequireGrafanaCloud
// before invoking this function; RunPrompt will surface the underlying
// authentication error otherwise but will not produce a Cloud-specific
// message.
func RunPrompt(ctx context.Context, loader *providers.ConfigLoader, req PromptRequest) (PromptResponse, error) {
	if req.Message == "" {
		return PromptResponse{}, errors.New("assistant.RunPrompt: message is required")
	}
	if req.ContextID != "" && req.Continue {
		return PromptResponse{}, errors.New("assistant.RunPrompt: ContextID and Continue are mutually exclusive")
	}

	contextID := req.ContextID
	if req.Continue {
		id, err := assistant.GetLastContextID()
		if err != nil {
			return PromptResponse{}, fmt.Errorf("assistant: continue: %w", err)
		}
		contextID = id
	}

	clientOpts, err := resolveAssistantClientOptions(ctx, loader, req.TimeoutSeconds, req.AgentID)
	if err != nil {
		return PromptResponse{}, err
	}
	c := assistant.New(clientOpts) //nolint:contextcheck // assistant.New does not accept context; ctx is threaded into c.Chat below.

	if contextID != "" {
		if _, err := c.ValidateCLIContext(ctx, contextID); err != nil {
			return PromptResponse{}, err
		}
	}

	streamOpts := assistant.StreamOptions{
		Timeout:   req.TimeoutSeconds,
		ContextID: contextID,
	}
	// Bare c.Chat is `ChatWithApproval(..., nil)`, which auto-DENIES every
	// approval request and — in practice on Grafana Cloud — surfaces those
	// denials as an unhelpful "HTTP 500: Permission check failed" rather
	// than a clean stream error. Always route through ChatWithApproval and
	// let the caller's policy (or the rejecting default) decide.
	handler := req.ApprovalHandler
	if handler == nil {
		handler = rejectApprovalHandler{}
	}
	result := c.ChatWithApproval(ctx, req.Message, streamOpts, handler)

	switch {
	case result.Completed:
		if req.PersistContextID && result.ContextID != "" {
			_ = assistant.SaveLastContextID(result.ContextID)
		}
		return PromptResponse{
			Response:  result.Response,
			ContextID: result.ContextID,
			TaskID:    result.TaskID,
		}, nil
	case result.TimedOut:
		timeout := req.TimeoutSeconds
		if timeout <= 0 {
			timeout = 300
		}
		return PromptResponse{}, fmt.Errorf("assistant: request timed out after %ds", timeout)
	case result.Canceled:
		return PromptResponse{}, errors.New("assistant: request canceled")
	case result.Failed:
		return PromptResponse{}, fmt.Errorf("assistant: %s", result.ErrorMessage)
	default:
		return PromptResponse{}, errors.New("assistant: stream ended without completion")
	}
}

// RequireGrafanaCloud returns an error if the given Grafana context is not
// a Grafana Cloud stack. Callers should check this before invoking RunPrompt
// to produce a clearer error than the raw auth failure.
func RequireGrafanaCloud(ctx *config.Context) error {
	return requireGrafanaCloud(ctx)
}

// AlwaysApprove is an ApprovalHandler that approves every request. Callers
// pass this in PromptRequest.ApprovalHandler when they explicitly want
// non-interactive auto-approval — for example `gcx instrumentation check
// --fix-plan`, which asks Assistant to synthesize markdown and doesn't
// expect tool calls.
//
// Do NOT use this if any Assistant tool your prompt could reach might
// mutate cloud or filesystem state; every approval here is granted without
// prompting.
type AlwaysApprove struct{}

// HandleApproval satisfies assistant.ApprovalHandler by approving every
// request unconditionally.
func (AlwaysApprove) HandleApproval(_ assistant.ApprovalRequest) bool {
	return true
}

// rejectApprovalHandler denies every approval request. It's the fail-closed
// default used by RunPrompt when the caller does not supply an
// ApprovalHandler on PromptRequest.
type rejectApprovalHandler struct{}

func (rejectApprovalHandler) HandleApproval(_ assistant.ApprovalRequest) bool {
	return false
}
