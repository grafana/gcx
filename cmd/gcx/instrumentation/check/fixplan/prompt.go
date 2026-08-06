// Package fixplan wraps `gcx instrumentation check` with a fix-plan
// generator: it resolves each finding's explain doc, then either asks
// Grafana Assistant to synthesize a single prioritized plan (Cloud only) or
// falls back to a local aggregation of the docs' "How to fix" sections.
//
// This package is only invoked when the --fix-plan flag is set on
// `gcx instrumentation check`.
package fixplan

import (
	"fmt"
	"sort"
	"strings"

	otelexplain "github.com/grafana/otel-checker/checks/explain"
	otelutils "github.com/grafana/otel-checker/checks/utils"
)

// Finding is the intermediate shape shared by the Assistant prompt and the
// local aggregator. It carries a severity string ("FAIL" or "WARN") to keep
// downstream formatting simple.
type Finding struct {
	Severity  string // "FAIL" or "WARN"
	Component string
	Message   string
	ExplainID string
}

// collectFindings returns findings that carry an explain ID that we know
// how to look up. Successful checks are excluded — they don't need a plan.
// Order is errors first, then warnings, both preserving source order.
func collectFindings(results otelutils.Results) []Finding {
	out := make([]Finding, 0, len(results.Errors)+len(results.Warnings))
	for _, r := range results.Errors {
		out = append(out, Finding{Severity: "FAIL", Component: r.Component, Message: r.Message, ExplainID: r.ExplainID})
	}
	for _, r := range results.Warnings {
		out = append(out, Finding{Severity: "WARN", Component: r.Component, Message: r.Message, ExplainID: r.ExplainID})
	}
	return out
}

// resolveDocs looks up unique explain IDs across the supplied findings and
// returns the corresponding docs, alphabetically ordered so the output is
// diff-stable across runs.
//
// IDs that don't resolve are silently skipped — the coverage test in
// otel-checker guarantees every emitted ID has a doc, but a mismatched
// binary/library pair shouldn't crash the CLI.
func resolveDocs(findings []Finding) []otelexplain.Doc {
	seen := make(map[string]struct{}, len(findings))
	var ids []string
	for _, f := range findings {
		if f.ExplainID == "" {
			continue
		}
		if _, ok := seen[f.ExplainID]; ok {
			continue
		}
		seen[f.ExplainID] = struct{}{}
		ids = append(ids, f.ExplainID)
	}
	sort.Strings(ids)

	docs := make([]otelexplain.Doc, 0, len(ids))
	for _, id := range ids {
		if d, ok := otelexplain.Lookup(id); ok {
			docs = append(docs, d)
		}
	}
	return docs
}

// buildPrompt renders the message sent to Grafana Assistant. The output is
// a single string with three sections — findings, docs, and instructions —
// separated by markdown headers.
//
// Kept pure (no I/O, no time-of-day inputs) so it snapshot-tests cleanly.
func buildPrompt(findings []Finding, docs []otelexplain.Doc) string {
	var b strings.Builder

	b.WriteString("You are helping fix OpenTelemetry instrumentation problems in a local project.\n\n")
	b.WriteString("I ran otel-checker and got these findings:\n\n")

	b.WriteString("# Findings\n\n")
	for _, f := range findings {
		fmt.Fprintf(&b, "%s [%s] %s", f.Severity, f.Component, f.Message)
		if f.ExplainID != "" {
			fmt.Fprintf(&b, " (id: %s)", f.ExplainID)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if len(docs) > 0 {
		b.WriteString("# Explanation Docs\n\n")
		for _, d := range docs {
			fmt.Fprintf(&b, "## %s — %s\n\n%s\n\n", d.ID, d.Title, strings.TrimSpace(d.Body))
		}
	} else {
		b.WriteString("(No explanation docs are available for these findings.)\n\n")
	}

	b.WriteString("# Instructions\n\n")
	b.WriteString("- Produce ONE prioritized plan that resolves every FAIL and WARN above.\n")
	b.WriteString("- Merge overlapping fixes into single steps where the same file, env var, or config value covers multiple findings. Do NOT explain that you merged — just present the merged step directly, with no meta-commentary about combining, deduplication, or which findings it covers.\n")
	b.WriteString("- Order by dependency, then by severity — put fixes that unblock others first.\n")
	b.WriteString("- Do not restate the raw findings; the developer already has them.\n")
	b.WriteString("- Do NOT add a final \"re-run otel-checker\" or \"verify the fix\" step. Stop after the last real change.\n")
	b.WriteString("- When a step sets an environment variable (e.g. `export OTEL_RESOURCE_ATTRIBUTES=...`, `NODE_OPTIONS=...`, or any variable that commonly holds a comma/space-separated list of pre-existing values), add a one-line warning that the shown command REPLACES any current value. If the value is a list-style variable, show the append form too (e.g. `export OTEL_RESOURCE_ATTRIBUTES=\"$OTEL_RESOURCE_ATTRIBUTES,service.namespace=shop\"`) so the reader can pick replace-vs-append based on whether their existing value is wrong or just incomplete. Skip the warning for variables that are single-valued and where replacement is unambiguously the intent (e.g. `OTEL_SERVICE_NAME`, `OTEL_EXPORTER_OTLP_PROTOCOL`).\n")
	b.WriteString("\n")
	b.WriteString("# Output format\n\n")
	b.WriteString("- Do NOT use a markdown numbered list (`1.`, `2.`, ...) for the steps — markdown collapses the spacing between list items when rendered, making the plan look cramped.\n")
	b.WriteString("- Instead, format each step as a level-3 heading followed by its body, and separate steps with a blank line so they render with visible vertical spacing. Use exactly this shape:\n\n")
	b.WriteString("  ```\n")
	b.WriteString("  ### Step 1 — <short imperative title>\n\n")
	b.WriteString("  <one sentence saying what and why>\n\n")
	b.WriteString("  <the exact command / env var / file change>\n\n")
	b.WriteString("  ### Step 2 — <short imperative title>\n\n")
	b.WriteString("  ...\n")
	b.WriteString("  ```\n\n")
	b.WriteString("- Keep a blank line between every step and between the heading, prose, and code block within a step.\n")
	b.WriteString("- Return markdown only. No preamble, no closing summary.\n")

	return b.String()
}
