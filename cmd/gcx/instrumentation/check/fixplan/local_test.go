//nolint:testpackage // white-box testing: accesses unexported extractHowToFix, buildLocalPlan.
package fixplan

import (
	"strings"
	"testing"

	otelexplain "github.com/grafana/otel-checker/checks/explain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractHowToFix_FindsSection(t *testing.T) {
	body := `## Why this matters
Some background.

## How to fix
Set the variable:

    export OTEL_SERVICE_NAME=x

## Example
Full example.
`
	got := extractHowToFix(body)
	assert.Contains(t, got, "Set the variable:")
	assert.Contains(t, got, "export OTEL_SERVICE_NAME=x")
	assert.NotContains(t, got, "Some background.", "Why-this-matters must not leak in")
	assert.NotContains(t, got, "Full example.", "Example must not leak in")
}

func TestExtractHowToFix_FallsBackWhenSectionMissing(t *testing.T) {
	body := "Just some prose with no How-to-fix heading.\n"
	got := extractHowToFix(body)
	assert.Equal(t, "Just some prose with no How-to-fix heading.", got)
}

func TestExtractHowToFix_CaseInsensitive(t *testing.T) {
	body := "## HOW TO FIX\ndo the thing\n"
	got := extractHowToFix(body)
	assert.Equal(t, "do the thing", got)
}

func TestExtractHowToFix_HandlesLastSection(t *testing.T) {
	// No terminating H2 — the "How to fix" section should run to end of body.
	body := "## How to fix\nend of doc\n"
	got := extractHowToFix(body)
	assert.Equal(t, "end of doc", got)
}

func TestExtractHowToFix_AgainstRealExplainDoc(t *testing.T) {
	// Every real explain doc should have a "How to fix" (or the fallback
	// full body still returns non-empty). Grab one and verify extraction
	// returns a non-empty string.
	ids := otelexplain.All()
	require.NotEmpty(t, ids)
	d, ok := otelexplain.Lookup(ids[0])
	require.True(t, ok)
	got := extractHowToFix(d.Body)
	assert.NotEmpty(t, got)
}

func TestBuildLocalPlan_GroupsByDoc(t *testing.T) {
	findings := []Finding{
		{Severity: "WARN", Component: "Common", Message: "namespace missing", ExplainID: "env.resource-attributes.missing"},
		{Severity: "WARN", Component: "Common", Message: "environment missing", ExplainID: "env.resource-attributes.missing"},
	}
	docs := []otelexplain.Doc{
		{ID: "env.resource-attributes.missing", Title: "Resource attribute missing", Severity: "warning",
			Body: "## How to fix\nSet OTEL_RESOURCE_ATTRIBUTES.\n"},
	}
	got := buildLocalPlan(findings, docs)
	assert.Contains(t, got, "# Combined fix")
	assert.Contains(t, got, "## env.resource-attributes.missing — Resource attribute missing")
	assert.Contains(t, got, "namespace missing")
	assert.Contains(t, got, "environment missing")
	assert.Contains(t, got, "Set OTEL_RESOURCE_ATTRIBUTES.")
	// Doc header appears once even though two findings share it.
	assert.Equal(t, 1, strings.Count(got, "## env.resource-attributes.missing"))
}

func TestBuildLocalPlan_OrphansSection(t *testing.T) {
	findings := []Finding{
		{Severity: "FAIL", Component: "Custom", Message: "no explain id here"},
	}
	got := buildLocalPlan(findings, nil)
	assert.Contains(t, got, "Findings without explanation docs")
	assert.Contains(t, got, "no explain id here")
}

func TestBuildLocalPlan_SkipsDocsWithNoFindings(t *testing.T) {
	// Doc resolved but no finding references it — shouldn't appear in output.
	docs := []otelexplain.Doc{
		{ID: "a.b.c", Title: "T", Body: "## How to fix\nx"},
	}
	got := buildLocalPlan(nil, docs)
	assert.NotContains(t, got, "## a.b.c")
}
