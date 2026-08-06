package fixplan

import (
	"fmt"
	"regexp"
	"strings"

	otelexplain "github.com/grafana/otel-checker/checks/explain"
)

// howToFixHeading matches a markdown H2 header whose text starts with
// "how to fix" (case-insensitive). Explain docs conventionally use exactly
// "## How to fix", but we tolerate light variation.
var howToFixHeading = regexp.MustCompile(`(?im)^##\s+how to fix\b.*$`)

// nextH2 matches any subsequent markdown H2 header, used to bound the
// extracted section body.
var nextH2 = regexp.MustCompile(`(?m)^##\s+.*$`)

// extractHowToFix returns the body of the doc's "## How to fix" section, or
// the whole body when no such section exists. Leading and trailing
// whitespace on the returned block is trimmed.
func extractHowToFix(body string) string {
	loc := howToFixHeading.FindStringIndex(body)
	if loc == nil {
		return strings.TrimSpace(body)
	}
	after := body[loc[1]:]
	if end := nextH2.FindStringIndex(after); end != nil {
		after = after[:end[0]]
	}
	return strings.TrimSpace(after)
}

// buildLocalPlan renders a deterministic markdown document that groups
// findings by their explain doc and reproduces each doc's "How to fix"
// section. It is the fallback surface for environments where Grafana
// Assistant isn't reachable (self-hosted, missing auth, etc.).
//
// Docs whose findings didn't resolve to a real explain doc are collected at
// the end under a "Findings without explanation docs" heading.
func buildLocalPlan(findings []Finding, docs []otelexplain.Doc) string {
	byID := make(map[string][]Finding, len(docs))
	var orphans []Finding
	for _, f := range findings {
		if f.ExplainID == "" {
			orphans = append(orphans, f)
			continue
		}
		byID[f.ExplainID] = append(byID[f.ExplainID], f)
	}

	var b strings.Builder
	b.WriteString("# Combined fix (local aggregation — no AI reasoning applied)\n\n")
	b.WriteString("Each section below reproduces the `How to fix` block from an otel-checker explanation ")
	b.WriteString("doc, grouped by the findings it covers. Apply the sections in order.\n\n")

	for _, d := range docs {
		fs, ok := byID[d.ID]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "## %s — %s\n\n", d.ID, d.Title)
		fmt.Fprintf(&b, "_Severity: %s_\n\n", d.Severity)
		b.WriteString("Findings this addresses:\n\n")
		for _, f := range fs {
			fmt.Fprintf(&b, "- %s [%s] %s\n", f.Severity, f.Component, f.Message)
		}
		b.WriteString("\n")
		b.WriteString(extractHowToFix(d.Body))
		b.WriteString("\n\n")
	}

	if len(orphans) > 0 {
		b.WriteString("## Findings without explanation docs\n\n")
		b.WriteString("These findings do not carry an explain ID. Review the raw messages:\n\n")
		for _, f := range orphans {
			fmt.Fprintf(&b, "- %s [%s] %s\n", f.Severity, f.Component, f.Message)
		}
		b.WriteString("\n")
	}

	return b.String()
}
