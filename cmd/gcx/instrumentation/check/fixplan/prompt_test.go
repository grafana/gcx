//nolint:testpackage // white-box testing: accesses unexported collectFindings, resolveDocs, buildPrompt.
package fixplan

import (
	"strings"
	"testing"

	otelexplain "github.com/grafana/otel-checker/checks/explain"
	otelutils "github.com/grafana/otel-checker/checks/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectFindings_OrdersErrorsThenWarnings(t *testing.T) {
	results := otelutils.Results{
		Errors: []otelutils.ComponentResult{
			{Component: "Grafana Cloud", Message: "no headers", ExplainID: "grafana-cloud.headers.missing-auth"},
		},
		Warnings: []otelutils.ComponentResult{
			{Component: "Common", Message: "OTEL_SERVICE_NAME unset", ExplainID: "env.otel-service-name.unset"},
		},
		Checks: []otelutils.ComponentResult{
			{Component: "SDK", Message: "node version OK"},
		},
	}
	got := collectFindings(results)
	require.Len(t, got, 2)
	assert.Equal(t, "FAIL", got[0].Severity)
	assert.Equal(t, "WARN", got[1].Severity)
	assert.Equal(t, "grafana-cloud.headers.missing-auth", got[0].ExplainID)
}

func TestCollectFindings_SkipsSuccessfulChecks(t *testing.T) {
	results := otelutils.Results{
		Checks: []otelutils.ComponentResult{
			{Component: "SDK", Message: "OK", ExplainID: ""},
		},
	}
	assert.Empty(t, collectFindings(results))
}

func TestResolveDocs_DedupsAndSorts(t *testing.T) {
	// Grab two real IDs from the embedded registry so we test against
	// actual data. Emitting the same ID twice should collapse.
	ids := otelexplain.All()
	require.GreaterOrEqual(t, len(ids), 2, "registry must have at least 2 docs to run this test")
	pick := []string{ids[1], ids[0], ids[1]} // out of order + duplicate
	findings := make([]Finding, len(pick))
	for i, id := range pick {
		findings[i] = Finding{Severity: "FAIL", Component: "x", Message: "m", ExplainID: id}
	}
	docs := resolveDocs(findings)
	require.Len(t, docs, 2, "expected two unique docs")
	// Alphabetical order.
	assert.Less(t, docs[0].ID, docs[1].ID, "expected alphabetical order, got %q %q", docs[0].ID, docs[1].ID)
}

func TestResolveDocs_SkipsUnknownIDs(t *testing.T) {
	findings := []Finding{
		{ExplainID: "totally.made-up.id"},
	}
	assert.Empty(t, resolveDocs(findings))
}

func TestBuildPrompt_IncludesFindingsAndDocs(t *testing.T) {
	findings := []Finding{
		{Severity: "FAIL", Component: "Grafana Cloud", Message: "no headers", ExplainID: "grafana-cloud.headers.missing-auth"},
	}
	docs := []otelexplain.Doc{
		{ID: "grafana-cloud.headers.missing-auth", Title: "Headers missing", Body: "## How to fix\nSet the header.\n"},
	}
	out := buildPrompt(findings, docs)
	// Sections present.
	assert.Contains(t, out, "# Findings")
	assert.Contains(t, out, "# Explanation Docs")
	assert.Contains(t, out, "# Instructions")
	// Finding text present with severity, component, id.
	assert.Contains(t, out, "FAIL [Grafana Cloud] no headers")
	assert.Contains(t, out, "(id: grafana-cloud.headers.missing-auth)")
	// Doc header with ID and title.
	assert.Contains(t, out, "## grafana-cloud.headers.missing-auth — Headers missing")
	// Instruction imperative present.
	assert.Contains(t, out, "ONE prioritized plan")
}

func TestBuildPrompt_NoDocsPathStillEmitsSections(t *testing.T) {
	findings := []Finding{{Severity: "WARN", Component: "x", Message: "y"}}
	out := buildPrompt(findings, nil)
	assert.Contains(t, out, "# Findings")
	assert.Contains(t, out, "No explanation docs are available")
	assert.NotContains(t, out, "# Explanation Docs\n\n##", "should not emit an empty docs block")
}

func TestBuildPrompt_StableAcrossRuns(t *testing.T) {
	// Prompt building is pure — same input must produce the same output.
	// Guards against nondeterministic map iteration slipping in.
	findings := []Finding{
		{Severity: "FAIL", Component: "A", Message: "one", ExplainID: "a.b.c"},
		{Severity: "WARN", Component: "B", Message: "two", ExplainID: "a.b.c"},
	}
	docs := []otelexplain.Doc{{ID: "a.b.c", Title: "T", Body: "B"}}
	first := buildPrompt(findings, docs)
	for range 5 {
		assert.Equal(t, first, buildPrompt(findings, docs))
	}
	assert.Positive(t, strings.Count(first, "a.b.c"))
}
