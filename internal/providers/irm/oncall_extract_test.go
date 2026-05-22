//nolint:testpackage // white-box tests require access to unexported IRM types and helpers
package irm

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestStripTitleTargetSuffix asserts that OnCall's render_for_web titles —
// which always include a trailing "(cluster, namespace)" target block —
// have that block removed when the contents look like cluster/service/
// namespace identifiers. Alert names that legitimately contain parens
// (e.g. "FailedDeploys (canary)" where the qualifier is human-readable
// prose) are left alone.
func TestStripTitleTargetSuffix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single namespace", "KubePodNotReady (grafana-apps)", "KubePodNotReady"},
		{"cluster + namespace", "CloudSQLSlowQueries (prod-eu-west-2, machine-learning)", "CloudSQLSlowQueries"},
		{"three identifiers", "Foo (a, b, c)", "Foo"},
		{"no parens", "AlwaysFiringAlert", "AlwaysFiringAlert"},
		{"compound name with hyphen and parens", "GrafanaRulerWriteTimeSeries - FastErrorBudgetBurn (prod-eu-west-2, grafana-ruler)", "GrafanaRulerWriteTimeSeries - FastErrorBudgetBurn"},
		{"empty", "", ""},
		// Conservative: don't strip parentheticals that contain prose
		// (spaces inside the parens, or 4+ comma-separated segments).
		{"prose left intact", "FailedDeploys (canary build)", "FailedDeploys (canary build)"},
		{"too many commas left intact", "Wide (a, b, c, d)", "Wide (a, b, c, d)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := stripTitleTargetSuffix(tc.in)
			if got != tc.want {
				t.Errorf("stripTitleTargetSuffix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestExtractTitleFromRenderForWeb asserts the title extraction strips the
// target suffix even when the render_for_web blob contains noise.
func TestExtractTitleFromRenderForWeb(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   json.RawMessage
		want string
	}{
		{"with target", json.RawMessage(`{"title": "PyroscopeRingHasUnhealthyMembers (prod-au-southeast-1, profiles-prod-016)"}`), "PyroscopeRingHasUnhealthyMembers"},
		{"no target", json.RawMessage(`{"title": "k6CloudIngestLagOverThreshold"}`), "k6CloudIngestLagOverThreshold"},
		{"empty input", nil, ""},
		{"null", json.RawMessage(`null`), ""},
		{"missing title", json.RawMessage(`{"message": "..."}`), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractTitleFromRenderForWeb(tc.in)
			if got != tc.want {
				t.Errorf("extractTitleFromRenderForWeb(%q) = %q, want %q", string(tc.in), got, tc.want)
			}
		})
	}
}

// TestExtractSeverityFromRenderForWeb asserts the HTML-fallback severity
// extractor pulls "warning" / "critical" / etc. out of the OnCall server-
// rendered message body so the SEVERITY column on the list path renders
// real values when last_alert.raw_request_data is absent.
func TestExtractSeverityFromRenderForWeb(t *testing.T) {
	t.Parallel()
	const sample = `{"message": "<p>Status: firing</p><ul><li>cluster: prod  </li><li>severity: warning  </li><li>alertname: Foo  </li></ul>"}`
	cases := []struct {
		name string
		in   json.RawMessage
		want string
	}{
		{"warning from CommonLabels", json.RawMessage(sample), "warning"},
		{"critical", json.RawMessage(`{"message": "<li>severity: critical</li>"}`), "critical"},
		{"none", json.RawMessage(`{"message": "<li>severity: none</li>"}`), "none"},
		{"missing severity", json.RawMessage(`{"message": "<li>foo: bar</li>"}`), ""},
		{"empty", nil, ""},
		{"no message field", json.RawMessage(`{"title": "x"}`), ""},
		{"malformed JSON", json.RawMessage(`{not json`), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractSeverityFromRenderForWeb(tc.in)
			if got != tc.want {
				t.Errorf("extractSeverityFromRenderForWeb(%q) = %q, want %q", string(tc.in), got, tc.want)
			}
		})
	}
}

