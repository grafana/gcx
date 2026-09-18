package kg_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func thresholdsLoader(server *httptest.Server) *kg.FakeWriteLoader {
	return &kg.FakeWriteLoader{
		Cfg: config.NamespacedRESTConfig{
			Config:    rest.Config{Host: server.URL},
			Namespace: "stack-123",
		},
	}
}

func TestClient_GetThresholds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "v1/config/threshold-rules")
		assert.NotContains(t, r.URL.Path, "threshold-rules/") // whole-config: no category segment
		writeJSON(w, map[string]any{
			"name": "custom_thresholds",
			"groups": []any{map[string]any{
				"name": "custom_thresholds",
				"rules": []any{map[string]any{
					"record": "asserts:latency:average:threshold",
					"expr":   "0.1",
					"labels": map[string]string{"job": "integrations/db-o11y"},
				}},
			}},
		})
	}))
	defer server.Close()

	rule, err := newTestClient(t, server).GetThresholds(t.Context())
	require.NoError(t, err)
	require.Equal(t, "custom_thresholds", rule.Name)
	require.Len(t, rule.Groups, 1)
	require.Len(t, rule.Groups[0].Rules, 1)
	assert.Equal(t, "asserts:latency:average:threshold", rule.Groups[0].Rules[0].Record)
	assert.Equal(t, "0.1", rule.Groups[0].Rules[0].Expr)
}

func TestClient_GetThresholdsByCategory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.True(t, strings.HasSuffix(r.URL.Path, "/threshold-rules/request"), "path: %s", r.URL.Path)
		writeJSON(w, kg.ThresholdRulesDto{
			CustomThresholds: []kg.Threshold{{
				Active: false,
				Record: "asserts:latency:average:threshold",
				Expr:   "0.1",
				Labels: map[string]string{"asserts_request_type": "statements"},
			}},
			GlobalThresholds: []kg.Threshold{{
				Active: true,
				Record: "asserts:resource:rate:threshold_by_stddev",
				Expr:   "2",
				// global thresholds frequently omit labels
			}},
		})
	}))
	defer server.Close()

	dto, err := newTestClient(t, server).GetThresholdsByCategory(t.Context(), "request")
	require.NoError(t, err)
	require.Len(t, dto.CustomThresholds, 1)
	require.Len(t, dto.GlobalThresholds, 1)
	assert.Equal(t, "statements", dto.CustomThresholds[0].Labels["asserts_request_type"])
	assert.True(t, dto.GlobalThresholds[0].Active)
	assert.Empty(t, dto.GlobalThresholds[0].Labels)
}

// The command layer validates --category, but the client method is exported and
// the writes follow-up takes the same category-shaped input, so the escaping has
// to hold at the client boundary too: an unescaped "/" would resolve to a
// different config endpoint entirely.
func TestClient_GetThresholdsByCategory_EscapesPathSegment(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		writeJSON(w, kg.ThresholdRulesDto{})
	}))
	defer server.Close()

	_, err := newTestClient(t, server).GetThresholdsByCategory(t.Context(), "../model-rules")
	require.NoError(t, err)
	assert.Contains(t, gotPath, "threshold-rules/..%2Fmodel-rules")
	assert.NotContains(t, gotPath, "config/model-rules")
}

func TestThresholdsGetCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"name": "custom_thresholds",
			"groups": []any{map[string]any{
				"name":  "custom_thresholds",
				"rules": []any{map[string]any{"record": "asserts:latency:average:threshold", "expr": "0.1"}},
			}},
		})
	}))
	defer server.Close()

	t.Run("default output is a table", func(t *testing.T) {
		pinHumanMode(t)
		cmd := kg.NewThresholdsCommand(thresholdsLoader(server))
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs([]string{"get"})
		require.NoError(t, cmd.Execute())

		out := buf.String()
		assert.Contains(t, out, "NAME")
		assert.Contains(t, out, "GROUPS")
		assert.Contains(t, out, "RULES")
		assert.Contains(t, out, "custom_thresholds")
	})

	t.Run("yaml keeps the resource envelope at the document root", func(t *testing.T) {
		cmd := kg.NewThresholdsCommand(thresholdsLoader(server))
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs([]string{"get", "-o", "yaml"})
		require.NoError(t, cmd.Execute())

		out := buf.String()
		assert.Contains(t, out, "custom_thresholds")
		assert.Contains(t, out, "asserts:latency:average:threshold")
		// The K8s envelope must be the top-level document. unstructured.Unstructured
		// implements MarshalJSON on the pointer receiver, so encoding a bare value
		// silently nests everything under an "Object" key — assert on the document
		// root, because a Contains check on "kind: Rule" passes either way.
		assert.True(t, strings.HasPrefix(out, "apiVersion: "), "envelope must be top-level, got:\n%s", out)
		assert.NotContains(t, out, "Object:")
	})
}

