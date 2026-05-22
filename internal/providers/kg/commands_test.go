package kg_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
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
