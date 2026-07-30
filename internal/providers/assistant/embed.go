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
// for Continue, and streaming completion. It does NOT wire an interactive
// approval handler — callers embedding this in other commands should not be
// interactive.
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
	result := c.ChatWithApproval(ctx, req.Message, streamOpts, autoApproveHandler{})

	switch {
	case result.Completed:
		if result.ContextID != "" {
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

// autoApproveHandler satisfies assistant.ApprovalHandler by approving every
// request. Used by RunPrompt for non-interactive embed callers where there
// is no stdin to prompt on.
type autoApproveHandler struct{}

func (autoApproveHandler) HandleApproval(_ assistant.ApprovalRequest) bool {
	return true
}
