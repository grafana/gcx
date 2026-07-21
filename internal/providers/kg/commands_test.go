package kg_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func scopesHandler(scopes map[string][]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"scopeValues": scopes})
	}
}

func TestScopeFlags_ValidateScopes(t *testing.T) {
	knownScopes := map[string][]string{
		"env":       {"ops-eu-south-0", "ops-eu-north-1", "prod-us-east-1"},
		"site":      {"site-a", "site-b"},
		"namespace": {"default", "monitoring"},
	}

	tests := []struct {
		name         string
		flags        kg.ScopeFlags
		serverScopes map[string][]string
		serverErr    bool
		wantErr      bool
		errContains  string
	}{
		{
			name:         "no scope flags set — skips validation",
			flags:        kg.NewTestScopeFlags("", "", ""),
			serverScopes: knownScopes,
		},
		{
			name:         "exact match — no error",
			flags:        kg.NewTestScopeFlags("ops-eu-south-0", "", ""),
			serverScopes: knownScopes,
		},
		{
			name:         "exact match multiple flags — no error",
			flags:        kg.NewTestScopeFlags("ops-eu-south-0", "", "default"),
			serverScopes: knownScopes,
		},
		{
			name:         "partial match — error with candidates",
			flags:        kg.NewTestScopeFlags("ops", "", ""),
			serverScopes: knownScopes,
			wantErr:      true,
			errContains:  `did you mean one of: ops-eu-north-1, ops-eu-south-0`,
		},
		{
			name:         "no candidates — lists known values",
			flags:        kg.NewTestScopeFlags("totally-unknown", "", ""),
			serverScopes: knownScopes,
			wantErr:      true,
			errContains:  `known env values:`,
		},
		{
			name:  "known values truncated at 10 with hint",
			flags: kg.NewTestScopeFlags("zzz-no-match", "", ""),
			serverScopes: map[string][]string{
				"env": {"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10", "a11"},
			},
			wantErr:     true,
			errContains: "and 1 more — run gcx kg meta scopes",
		},
		{
			name:         "multiple invalid flags — error lists all",
			flags:        kg.NewTestScopeFlags("bad-env", "bad-site", ""),
			serverScopes: knownScopes,
			wantErr:      true,
			errContains:  "--env",
		},
		{
			name:      "API error — best-effort, no error returned",
			flags:     kg.NewTestScopeFlags("anything", "", ""),
			serverErr: true,
		},
		{
			name:         "empty known values for dimension — skips that dimension",
			flags:        kg.NewTestScopeFlags("whatever", "", ""),
			serverScopes: map[string][]string{"env": {}},
		},
		{
			name:         "case-insensitive substring match",
			flags:        kg.NewTestScopeFlags("OPS", "", ""),
			serverScopes: knownScopes,
			wantErr:      true,
			errContains:  "ops-eu",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.serverErr {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				scopesHandler(tt.serverScopes)(w, r)
			}))
			defer server.Close()

			client := newTestClient(t, server)
			err := tt.flags.ValidateScopes(t.Context(), client)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestParseInsightFlag(t *testing.T) {
	tests := []struct {
		in      string
		want    kg.InsightMatcher
		wantErr string
	}{
		{in: "any", want: kg.InsightMatcher{}},
		{in: "ANY", want: kg.InsightMatcher{}},
		{in: " any ", want: kg.InsightMatcher{}},
		{in: "name=Saturation", want: kg.InsightMatcher{Key: "name", Op: "=", Value: "Saturation"}},
		{in: "name=~Sat", want: kg.InsightMatcher{Key: "name", Op: "CONTAINS", Value: "Sat"}},
		{in: "severity=critical", want: kg.InsightMatcher{Key: "severity", Op: "=", Value: "critical"}},
		{in: "Name=Foo", want: kg.InsightMatcher{Key: "name", Op: "=", Value: "Foo"}},
		{in: "severity=~crit", wantErr: "substring match"},
		{in: "scope=foo", wantErr: "unsupported key"},
		{in: "no-equals", wantErr: "expected 'any'"},
		{in: "=value", wantErr: "expected 'any'"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := kg.ParseInsightFlag(tt.in)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFilterByInsightMatchers(t *testing.T) {
	assertion := func(name, sev string) map[string]any {
		return map[string]any{"assertionName": name, "severity": sev}
	}
	group := func(items ...map[string]any) map[string]any {
		arr := make([]any, len(items))
		for i, it := range items {
			arr[i] = it
		}
		return map[string]any{"assertions": arr}
	}

	results := []kg.SearchResult{
		{Name: "a", Assertion: group(assertion("Saturation", "critical"))},
		{Name: "b", Assertion: group(assertion("ErrorRatioBreach", "critical"))},
		{Name: "c", ConnectedAssertion: group(assertion("Saturation", "warning"))},
		{Name: "d", Assertion: group(assertion("Saturation", "info"), assertion("Other", "critical"))},
		{Name: "e"},
	}

	tests := []struct {
		name     string
		matchers []kg.InsightMatcher
		want     []string
	}{
		{
			name:     "no matchers returns all",
			matchers: nil,
			want:     []string{"a", "b", "c", "d", "e"},
		},
		{
			name:     "name filter matches self and connected",
			matchers: []kg.InsightMatcher{{Key: "name", Op: "=", Value: "Saturation"}},
			want:     []string{"a", "c", "d"},
		},
		{
			name:     "name CONTAINS",
			matchers: []kg.InsightMatcher{{Key: "name", Op: "CONTAINS", Value: "sat"}},
			want:     []string{"a", "c", "d"},
		},
		{
			name: "name AND severity must match on same assertion",
			matchers: []kg.InsightMatcher{
				{Key: "name", Op: "=", Value: "Saturation"},
				{Key: "severity", Op: "=", Value: "critical"},
			},
			// d has Saturation/info and Other/critical — neither assertion
			// satisfies both predicates simultaneously, so d is excluded.
			want: []string{"a"},
		},
		{
			name:     "severity only",
			matchers: []kg.InsightMatcher{{Key: "severity", Op: "=", Value: "critical"}},
			want:     []string{"a", "b", "d"},
		},
		{
			name:     "no matches",
			matchers: []kg.InsightMatcher{{Key: "name", Op: "=", Value: "Nope"}},
			want:     nil,
		},
		{
			name:     "wildcard matches anything with at least one assertion",
			matchers: []kg.InsightMatcher{{}},
			// e has no Assertion/ConnectedAssertion, so it's excluded; the rest all have assertions.
			want: []string{"a", "b", "c", "d"},
		},
		{
			name: "wildcard combined with a predicate is a no-op (predicate still applies)",
			matchers: []kg.InsightMatcher{
				{},
				{Key: "name", Op: "=", Value: "ErrorRatioBreach"},
			},
			want: []string{"b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kg.FilterByInsightMatchers(results, tt.matchers)
			var names []string
			for _, r := range got {
				names = append(names, r.Name)
			}
			assert.Equal(t, tt.want, names)
		})
	}
}

// TestKgInsightsSearchRemoved guards against re-introducing the legacy
// `kg insights search` subcommand. It was replaced by `kg entities list --insight`.
func TestKgInsightsSearchRemoved(t *testing.T) {
	cmds := (&kg.KGProvider{}).Commands()
	require.Len(t, cmds, 1)
	for _, c := range cmds[0].Commands() {
		if c.Name() != "insights" {
			continue
		}
		for _, sub := range c.Commands() {
			assert.NotEqual(t, "search", sub.Name(),
				"kg insights search was removed; use kg entities list --insight instead")
		}
	}
}

// TestKgRelabelRulesCommand asserts the relabel-rules command exposes only
// a `get` subcommand with a `--type` flag defaulting to "generated", and
// that the legacy `create` subcommand has been removed.
func TestKgRelabelRulesCommand(t *testing.T) {
	root := (&kg.KGProvider{}).Commands()
	require.Len(t, root, 1)

	var relabel *cobra.Command
	for _, c := range root[0].Commands() {
		if c.Name() == "relabel-rules" {
			relabel = c
			break
		}
	}
	require.NotNil(t, relabel, "relabel-rules command should be registered under kg")

	subNames := make(map[string]*cobra.Command, len(relabel.Commands()))
	for _, sub := range relabel.Commands() {
		subNames[sub.Name()] = sub
	}
	assert.NotContains(t, subNames, "create",
		"`kg relabel-rules create` was dropped — write path stays disabled until the UI feature flag ships")

	getCmd, ok := subNames["get"]
	require.True(t, ok, "`kg relabel-rules get` should be registered")

	typeFlag := getCmd.Flags().Lookup("type")
	require.NotNil(t, typeFlag, "--type flag should exist on `get`")
	assert.Equal(t, "generated", typeFlag.DefValue,
		"--type should default to generated (read-only, safe for users without the UI flag)")

	outputFlag := getCmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag, "--output flag should exist on `get`")
	assert.Equal(t, "table", outputFlag.DefValue,
		"--output should default to table (agent-mode auto-flips to agents)")
}

func TestRelabelRuleTableCodec_Encode(t *testing.T) {
	group := map[string]any{
		"name": "prologue",
		"rules": []any{
			map[string]any{
				"selector":     `{deployment_environment!=""}`,
				"target_label": "asserts_env",
				"replacement":  "$1",
			},
			map[string]any{
				"selector":     `{k8s_namespace_name!=""}`,
				"target_label": "asserts_site",
				"join_labels":  []any{"k8s_namespace_name", "service_name"},
			},
			map[string]any{
				"selector":      `{__name__=~".*request_duration_seconds_.*"}`,
				"target_label":  "asserts_request_context",
				"ranked_choice": []any{"route", "url"},
			},
			map[string]any{
				"selector": `{noise="true"}`,
				"drop":     true,
			},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, (&kg.RelabelRuleTableCodec{}).Encode(&buf, group))
	out := buf.String()
	for _, want := range []string{
		"SELECTOR", "TARGET LABEL", "JOIN LABELS", "RANKED CHOICE", "REPLACEMENT", "DROP",
		`{deployment_environment!=""}`, "asserts_env", "$1",
		`{k8s_namespace_name!=""}`, "asserts_site", "k8s_namespace_name, service_name",
		"asserts_request_context", "route, url",
		`{noise="true"}`, "true",
	} {
		assert.Contains(t, out, want)
	}
}

func TestRelabelRuleTableCodec_RejectsWrongType(t *testing.T) {
	err := (&kg.RelabelRuleTableCodec{}).Encode(&bytes.Buffer{}, "nope")
	require.Error(t, err)
}

func TestRelabelRuleType_IsValid(t *testing.T) {
	for _, valid := range []kg.RelabelRuleType{
		kg.RelabelRuleTypePrologue,
		kg.RelabelRuleTypeEpilogue,
		kg.RelabelRuleTypeGenerated,
	} {
		assert.True(t, valid.IsValid(), "%q should be valid", valid)
	}
	for _, bad := range []kg.RelabelRuleType{"", "PROLOGUE", "default", "bogus"} {
		assert.False(t, bad.IsValid(), "%q should not be valid", bad)
	}
}

func ruleObj(name string, groups []map[string]any) unstructured.Unstructured {
	spec := map[string]any{"name": name}
	if groups != nil {
		spec["groups"] = groups
	}
	return unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kg.ext.grafana.app/v1alpha1",
		"kind":       "Rule",
		"metadata":   map[string]any{"name": name, "namespace": "stack-1"},
		"spec":       spec,
	}}
}

func TestRuleTableCodec_Encode(t *testing.T) {
	objs := []unstructured.Unstructured{
		ruleObj("file-a", []map[string]any{
			{"name": "g1", "rules": []any{
				map[string]any{"alert": "X", "expr": "1"},
				map[string]any{"record": "y", "expr": "1"},
			}},
			{"name": "g2", "rules": []any{
				map[string]any{"record": "z", "expr": "1"},
			}},
		}),
		ruleObj("file-empty", nil),
	}
	var buf bytes.Buffer
	require.NoError(t, (&kg.RuleTableCodec{}).Encode(&buf, objs))
	out := buf.String()
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "GROUPS")
	assert.Contains(t, out, "RULES")
	assert.Contains(t, out, "file-a")
	assert.Contains(t, out, "file-empty")
}

func TestRuleWideTableCodec_Encode(t *testing.T) {
	objs := []unstructured.Unstructured{
		ruleObj("file-a", []map[string]any{
			{"name": "g1", "rules": []any{
				map[string]any{"alert": "X", "expr": "1"},
				map[string]any{"alert": "Y", "expr": "1"},
				map[string]any{"record": "z", "expr": "1"},
			}},
		}),
	}
	var buf bytes.Buffer
	require.NoError(t, (&kg.RuleWideTableCodec{}).Encode(&buf, objs))
	out := buf.String()
	for _, want := range []string{"NAME", "GROUPS", "RULES", "ALERTS", "RECORDING", "file-a"} {
		assert.Contains(t, out, want)
	}
}

func TestRuleTableCodec_RejectsWrongType(t *testing.T) {
	err := (&kg.RuleTableCodec{}).Encode(&bytes.Buffer{}, []string{"nope"})
	require.Error(t, err)
	err = (&kg.RuleWideTableCodec{}).Encode(&bytes.Buffer{}, []string{"nope"})
	require.Error(t, err)
}

// suppressionsDryRunHandler routes the three endpoints a dry-run touches. It
// records whether the single-config write endpoint is ever hit so tests can
// assert dry-run never writes.
func suppressionsDryRunHandler(t *testing.T, validateStatus int, validateBody string, remote kg.Suppressions, writeHit *bool) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "disabled-alerts-validate"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(validateStatus)
			if validateBody != "" {
				_, _ = w.Write([]byte(validateBody))
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "disabled-alerts"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(remote)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "disabled-alert"):
			*writeHit = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

const localSuppressionYAML = `disabledAlertConfigs:
  - name: my-suppression
    matchLabels:
      alertname: ErrorRatioBreach
`

func TestSuppressionsPush_DryRun_Invalid(t *testing.T) {
	writeHit := false
	body := `{"message":"Invalid disabled alert configuration file","subErrors":[` +
		`{"field":"disabledAlertConfigs[0].matchLabels","message":"Missing Assertion"}]}`
	server := httptest.NewServer(suppressionsDryRunHandler(t, http.StatusUnprocessableEntity, body, kg.Suppressions{}, &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewSuppressionsCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"push", "-f", "-", "--dry-run"})
	cmd.SetIn(bytes.NewBufferString(localSuppressionYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Missing Assertion")
	assert.Empty(t, stdout.String(), "no diff on stdout for invalid input")
	assert.False(t, writeHit, "dry-run must not write")
}

func TestSuppressionsPush_DryRun_ValidWithChanges(t *testing.T) {
	writeHit := false
	// Remote has a different matchLabels value, so a diff is produced.
	remote := kg.Suppressions{DisabledAlertConfigs: []kg.Suppression{
		{Name: "my-suppression", MatchLabels: map[string]string{"alertname": "SomethingElse"}},
	}}
	server := httptest.NewServer(suppressionsDryRunHandler(t, http.StatusOK, "", remote, &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewSuppressionsCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"push", "-f", "-", "--dry-run", "-o", "text"})
	cmd.SetIn(bytes.NewBufferString(localSuppressionYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())
	// Banner on stderr, diff body on stdout.
	assert.Contains(t, stderr.String(), "[dry-run]")
	assert.Contains(t, stderr.String(), "change(s)")
	assert.Contains(t, stdout.String(), "--- remote")
	assert.Contains(t, stdout.String(), "+++ local")
	assert.Contains(t, stdout.String(), "ErrorRatioBreach")
	assert.False(t, writeHit, "dry-run must not write")
}

func TestSuppressionsPush_DryRun_JSONOutput(t *testing.T) {
	writeHit := false
	// Remote has one differing entry (modify) and, alongside the local-only
	// entry (add), a remote-only entry that must be ignored (scoped to inputs).
	remote := kg.Suppressions{DisabledAlertConfigs: []kg.Suppression{
		{Name: "my-suppression", MatchLabels: map[string]string{"alertname": "SomethingElse"}},
		{Name: "remote-only", MatchLabels: map[string]string{"alertname": "Foo"}},
	}}
	server := httptest.NewServer(suppressionsDryRunHandler(t, http.StatusOK, "", remote, &writeHit))
	defer server.Close()

	const localYAML = `disabledAlertConfigs:
  - name: my-suppression
    matchLabels:
      alertname: ErrorRatioBreach
  - name: brand-new
    matchLabels:
      alertname: HighLatency
`
	var stdout, stderr bytes.Buffer
	cmd := kg.NewSuppressionsCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"push", "-f", "-", "--dry-run", "-o", "json"})
	cmd.SetIn(bytes.NewBufferString(localYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())

	var got kg.SuppressionsDryRunResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	assert.True(t, got.Valid)
	assert.True(t, got.Changed, "add/modify present means push would change state")
	// Scoped to the file's entries: modify (my-suppression), add (brand-new).
	// The remote-only entry is ignored entirely — push never deletes.
	actions := map[string]string{}
	for _, c := range got.Changes {
		actions[c.Name] = c.Action
	}
	assert.Equal(t, "modify", actions["my-suppression"])
	assert.Equal(t, "add", actions["brand-new"])
	assert.NotContains(t, actions, "remote-only", "remote-only entries are not reported (scoped to inputs)")
	assert.Len(t, got.Changes, 2)
	assert.NotEmpty(t, got.Diff, "structured result carries the unified diff too")
	assert.NotContains(t, got.Diff, "remote-only", "diff is scoped to the input entries")
	assert.False(t, writeHit, "dry-run must not write")
}

func TestSuppressionsPush_DryRun_ValidNoChanges(t *testing.T) {
	writeHit := false
	remote := kg.Suppressions{DisabledAlertConfigs: []kg.Suppression{
		{Name: "my-suppression", MatchLabels: map[string]string{"alertname": "ErrorRatioBreach"}},
	}}
	server := httptest.NewServer(suppressionsDryRunHandler(t, http.StatusOK, "", remote, &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewSuppressionsCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"push", "-f", "-", "--dry-run", "-o", "text"})
	cmd.SetIn(bytes.NewBufferString(localSuppressionYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, stderr.String(), "no changes")
	assert.Empty(t, stdout.String(), "no diff on stdout when there are no changes")
	assert.False(t, writeHit, "dry-run must not write")
}

// When the file's entries all match remote but remote has extra entries, the
// dry-run reports no changes: remote-only entries are ignored (scoped to inputs)
// and produce neither a change nor a diff.
func TestSuppressionsPush_DryRun_RemoteOnlyIgnored(t *testing.T) {
	writeHit := false
	remote := kg.Suppressions{DisabledAlertConfigs: []kg.Suppression{
		{Name: "my-suppression", MatchLabels: map[string]string{"alertname": "ErrorRatioBreach"}},
		{Name: "remote-only", MatchLabels: map[string]string{"alertname": "Foo"}},
	}}
	server := httptest.NewServer(suppressionsDryRunHandler(t, http.StatusOK, "", remote, &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewSuppressionsCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"push", "-f", "-", "--dry-run", "-o", "json"})
	// localSuppressionYAML contains only my-suppression, identical to remote.
	cmd.SetIn(bytes.NewBufferString(localSuppressionYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())

	var got kg.SuppressionsDryRunResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	assert.True(t, got.Valid)
	assert.False(t, got.Changed, "push applies nothing when file entries match remote")
	assert.Empty(t, got.Changes, "remote-only entries are not reported")
	assert.Empty(t, got.Diff, "no diff when the input entries match remote")
	// changes serializes as [] (not null) when empty.
	assert.Contains(t, stdout.String(), `"changes": []`)
	assert.Contains(t, stderr.String(), "no changes")
	assert.False(t, writeHit, "dry-run must not write")
}

// System-managed fields (managedBy) are populated by the backend, not the input
// file, so an entry whose user fields match remote must report no change even
// when remote carries a managedBy the file omits — otherwise the diff would show
// a spurious removal for a field push does not touch.
func TestSuppressionsPush_DryRun_SystemFieldsIgnored(t *testing.T) {
	writeHit := false
	remote := kg.Suppressions{DisabledAlertConfigs: []kg.Suppression{
		{Name: "my-suppression", MatchLabels: map[string]string{"alertname": "ErrorRatioBreach"}, ManagedBy: "terraform"},
	}}
	server := httptest.NewServer(suppressionsDryRunHandler(t, http.StatusOK, "", remote, &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewSuppressionsCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"push", "-f", "-", "--dry-run", "-o", "json"})
	// localSuppressionYAML has the same name/matchLabels but no managedBy.
	cmd.SetIn(bytes.NewBufferString(localSuppressionYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())

	var got kg.SuppressionsDryRunResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	assert.True(t, got.Valid)
	assert.False(t, got.Changed, "managedBy is a system field; user fields are unchanged")
	assert.Empty(t, got.Changes)
	assert.Empty(t, got.Diff, "system-only differences must not appear in the diff")
	assert.NotContains(t, stdout.String(), "managedBy")
	assert.False(t, writeHit, "dry-run must not write")
}
