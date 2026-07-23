package fixplan

import (
	"context"
	"errors"

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

// Plan is the output of Generate. Empty (Empty == true) when there are no
// findings that need a fix plan; callers should skip rendering in that case.
type Plan struct {
	Source   Source
	Content  string   // markdown
	DocsUsed []string // explain IDs the plan consulted
	Empty    bool     // true when there are no error/warning findings
	Fallback bool     // true when Assistant was requested but local was used
	Reason   string   // when Fallback: one-line human explanation
}

// Options configures Generate. All optional; sensible defaults are applied.
type Options struct {
	// Loader resolves the current context and its auth for the Assistant
	// path. Required when Assistant may be called.
	Loader *providers.ConfigLoader

	// AgentID selects a specialist Assistant agent. Empty → default.
	AgentID string

	// TimeoutSeconds bounds the Assistant streaming call. ≤0 → 300s.
	TimeoutSeconds int

	// PrintPromptOnly returns the built Assistant prompt without calling
	// Assistant. Content is the prompt text; Source is set to
	// SourceAssistant. No Assistant tokens are consumed.
	PrintPromptOnly bool

	// promptRunner is the injection seam for tests. Nil → assistantprov.RunPrompt.
	promptRunner func(context.Context, *providers.ConfigLoader, assistantprov.PromptRequest) (assistantprov.PromptResponse, error)

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

	if opts.PrintPromptOnly {
		return Plan{
			Source:   SourceAssistant,
			Content:  prompt,
			DocsUsed: docIDs,
		}, nil
	}

	// Try Assistant first when a loader is provided.
	if opts.Loader != nil {
		cloudErr := checkCloud(ctx, opts)
		if cloudErr != nil {
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
			runner = assistantprov.RunPrompt
		}
		resp, err := runner(ctx, opts.Loader, assistantprov.PromptRequest{
			Message:        prompt,
			AgentID:        opts.AgentID,
			TimeoutSeconds: opts.TimeoutSeconds,
		})
		if err != nil {
			// Same fallback contract as the Cloud-check branch above.
			return Plan{ //nolint:nilerr // fallback is intentional; reason is preserved on Plan.
				Source:   SourceLocal,
				Content:  buildLocalPlan(findings, docs),
				DocsUsed: docIDs,
				Fallback: true,
				Reason:   "Assistant request failed: " + err.Error(),
			}, nil
		}
		return Plan{
			Source:   SourceAssistant,
			Content:  resp.Response,
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

// checkCloud runs the Grafana-Cloud precondition for the Assistant path.
// It resolves the current context and returns a user-facing error string
// when Assistant isn't reachable. Returns nil on success.
func checkCloud(ctx context.Context, opts Options) error {
	if opts.cloudChecker != nil {
		return opts.cloudChecker(ctx, opts.Loader)
	}
	cfg, err := opts.Loader.LoadConfig(ctx)
	if err != nil {
		return err
	}
	curCtx := cfg.Contexts[cfg.CurrentContext]
	if curCtx == nil {
		return errors.New("no current context configured")
	}
	return assistantprov.RequireGrafanaCloud(curCtx)
}
