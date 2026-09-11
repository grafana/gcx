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

	t.Run("json output is faithful to custom/global split", func(t *testing.T) {
		cmd := kg.NewThresholdsCommand(thresholdsLoader(server))
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs([]string{"list", "--category", "resource", "-o", "json"})
		require.NoError(t, cmd.Execute())

		require.True(t, strings.HasSuffix(gotPath, "/threshold-rules/resource"), "path: %s", gotPath)

		var got kg.ThresholdRulesDto
		require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
		assert.Len(t, got.CustomThresholds, 2)
		assert.Len(t, got.GlobalThresholds, 1)
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
	assert.JSONEq(t, `{"customThresholds": [], "globalThresholds": []}`, buf.String())
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
