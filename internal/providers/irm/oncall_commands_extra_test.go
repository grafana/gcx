//nolint:testpackage // white-box tests require access to unexported IRM types and helpers
package irm

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/pflag"
)

// TestStringifyAlertGroupListFilters covers the round-17 filter-summary
// stringifier used by the post-result hint on `alert-groups list`.
//
// Coverage:
//   - default-only (no flags) → fixed phrase exposing implicit exclusions,
//   - explicit flags → ordered, comma-joined description prefixed with
//     "default + " when implicit exclusions are still in effect,
//   - --all alone → "all" (caller is expected to suppress emission entirely
//     in this case via alertGroupListHasExplicitFilter — but the stringifier
//     still produces a sensible value),
//   - explicit --state suppresses the "default + " prefix (status-default no
//     longer applies),
//   - very long combinations collapse to the "<N filters>" fallback.
func TestStringifyAlertGroupListFilters(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   *alertGroupListOpts
		want string
	}{
		{
			name: "default only",
			in:   &alertGroupListOpts{},
			want: "default (excludes resolved + child groups)",
		},
		{
			name: "all alone",
			in:   &alertGroupListOpts{All: true},
			want: "all",
		},
		{
			name: "team only — defaults still in effect",
			in:   &alertGroupListOpts{Teams: []string{"prod-sre"}},
			want: "default + team=prod-sre",
		},
		{
			name: "explicit state — defaults dropped",
			in:   &alertGroupListOpts{States: []string{"firing"}},
			want: "status=firing",
		},
		{
			name: "max-age augments default",
			in:   &alertGroupListOpts{MaxAge: "24h"},
			want: "default + max-age=24h",
		},
		{
			name: "include-child-groups + team",
			in: &alertGroupListOpts{
				Teams:              []string{"prod-sre"},
				IncludeChildGroups: true,
			},
			want: "team=prod-sre, include-child-groups",
		},
		{
			name: "many filters collapse to count",
			in: &alertGroupListOpts{
				States:             []string{"firing", "acknowledged", "silenced"},
				Teams:              []string{"team-with-very-long-identifier-1", "team-with-very-long-identifier-2"},
				Integrations:       []string{"integration-with-a-long-name"},
				MaxAge:             "168h",
				Mine:               true,
				WithResolutionNote: true,
				HasRelatedIncident: true,
			},
			want: "7 filters",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := stringifyAlertGroupListFilters(tc.in)
			if got != tc.want {
				t.Errorf("stringifyAlertGroupListFilters() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAlertGroupListHasExplicitFilter — silent-on-`--all` decision predicate.
func TestAlertGroupListHasExplicitFilter(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   *alertGroupListOpts
		want bool
	}{
		{"empty", &alertGroupListOpts{}, false},
		{"all alone", &alertGroupListOpts{All: true}, false},
		{"team", &alertGroupListOpts{Teams: []string{"x"}}, true},
		{"max-age", &alertGroupListOpts{MaxAge: "1h"}, true},
		{"mine", &alertGroupListOpts{Mine: true}, true},
		{"include-child-groups", &alertGroupListOpts{IncludeChildGroups: true}, true},
		{"all + team", &alertGroupListOpts{All: true, Teams: []string{"x"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := alertGroupListHasExplicitFilter(tc.in); got != tc.want {
				t.Errorf("alertGroupListHasExplicitFilter() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFirstAlertRuleUID — first-occurrence-wins extraction used by the
// list-alerts rule-pivot hints.
func TestFirstAlertRuleUID(t *testing.T) {
	t.Parallel()
	mkEnv := func(uid string) alertEnvelope {
		var links *AlertLinks
		if uid != "" {
			links = &AlertLinks{
				Alert: &AlertLinkAlert{
					Rule: &AlertRule{UID: uid},
				},
			}
		}
		return alertEnvelope{
			Status: AlertStatus{Links: links},
		}
	}

	cases := []struct {
		name string
		in   []alertEnvelope
		want string
	}{
		{"empty slice", nil, ""},
		{"all empty", []alertEnvelope{mkEnv(""), mkEnv("")}, ""},
		{"single", []alertEnvelope{mkEnv("rule-1")}, "rule-1"},
		{"multi same", []alertEnvelope{mkEnv("rule-1"), mkEnv("rule-1")}, "rule-1"},
		{"multi differ — first wins", []alertEnvelope{mkEnv("rule-a"), mkEnv("rule-b")}, "rule-a"},
		{"empty then non-empty", []alertEnvelope{mkEnv(""), mkEnv("rule-c")}, "rule-c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := firstAlertRuleUID(tc.in)
			if got != tc.want {
				t.Errorf("firstAlertRuleUID() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFormatTeamCell — TEAM column rendering with ID preservation across
// width regimes (round 2 brief). Team IDs MUST stay intact when the cell
// overflows budget so they remain copy-pasteable for `--team-id=...`.
func TestFormatTeamCell(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		teamName string
		teamID   string
		budget   int
		want     string
	}{
		// Empty inputs.
		{"both empty", "", "", 0, "-"},
		{"name empty, id only", "", "T123", 0, "(T123)"},
		{"id empty, name only", "team-x", "", 0, "team-x"},
		{"id empty, name truncated to budget", "longteamname", "", 6, "longt…"},
		// Budget=0 — no truncation, render verbatim.
		{"no budget renders verbatim", "loki_Ingest-Query", "TIRSPU012345", 0, "loki_Ingest-Query (TIRSPU012345)"},
		// Negative budget treated like 0 (no truncation).
		{"negative budget no truncation", "team-a", "T1", -1, "team-a (T1)"},
		// Regime 1: full string fits in budget → verbatim.
		{"fits exactly", "team-a", "T1", 11, "team-a (T1)"},
		{"fits with headroom", "team-a", "T1", 100, "team-a (T1)"},
		// Regime 2: budget tight → truncate name, keep full id.
		{
			name: "name truncated id intact",
			// "loki_Ingest-Query (TIRSPU012345)" = 32 runes total.
			// Budget = 28; need to drop 4 runes from name segment.
			// nameBudget = 28 - 14 (wrapper "(TIRSPU012345)") - 1 (space) = 13.
			// truncateRunes("loki_Ingest-Query", 13) -> "loki_Ingest-…" (12 runes + …).
			teamName: "loki_Ingest-Query",
			teamID:   "TIRSPU012345",
			budget:   28,
			want:     "loki_Ingest-… (TIRSPU012345)",
		},
		{
			name:     "name barely truncated — id intact",
			teamName: "very-long-team-name",
			teamID:   "T123",
			// "very-long-team-name (T123)" = 26 runes total.
			// Budget=20: nameBudget = 20 - 6 - 1 = 13 → "very-long-te…" (13 runes incl …).
			budget: 20,
			want:   "very-long-te… (T123)",
		},
		// Regime 3: budget so tight that even `… (<id>)` doesn't fit
		// → fall back to `(<id>)` alone.
		{
			name:     "tight budget — id-only fallback",
			teamName: "long",
			teamID:   "T1234567",
			// `(T1234567)` = 10 runes; smaller budget falls back.
			budget: 8,
			want:   "(T1234567)",
		},
		{
			name:     "very tight budget — id preserved",
			teamName: "anything",
			teamID:   "TIRSPU012345",
			budget:   5,
			want:     "(TIRSPU012345)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatTeamCell(tc.teamName, tc.teamID, tc.budget)
			if got != tc.want {
				t.Errorf("formatTeamCell(%q, %q, %d) = %q, want %q", tc.teamName, tc.teamID, tc.budget, got, tc.want)
			}
		})
	}
}

// TestFormatTeamCellPreservesID is a property-style assertion: across all
// truncation regimes (except the empty-id case), the rendered cell MUST
// contain the full team ID literally. This guards against future budget
// math regressions silently chopping into the ID.
func TestFormatTeamCellPreservesID(t *testing.T) {
	t.Parallel()
	const id = "TIRSPU012345"
	const name = "this-is-a-fairly-long-team-name-for-overflow-testing"
	for budget := range 80 {
		got := formatTeamCell(name, id, budget)
		if !strings.Contains(got, id) {
			t.Errorf("budget=%d: cell %q dropped id %q", budget, got, id)
		}
	}

	// Regime-2 concrete case: budget allows `<truncated-name>… (<id>)` but
	// not the full string. The ID must be intact and the cell must carry an
	// ellipsis indicating the name was truncated.
	//
	// full = "this-is-a-fairly-long-team-name (TIRSPU012345)" (47 runes)
	// budget = 25 → nameBudget = 25 - 14 (wrapper "(TIRSPU012345)") - 1 = 10
	// truncateRunes("this-is-a-fairly-long-team-name", 10) → "this-is-a…"
	// result = "this-is-a… (TIRSPU012345)"
	regime2 := formatTeamCell("this-is-a-fairly-long-team-name", "TIRSPU012345", 25)
	if !strings.Contains(regime2, "TIRSPU012345") {
		t.Errorf("regime-2 budget=25: team ID must be preserved; got %q", regime2)
	}
	if !strings.Contains(regime2, "…") {
		t.Errorf("regime-2 budget=25: cell must contain ellipsis to indicate name truncation; got %q", regime2)
	}
}

// TestAlertRuleCell — wide-mode RULE column URL-over-UID precedence.
// (Default columns dropped DASHBOARD; RULE is wide-only — see ADR §6.)
func TestAlertRuleCell(t *testing.T) {
	t.Parallel()
	mk := func(uid, urlStr string) alertEnvelope {
		return alertEnvelope{
			Status: AlertStatus{
				Links: &AlertLinks{
					Alert: &AlertLinkAlert{
						Rule: &AlertRule{UID: uid, URL: urlStr},
					},
				},
			},
		}
	}
	cases := []struct {
		name string
		in   alertEnvelope
		want string
	}{
		{"empty links", alertEnvelope{}, "-"},
		{"uid only", mk("uid-1", ""), "uid-1"},
		{"url preferred", mk("uid-1", "https://example.com/rule"), "https://example.com/rule"},
		{"both empty", mk("", ""), "-"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := alertRuleCell(tc.in)
			if got != tc.want {
				t.Errorf("alertRuleCell() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAlertGroupItemsEnvelope_JSONShape pins the list envelope
// contract for `alert-groups list` / `list-alerts`: stdout MUST be
// `{"items": [...]}` — never a bare array, never null. Empty result is
// `{"items": []}` (not `{"items": null}`).
func TestAlertGroupItemsEnvelope_JSONShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		env  any
		want string
	}{
		{
			"alert-groups list empty",
			alertGroupItemsEnvelope{Items: []alertGroupEnvelope{}},
			`{"items":[]}`,
		},
		{
			"alert-groups list non-empty",
			alertGroupItemsEnvelope{Items: []alertGroupEnvelope{
				{APIVersion: APIVersion, Kind: "AlertGroup", Metadata: k8sMetadata{Name: "I1"}},
			}},
			`"items":[`,
		},
		{
			"list-alerts empty",
			alertItemsEnvelope{Items: []alertEnvelope{}},
			`{"items":[]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, err := json.Marshal(tc.env)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got := string(b)
			if !strings.Contains(got, tc.want) {
				t.Errorf("missing fragment %q in %s", tc.want, got)
			}
			// Negative: must not be a bare array or null.
			if strings.HasPrefix(got, "[") || got == "null" {
				t.Errorf("top-level must be an object with `items`, got %s", got)
			}
		})
	}
}

// TestAlertGroupTableCodec_AcceptsSingleAndItemsEnvelope verifies that the
// single-row `get` path and the items envelope `list` path
// both round-trip through the same alertGroupTableCodec without errors.
func TestAlertGroupTableCodec_AcceptsSingleAndItemsEnvelope(t *testing.T) {
	t.Parallel()
	codec := &alertGroupTableCodec{}
	wide := &alertGroupTableCodec{Wide: true}

	one := alertGroupEnvelope{
		APIVersion: APIVersion,
		Kind:       "AlertGroup",
		Metadata:   k8sMetadata{Name: "I1"},
		Status:     AlertGroupStatus{State: "firing", Title: "ex"},
	}
	wrapped := alertGroupItemsEnvelope{Items: []alertGroupEnvelope{one, one}}

	cases := []struct {
		name string
		v    any
	}{
		{"single envelope (get path)", one},
		{"items envelope (list path)", wrapped},
		{"bare slice (back-compat)", []alertGroupEnvelope{one}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := codec.Encode(&buf, tc.v); err != nil {
				t.Errorf("default codec encode failed: %v", err)
			}
			if buf.Len() == 0 {
				t.Errorf("default codec produced empty output for %T", tc.v)
			}
			buf.Reset()
			if err := wide.Encode(&buf, tc.v); err != nil {
				t.Errorf("wide codec encode failed: %v", err)
			}
			if buf.Len() == 0 {
				t.Errorf("wide codec produced empty output for %T", tc.v)
			}
		})
	}
}

// TestAlertTableCodec_AcceptsItemsEnvelope verifies the list envelope
// is rendered by the list-alerts table codec.
func TestAlertTableCodec_AcceptsItemsEnvelope(t *testing.T) {
	t.Parallel()
	codec := &alertTableCodec{}
	envs := []alertEnvelope{{
		APIVersion: APIVersion,
		Kind:       "Alert",
		Metadata:   k8sMetadata{Name: "A1"},
		Status:     AlertStatus{State: "firing"},
	}}
	var buf bytes.Buffer
	if err := codec.Encode(&buf, alertItemsEnvelope{Items: envs}); err != nil {
		t.Fatalf("encode items envelope: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty output for items envelope")
	}
	buf.Reset()
	// Back-compat: bare slice still works.
	if err := codec.Encode(&buf, envs); err != nil {
		t.Fatalf("encode bare slice: %v", err)
	}
}

// TestAlertGroupGetRichOpts_DefaultsToTable pins the codec default for
// `alert-groups get`: the default format MUST be "table" (NOT yaml),
// with the table codec registered — uniform with `list` and `list-alerts`.
func TestAlertGroupGetRichOpts_DefaultsToTable(t *testing.T) {
	resetAgentMode(t)
	opts := &alertGroupGetRichOpts{}
	flagset := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.setup(flagset)
	// The output flag default reflects what DefaultFormat() seeded.
	out := flagset.Lookup("output")
	if out == nil {
		t.Fatal("expected --output flag to be bound by setup()")
	}
	if got := out.DefValue; got != "table" {
		t.Errorf("alert-groups get default codec want %q, got %q", "table", got)
	}
}

// TestEmitWarnEmitNote_Format pins the output contract for the
// centralised diagnostic helpers: TTY plain-text with the prefixed class
// name; agent mode JSONL with typed `class` field.
func TestEmitWarnEmitNote_Format(t *testing.T) {
	t.Run("tty", func(t *testing.T) {
		resetAgentMode(t)
		var buf bytes.Buffer
		emitWarn(&buf, "near the cap")
		emitNote(&buf, "filter is in effect")

		got := buf.String()
		if !strings.Contains(got, "warn: near the cap") {
			t.Errorf("TTY emitWarn must render `warn: <summary>`; got %q", got)
		}
		if !strings.Contains(got, "note: filter is in effect") {
			t.Errorf("TTY emitNote must render `note: <summary>`; got %q", got)
		}
	})

	t.Run("agent mode JSONL", func(t *testing.T) {
		t.Setenv("GCX_AGENT_MODE", "true")
		agent.ResetForTesting()
		defer agent.ResetForTesting()

		var warnBuf, noteBuf bytes.Buffer
		emitWarn(&warnBuf, "near the cap")
		emitNote(&noteBuf, "rate limited")

		// Parse warn JSON.
		var warnEv map[string]any
		if err := json.Unmarshal(warnBuf.Bytes(), &warnEv); err != nil {
			t.Fatalf("emitWarn agent mode: expected valid JSON; got %q: %v", warnBuf.String(), err)
		}
		if got := warnEv["class"]; got != "warning" {
			t.Errorf("emitWarn agent mode: class want %q, got %v", "warning", got)
		}
		if got := warnEv["summary"]; got != "near the cap" {
			t.Errorf("emitWarn agent mode: summary want %q, got %v", "near the cap", got)
		}

		// Parse note JSON.
		var noteEv map[string]any
		if err := json.Unmarshal(noteBuf.Bytes(), &noteEv); err != nil {
			t.Fatalf("emitNote agent mode: expected valid JSON; got %q: %v", noteBuf.String(), err)
		}
		if got := noteEv["class"]; got != "note" {
			t.Errorf("emitNote agent mode: class want %q, got %v", "note", got)
		}
		if got := noteEv["summary"]; got != "rate limited" {
			t.Errorf("emitNote agent mode: summary want %q, got %v", "rate limited", got)
		}
	})
}