func TestThresholdsListCommand(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(w, kg.ThresholdRulesDto{
			CustomThresholds: []kg.Threshold{{Record: "custom-b", Expr: "1"}, {Record: "custom-a", Expr: "2"}},
			GlobalThresholds: []kg.Threshold{{Record: "global-a", Expr: "3", Active: true}},
		})
	}))
	defer server.Close()

	t.Run("json output is an items envelope with scope", func(t *testing.T) {
		cmd := kg.NewThresholdsCommand(thresholdsLoader(server))
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs([]string{"list", "--category", "resource", "-o", "json"})
		require.NoError(t, cmd.Execute())

		require.True(t, strings.HasSuffix(gotPath, "/threshold-rules/resource"), "path: %s", gotPath)

		var got struct {
			Items []struct {
				Scope  string            `json:"scope"`
				Record string            `json:"record"`
				Expr   string            `json:"expr"`
				Active bool              `json:"active"`
				Labels map[string]string `json:"labels"`
			} `json:"items"`
		}
		require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
		require.Len(t, got.Items, 3)
		assert.Equal(t, "custom", got.Items[0].Scope)
		assert.Equal(t, "custom-a", got.Items[0].Record)
		assert.Equal(t, "custom", got.Items[1].Scope)
		assert.Equal(t, "custom-b", got.Items[1].Record)
		assert.Equal(t, "global", got.Items[2].Scope)
		assert.Equal(t, "global-a", got.Items[2].Record)
		assert.True(t, got.Items[2].Active)
	})

	t.Run("json field selection applies to each threshold", func(t *testing.T) {
		cmd := kg.NewThresholdsCommand(thresholdsLoader(server))
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs([]string{"list", "--category", "resource", "--json", "record"})
		require.NoError(t, cmd.Execute())

		var got struct {
			Items []map[string]any `json:"items"`
		}
		require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
		require.Len(t, got.Items, 3)
		assert.Equal(t, map[string]any{"record": "custom-a"}, got.Items[0])
		assert.Equal(t, map[string]any{"record": "custom-b"}, got.Items[1])
		assert.Equal(t, map[string]any{"record": "global-a"}, got.Items[2])
	})

	t.Run("agent spill counts and previews threshold items", func(t *testing.T) {
		setAgentMode(t)
		t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
		t.Setenv("TMPDIR", t.TempDir())

		cmd := kg.NewThresholdsCommand(thresholdsLoader(server))
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"list", "--category", "resource"})
		require.NoError(t, cmd.Execute())

		var summary map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &summary))
		assert.EqualValues(t, 3, summary["total_items"])
		preview, ok := summary["preview_sample"].([]any)
		require.True(t, ok)
		require.Len(t, preview, 3)
		first, ok := preview[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "custom-a", first["record"])
	})

	t.Run("table output flattens custom-first, sorted by record", func(t *testing.T) {
		cmd := kg.NewThresholdsCommand(thresholdsLoader(server))
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs([]string{"list", "--category", "request", "-o", "table"})
		require.NoError(t, cmd.Execute())

		out := buf.String()
		// custom-a should precede custom-b (sorted), and both precede global-a (scope order).
		aIdx := strings.Index(out, "custom-a")
		bIdx := strings.Index(out, "custom-b")
		gIdx := strings.Index(out, "global-a")
		require.NotEqual(t, -1, aIdx)
		require.NotEqual(t, -1, gIdx)
		assert.Less(t, aIdx, bIdx, "custom-a before custom-b")
		assert.Less(t, bIdx, gIdx, "custom before global")
	})
}

func TestThresholdsListCommand_ZeroResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// No threshold keys: the decoded slices are nil, which must still
		// serialize as [] in machine formats, never null.
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	cmd := kg.NewThresholdsCommand(thresholdsLoader(server))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"list", "--category", "request", "-o", "json"})
	require.NoError(t, cmd.Execute())
	assert.JSONEq(t, `{"items": []}`, buf.String())
}

func TestThresholdsListCommand_CategoryValidation(t *testing.T) {
	hit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		writeJSON(w, kg.ThresholdRulesDto{})
	}))
	defer server.Close()

	tests := []struct {
		name        string
		args        []string
		errContains string
	}{
		{"missing category", []string{"list"}, "--category is required"},
		{"invalid category", []string{"list", "--category", "health"}, "invalid --category"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := kg.NewThresholdsCommand(thresholdsLoader(server))
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
			assert.False(t, hit, "server must not be called when validation fails")
		})
	}
}