// TestIsDenylistedLabelKey covers the noise-label predicate used by
// filterLabels / extractSubject / extractDimensions. Round 2 tightened the
// rules:
//
//   - the `__`-prefix rule is single-leading (not wrap-style), so any key
//     starting with `__` is denylisted regardless of suffix shape.
//   - the explicit list now also covers free-form annotation keys
//     (description, summary, runbook_url, message, *_url).
func TestIsDenylistedLabelKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key  string
		want bool
	}{
		{"", true},
		// Wrap-style internals — caught by single-leading prefix rule.
		{"__name__", true},
		{"__alert_rule_uid__", true},
		{"__grafana_managed_route__", true},
		{"__converted_prometheus_rule__", true},
		// Single-leading `__`-prefix keys (round 2 — list-path scrape
		// pulls these in, brief calls them out by name).
		{"__enriched_by", true},
		{"__bypass_imported_global_am_allowlist", true},
		{"__grafana_origin", true},
		{"__ai_explanation", true},
		{"__enrich_logs_lines", true},
		{"__partial", true},
		// Pre-existing explicit denylist.
		{"alertname", true},
		{"severity", true},
		{"grafana_folder", true},
		// Round 2 explicit additions: annotation keys that the OnCall HTML
		// scrape pulls in alongside real labels.
		{"description", true},
		{"summary", true},
		{"runbook_url", true},
		{"message", true},
		{"dashboard_url", true},
		{"documentation_url", true},
		{"playbook_url", true},
		// Innocuous keys still pass through.
		{"cluster", false},
		{"namespace", false},
		{"service", false},
		{"pod", false},
		{"job", false},
		{"asserts_env", false},
		// Custom user annotations with non-standard names still pass
		// (accepted residual per round 2 brief).
		{"custom_label", false},
		{"my_team_url", false},
		// Boundary: trailing-only `__` is NOT covered (prefix rule is
		// leading-only).
		{"partial__", false},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			if got := isDenylistedLabelKey(tc.key); got != tc.want {
				t.Errorf("isDenylistedLabelKey(%q) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}

// TestRenderLabelCell covers the single-line label cell renderer used by
// the SUBJECT (alert-groups list) and DIMENSIONS (list-alerts) columns
// in default-table mode.
func TestRenderLabelCell(t *testing.T) {
	t.Parallel()
	subjectPriority := SubjectLabelPriority
	cases := []struct {
		name     string
		labels   map[string]string
		priority []string
		budget   int
		want     string
	}{
		{
			name:   "empty input renders dash",
			labels: nil,
			want:   "-",
		},
		{
			name:     "single matched-priority key, no extras",
			labels:   map[string]string{"service": "ruler"},
			priority: subjectPriority,
			want:     "service=ruler",
		},
		{
			name:     "matched priority + extras renders +N",
			labels:   map[string]string{"service": "ruler", "cluster": "prod-eu", "namespace": "ruler-prod"},
			priority: subjectPriority,
			want:     "service=ruler (+2)",
		},
		{
			name:     "alpha fallback when no priority key present",
			labels:   map[string]string{"zone": "us-east", "tier": "fastpath"},
			priority: subjectPriority,
			// alphabetical: tier comes before zone; +1 for zone.
			want: "tier=fastpath (+1)",
		},
		{
			name:     "denylisted keys never picked",
			labels:   map[string]string{"alertname": "x", "severity": "warning", "service": "ruler"},
			priority: subjectPriority,
			want:     "service=ruler",
		},
		{
			name:     "all-denylisted renders dash",
			labels:   map[string]string{"alertname": "x", "severity": "warning", "__name__": "y"},
			priority: subjectPriority,
			want:     "-",
		},
		{
			name:     "budget overflow truncates with ellipsis",
			labels:   map[string]string{"service": "abcdefghijklmnopqrstuvwxyz"},
			priority: subjectPriority,
			budget:   12,
			want:     "service=abc…",
		},
		{
			name:     "dimensions priority — pod first",
			labels:   map[string]string{"pod": "foo-123", "instance": "bar"},
			priority: DimensionsLabelPriority,
			want:     "pod=foo-123 (+1)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := RenderLabelCell(tc.labels, tc.priority, tc.budget)
			if got != tc.want {
				t.Errorf("RenderLabelCell() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestExtractSubject covers the commonLabels → status.subject.labels filter.
func TestExtractSubject(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   map[string]string
		want *AlertSubject
	}{
		{"empty", nil, nil},
		{"only denylisted", map[string]string{"alertname": "x", "severity": "warning"}, nil},
		{"mix kept", map[string]string{
			"cluster":            "prod",
			"alertname":          "x",
			"__alert_rule_uid__": "abc",
		}, &AlertSubject{Labels: map[string]string{"cluster": "prod"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractSubject(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("extractSubject(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestExtractDimensions covers the per-alert label diff vs commonLabels
// (set difference by VALUE) with denylist applied AFTER the diff.
func TestExtractDimensions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		alert     map[string]string
		common    map[string]string
		wantNil   bool
		wantLabel map[string]string
	}{
		{"empty alert", nil, map[string]string{"cluster": "prod"}, true, nil},
		{"all alert labels match common", map[string]string{"cluster": "prod"}, map[string]string{"cluster": "prod"}, true, nil},
		{"differing value kept", map[string]string{"cluster": "prod"}, map[string]string{"cluster": "stage"}, false, map[string]string{"cluster": "prod"}},
		{"alert-only label kept", map[string]string{"pod": "foo-1"}, map[string]string{"cluster": "prod"}, false, map[string]string{"pod": "foo-1"}},
		{"denylisted alert label discarded", map[string]string{"pod": "foo-1", "alertname": "x"}, map[string]string{}, false, map[string]string{"pod": "foo-1"}},
		{"denylisted as only diff", map[string]string{"alertname": "x"}, map[string]string{}, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractDimensions(tc.alert, tc.common)
			if tc.wantNil {
				if got != nil {
					t.Errorf("extractDimensions() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("extractDimensions() = nil, want %v", tc.wantLabel)
			}
			if !reflect.DeepEqual(got.Labels, tc.wantLabel) {
				t.Errorf("extractDimensions() labels = %v, want %v", got.Labels, tc.wantLabel)
			}
		})
	}
}

// TestExtractCommonLabelsFromRenderForWeb covers the HTML-scrape fallback
// used on the alert-groups list path where last_alert.raw_request_data is
// absent.
func TestExtractCommonLabelsFromRenderForWeb(t *testing.T) {
	t.Parallel()
	const sample = `{"message": "<p>Status: firing</p><ul>` +
		`<li>cluster: prod-eu-west-2  </li>` +
		`<li>severity: warning  </li>` +
		`<li>alertname: KubePodNotReady</li>` +
		`<li>service: ruler</li>` +
		`</ul>"}`
	got := extractCommonLabelsFromRenderForWeb(json.RawMessage(sample))
	want := map[string]string{
		"cluster":   "prod-eu-west-2",
		"severity":  "warning",
		"alertname": "KubePodNotReady",
		"service":   "ruler",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extractCommonLabelsFromRenderForWeb() = %v, want %v", got, want)
	}

	// Empty / no-message inputs.
	for _, tc := range []struct {
		name string
		in   json.RawMessage
	}{
		{"empty bytes", nil},
		{"null", json.RawMessage(`null`)},
		{"no message field", json.RawMessage(`{"title": "x"}`)},
		{"malformed", json.RawMessage(`{not json`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractCommonLabelsFromRenderForWeb(tc.in)
			if got != nil {
				t.Errorf("extractCommonLabelsFromRenderForWeb(%q) = %v, want nil", string(tc.in), got)
			}
		})
	}
}

// TestCanonicalLabelKey checks order-independence of the collapse-key
// canonicalization used by alertCollapseKey's dimensions-fallback branch.
func TestCanonicalLabelKey(t *testing.T) {
	t.Parallel()
	a := canonicalLabelKey(map[string]string{"pod": "foo", "container": "ruler"})
	b := canonicalLabelKey(map[string]string{"container": "ruler", "pod": "foo"})
	if a != b {
		t.Errorf("canonicalLabelKey not order-independent: %q vs %q", a, b)
	}
	if canonicalLabelKey(nil) != "" {
		t.Errorf("canonicalLabelKey(nil) should return \"\"")
	}
	got := canonicalLabelKey(map[string]string{"a": "1", "b": "2"})
	want := "a=1\x1fb=2"
	if got != want {
		t.Errorf("canonicalLabelKey ordering: got %q, want %q", got, want)
	}
}

// TestRenderLabelCellMultiLine — wide-mode renderer.
func TestRenderLabelCellMultiLine(t *testing.T) {
	t.Parallel()
	got := renderLabelCellMultiLine(map[string]string{
		"cluster":   "prod",
		"namespace": "ruler",
		"service":   "ruler-app",
		"alertname": "X", // denylisted
	}, SubjectLabelPriority)
	// service first (priority idx 0), then namespace (idx 6), then cluster (idx 7).
	want := "service=ruler-app\nnamespace=ruler\ncluster=prod"
	if got != want {
		t.Errorf("renderLabelCellMultiLine() = %q, want %q", got, want)
	}
	if renderLabelCellMultiLine(nil, SubjectLabelPriority) != "-" {
		t.Errorf("expected '-' for empty input")
	}

	// Alpha fallback for non-priority keys, stable order.
	got2 := renderLabelCellMultiLine(map[string]string{
		"zone":   "us-east",
		"region": "us",
	}, SubjectLabelPriority)
	lines := strings.Split(got2, "\n")
	sortedKeys := make([]string, len(lines))
	copy(sortedKeys, lines)
	sort.Strings(sortedKeys)
	if !reflect.DeepEqual(lines, sortedKeys) {
		t.Errorf("alpha fallback not sorted: got %v", lines)
	}
}

// TestApplyAlertCollapse — collapse / --history semantics.
func TestApplyAlertCollapse(t *testing.T) {
	t.Parallel()
	mkEnv := func(name, fp string, dim map[string]string) alertEnvelope {
		env := alertEnvelope{
			Metadata: k8sMetadata{Name: name},
		}
		if fp != "" {
			env.Status.Links = &AlertLinks{
				Alert: &AlertLinkAlert{
					Instance: &AlertInstance{ID: fp},
				},
			}
		}
		if len(dim) > 0 {
			env.Status.Dimensions = &AlertDimensions{Labels: dim}
		}
		return env
	}

	// Three alerts: two share fingerprint, one is unique.
	envs := []alertEnvelope{
		mkEnv("a1", "fp-1", nil),
		mkEnv("a2", "fp-1", nil),
		mkEnv("a3", "fp-2", nil),
	}

	// Default: collapse → 2 rows; first row occurrences=2, second=1.
	got := applyAlertCollapse(envs, false)
	if len(got) != 2 {
		t.Fatalf("collapse: got %d rows, want 2", len(got))
	}
	if got[0].Metadata.Name != "a1" || got[0].Status.Occurrences != 2 {
		t.Errorf("first collapsed row: name=%q occurrences=%d, want a1 / 2", got[0].Metadata.Name, got[0].Status.Occurrences)
	}
	if got[1].Metadata.Name != "a3" || got[1].Status.Occurrences != 1 {
		t.Errorf("second collapsed row: name=%q occurrences=%d, want a3 / 1", got[1].Metadata.Name, got[1].Status.Occurrences)
	}

	// History: no collapse, all occurrences=1.
	hist := applyAlertCollapse(envs, true)
	if len(hist) != 3 {
		t.Fatalf("history: got %d rows, want 3", len(hist))
	}
	for i, env := range hist {
		if env.Status.Occurrences != 1 {
			t.Errorf("history row %d occurrences=%d, want 1", i, env.Status.Occurrences)
		}
	}

	// Fallback: no fingerprint, no dimensions → distinct rows.
	noFP := []alertEnvelope{
		mkEnv("a", "", nil),
		mkEnv("b", "", nil),
	}
	gotNoFP := applyAlertCollapse(noFP, false)
	if len(gotNoFP) != 2 {
		t.Errorf("no-fp fallback: got %d rows, want 2", len(gotNoFP))
	}

	// Fingerprint absent but dimensions present → collapse by dimensions.
	dim1 := map[string]string{"pod": "x"}
	dim2 := map[string]string{"pod": "y"}
	dimEnvs := []alertEnvelope{
		mkEnv("a", "", dim1),
		mkEnv("b", "", dim1),
		mkEnv("c", "", dim2),
	}
	gotDim := applyAlertCollapse(dimEnvs, false)
	if len(gotDim) != 2 {
		t.Errorf("dim fallback: got %d rows, want 2", len(gotDim))
	}
	if gotDim[0].Status.Occurrences != 2 {
		t.Errorf("dim collapse: first row occurrences=%d, want 2", gotDim[0].Status.Occurrences)
	}
}

// TestTruncateRunes covers the table-codec rune-aware truncation: ASCII +
// multibyte content, the single-rune-budget edge case, and the "no
// truncation" sentinel.
func TestTruncateRunes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		s     string
		width int
		want  string
	}{
		{"shorter than width", "abc", 10, "abc"},
		{"exactly width", "abcdef", 6, "abcdef"},
		{"truncate ASCII", "abcdefghij", 5, "abcd…"},
		{"truncate width=1", "abc", 1, "…"},
		{"no truncation w=0", "verylongtitle", 0, "verylongtitle"},
		{"no truncation w<0", "verylongtitle", -1, "verylongtitle"},
		{"empty", "", 5, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := truncateRunes(tc.s, tc.width)
			if got != tc.want {
				t.Errorf("truncateRunes(%q, %d) = %q, want %q", tc.s, tc.width, got, tc.want)
			}
		})
	}
}
