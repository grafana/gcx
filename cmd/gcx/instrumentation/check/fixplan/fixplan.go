package fixplan

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/providers"
	assistantprov "github.com/grafana/gcx/internal/providers/assistant"
	otelexplain "github.com/grafana/otel-checker/checks/explain"
	otelutils "github.com/grafana/otel-checker/checks/utils"
)

// Source identifies which path produced a Plan.
type Source string

const (
	SourceAssistant Source = "assistant"
	SourceLocal     Source = "local"
)

// Mode selects the fix-plan generation path. Callers set it from the
// user-facing --fix-plan flag value. The two modes are intentionally
// disjoint: local never calls Assistant (no billing, works offline),
// assistant never falls back to local (any precondition failure returns
// a clear error so the user can retry or switch modes explicitly).
type Mode string

const (
	ModeLocal     Mode = "local"
	ModeAssistant Mode = "assistant"
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
}

// Options configures Generate.
type Options struct {
	// Mode selects the generation path. Required (see Mode constants).
	Mode Mode

	// Loader resolves the current context and its auth. Required when
	// Mode == ModeAssistant; ignored otherwise.
	Loader *providers.ConfigLoader

	// promptRunner is the injection seam for tests. Nil → runAssistant.
	promptRunner func(ctx context.Context, loader *providers.ConfigLoader, message string) (string, error)

	// cloudChecker is the injection seam for tests. Nil → real config load.
	cloudChecker func(context.Context, *providers.ConfigLoader) error
}

// Generate is the entry point. It resolves findings, builds a prompt, and
// dispatches to the selected mode. There is no cross-mode fallback: if
// Mode == ModeAssistant and the Assistant path fails at any point, the
// error is returned as-is so the user can decide whether to retry or
// re-run with --fix-plan=local.
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

	switch opts.Mode {
	case ModeLocal:
		return Plan{
			Source:   SourceLocal,
			Content:  buildLocalPlan(findings, docs),
			DocsUsed: docIDs,
		}, nil
	case ModeAssistant:
		return runAssistantMode(ctx, opts, findings, docs, docIDs)
	default:
		return Plan{}, fmt.Errorf("fixplan.Generate: invalid Mode %q (want %q or %q)", opts.Mode, ModeLocal, ModeAssistant)
	}
}

// runAssistantMode runs the Cloud precondition, then the Assistant call.
// Any failure — non-Cloud context, config error, request failure — is
// returned as a plain error prefixed so the top-level command reporter
// can surface it cleanly. Cancellation propagates so Ctrl+C stops the
// command instead of hiding as a generic failure.
func runAssistantMode(ctx context.Context, opts Options, findings []Finding, docs []otelexplain.Doc, docIDs []string) (Plan, error) {
	if opts.Loader == nil {
		return Plan{}, errors.New("--fix-plan=assistant: no config loader available")
	}

	if err := checkCloud(ctx, opts); err != nil {
		if errors.Is(err, context.Canceled) {
			return Plan{}, err
		}
		return Plan{}, fmt.Errorf("--fix-plan=assistant: Grafana Assistant not available (use --fix-plan=local for an offline plan): %w", err)
	}

	prompt := buildPrompt(findings, docs)
	runner := opts.promptRunner
	if runner == nil {
		runner = runAssistant
	}
	response, err := runner(ctx, opts.Loader, prompt)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return Plan{}, err
		}
		return Plan{}, fmt.Errorf("--fix-plan=assistant: Grafana Assistant request failed (try again, or use --fix-plan=local for an offline plan): %w", err)
	}
	return Plan{
		Source:   SourceAssistant,
		Content:  response,
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
		// cancellation and propagate rather than wrapping as a failure.
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
// It resolves the current context and returns a user-facing error when
// Assistant isn't reachable. Returns nil on success.
//
// Uses LoadConfigTolerant so a strict-validator failure (missing token,
// bad path, malformed context) surfaces here rather than deeper in the
// stream setup, and the caller can wrap it with a clear "--fix-plan=
// assistant not available" prefix.
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
