package fixplan

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/providers"
	assistantprov "github.com/grafana/gcx/internal/providers/assistant"
	otelutils "github.com/grafana/otel-checker/checks/utils"
)

// Source identifies which path produced a Plan. Callers use this to render
// an appropriate leading notice ("assistant" is silent; "local" prints a
// stderr notice explaining the degradation).
type Source string

const (
	SourceAssistant Source = "assistant"
	SourceLocal     Source = "local"
)

// Plan is the output of Generate — also the on-wire shape of the fix_plan
// envelope emitted under -o json/yaml.
//
// Empty (Empty == true) means there were no findings that need a fix plan;
// callers should skip rendering and attach nothing on the wire. Empty is
// json:"-" because it's an internal signal, not part of the payload.
type Plan struct {
	Source   Source   `json:"source" yaml:"source"`
	Content  string   `json:"content" yaml:"content"` // markdown fix plan
	DocsUsed []string `json:"docs_used,omitempty" yaml:"docs_used,omitempty"`
	Empty    bool     `json:"-" yaml:"-"`
	Fallback bool     `json:"fallback,omitempty" yaml:"fallback,omitempty"` // true when Assistant was requested but local was used
	Reason   string   `json:"reason,omitempty" yaml:"reason,omitempty"`     // when Fallback: one-line human explanation
}

// Options configures Generate. All optional; sensible defaults are applied.
type Options struct {
	// Loader resolves the current context and its auth for the Assistant
	// path. Required when Assistant may be called.
	Loader *providers.ConfigLoader

	// promptRunner is the injection seam for tests. Nil → runAssistant.
	promptRunner func(ctx context.Context, loader *providers.ConfigLoader, message string) (string, error)

	// cloudChecker is the injection seam for tests. Nil → real config load.
	cloudChecker func(context.Context, *providers.ConfigLoader) error
}

// Generate is the entry point. It resolves findings, builds a prompt,
// decides between Assistant and local aggregation, and returns a Plan.
func Generate(ctx context.Context, results otelutils.Results, opts Options) (Plan, error) {
	findings := collectFindings(results)
	if len(findings) == 0 {
		return Plan{Empty: true}, nil
	}
	docs := resolveDocs(findings)

	docIDs := make([]string, 0, len(docs))
	for _, d := range docs {
		docIDs = append(docIDs, d.ID)
	}

	prompt := buildPrompt(findings, docs)

	// Try Assistant first when a loader is provided.
	if opts.Loader != nil {
		cloudErr := checkCloud(ctx, opts)
		if cloudErr != nil {
			if errors.Is(cloudErr, context.Canceled) {
				return Plan{}, cloudErr
			}
			// Intentional fallback: Assistant isn't reachable, but the
			// local aggregator still produces a useful plan. Surface the
			// reason via Plan.Reason instead of a fatal error.
			return Plan{ //nolint:nilerr // fallback is intentional; reason is preserved on Plan.
				Source:   SourceLocal,
				Content:  buildLocalPlan(findings, docs),
				DocsUsed: docIDs,
				Fallback: true,
				Reason:   cloudErr.Error(),
			}, nil
		}

		runner := opts.promptRunner
		if runner == nil {
			runner = runAssistant
		}
		response, err := runner(ctx, opts.Loader, prompt)
		if err != nil {
			// A ctrl+C during the assistant call should surface as a real
			// cancellation, not as a Fallback plan that hides why the run
			// stopped. Check both the returned error and the context state
			// because the assistant SDK may return its own error string
			// rather than propagating context.Canceled directly.
			if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				return Plan{}, err
			}
			// Same fallback contract as the Cloud-check branch above:
			// the assistant call failed, but the local aggregator still
			// produces a usable plan. Surface the reason on Plan.Reason
			// rather than treating it as fatal.
			return Plan{
				Source:   SourceLocal,
				Content:  buildLocalPlan(findings, docs),
				DocsUsed: docIDs,
				Fallback: true,
				Reason:   fmt.Sprintf("Assistant request failed: %s", err),
			}, nil
		}
		return Plan{
			Source:   SourceAssistant,
			Content:  response,
			DocsUsed: docIDs,
		}, nil
	}

	// No loader — go straight to local.
	return Plan{
		Source:   SourceLocal,
		Content:  buildLocalPlan(findings, docs),
		DocsUsed: docIDs,
	}, nil
}

// runAssistant is the default promptRunner. It resolves client options,
// opens a streaming chat with auto-approve, and returns the assembled
// response text.
//
// Auto-approve is intentional: the fix-plan prompt asks for markdown
// synthesis, not tool calls, and there is no interactive stdin here. Bare
// c.Chat is `ChatWithApproval(..., nil)`, which auto-DENIES every approval
// request — and, in practice on Grafana Cloud, surfaces those denials as
// an unhelpful "HTTP 500: Permission check failed" rather than a clean
// stream error. Passing alwaysApprove{} avoids that.
func runAssistant(ctx context.Context, loader *providers.ConfigLoader, message string) (string, error) {
	clientOpts, err := assistantprov.ResolveClientOptions(ctx, loader, 0, "")
	if err != nil {
		return "", err
	}
	c := assistant.New(clientOpts) //nolint:contextcheck // assistant.New does not accept context; ctx is threaded into ChatWithApproval below.
	result := c.ChatWithApproval(ctx, message, assistant.StreamOptions{}, alwaysApprove{})
	switch {
	case result.Completed:
		return result.Response, nil
	case result.TimedOut:
		return "", errors.New("assistant: request timed out")
	case result.Canceled:
		// Return context.Canceled so callers can errors.Is-detect the
		// cancellation and skip the local-fallback path.
		return "", context.Canceled
	case result.Failed:
		return "", fmt.Errorf("assistant: %s", result.ErrorMessage)
	default:
		return "", errors.New("assistant: stream ended without completion")
	}
}

// alwaysApprove approves every tool-approval request. See runAssistant for
// the rationale.
type alwaysApprove struct{}

func (alwaysApprove) HandleApproval(_ assistant.ApprovalRequest) bool { return true }

// checkCloud runs the Grafana-Cloud precondition for the Assistant path.
// It resolves the current context and returns a user-facing error string
// when Assistant isn't reachable. Returns nil on success.
//
// Uses LoadConfigTolerant so a strict-validator failure (missing token,
// bad path, malformed context) still produces a useful "Assistant not
// available: <reason>" fallback rather than making that config error look
// like an Assistant error.
func checkCloud(ctx context.Context, opts Options) error {
	if opts.cloudChecker != nil {
		return opts.cloudChecker(ctx, opts.Loader)
	}
	cfg, err := opts.Loader.LoadConfigTolerant(ctx)
	if err != nil {
		return err
	}
	curCtx := cfg.Contexts[cfg.CurrentContext]
	if curCtx == nil {
		return errors.New("no current context configured")
	}
	return assistantprov.RequireGrafanaCloud(curCtx)
}
