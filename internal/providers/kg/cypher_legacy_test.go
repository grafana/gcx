package kg_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// Synthetic data pins the legacy entity/edge contract independently of graph rows.
const legacyCypherResult = `{"entities":[{"type":"Service","name":"checkout","scope":{"namespace":"demo"},"properties":{"version":"v1"},"insights":[]}],"edges":[{"type":"CALLS","sourceName":"checkout","sourceType":"Service","destinationName":"database","destinationType":"Service"}],"pageNum":2,"lastPage":false}`

func TestLegacyCypherCommandCompatibility(t *testing.T) {
	for _, tt := range []struct {
		name  string
		flags []string
		agent bool
		want  string
	}{
		{name: "json", flags: []string{"-o", "json"}, want: `"pageNum": 2`},
		{name: "yaml", flags: []string{"-o", "yaml"}, want: "pageNum: 2"},
		{name: "table", flags: []string{"-o", "table"}, want: "namespace=demo"},
		{name: "selected fields", flags: []string{"--json", "entities,edges,pageNum,lastPage"}, want: `"lastPage": false`},
		{name: "jq", flags: []string{"--jq", ".entities[0].name"}, want: "checkout"},
		{name: "agent", agent: true, want: `"pageNum": 2`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pinHumanMode(t)
			forceNoColor(t)
			if tt.agent {
				setAgentMode(t)
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/search/cypher", r.URL.Path)
				var body map[string]any
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
					return
				}
				assert.Equal(t, map[string]any{
					"cypherQuery":  "MATCH (s:Service) RETURN s",
					"timeCriteria": map[string]any{"start": float64(1757000000000), "end": float64(1757003600000)},
					"pageNum":      float64(2),
					"withInsights": true,
				}, body)
				_, _ = fmt.Fprint(w, legacyCypherResult)
			}))
			defer server.Close()
			loader := &kg.FakeWriteLoader{Cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}
			// No LIMIT: legacy callers must not inherit the new endpoint's validation.
			args := append([]string{"MATCH (s:Service) RETURN s", "--from", "1757000000", "--to", "1757003600", "--page", "2", "--insights-only"}, tt.flags...)
			out, stderr, err := runKgCommand(t, func() *cobra.Command { return kg.NewCypherCommand(loader) }, args, "")
			require.NoError(t, err)
			assert.Equal(t, 1, calls)
			assert.Contains(t, out, tt.want)
			assert.NotContains(t, out, "deprecated")
			assert.Contains(t, stderr, "gcx kg entities query is deprecated")
			assert.Contains(t, stderr, "gcx kg graph query")
			assert.Contains(t, stderr, "supported through v1.x")
			if tt.agent || tt.name == "json" || tt.name == "selected fields" {
				decodeSingleJSON(t, []byte(out))
				assert.Contains(t, out, `"entities"`)
				assert.Contains(t, out, `"edges"`)
				assert.Contains(t, out, `"lastPage": false`)
				assert.NotContains(t, out, `"columns"`)
				assert.NotContains(t, out, `"rows"`)
			}
		})
	}
}

func TestLegacyCypherCommandDefaults(t *testing.T) {
	for _, tt := range []struct {
		name     string
		flags    []string
		duration time.Duration
	}{
		{name: "last hour", duration: time.Hour},
		{name: "explicit since", flags: []string{"--since", "2h"}, duration: 2 * time.Hour},
		{name: "explicit empty since still defaults", flags: []string{"--since="}, duration: time.Hour},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pinHumanMode(t)
			before := time.Now().UnixMilli()
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var req kg.CypherSearchRequest
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) || !assert.NotNil(t, req.TimeCriteria) {
					return
				}
				assert.Equal(t, 0, req.PageNum)
				assert.False(t, req.WithInsights)
				assert.Equal(t, tt.duration.Milliseconds(), req.TimeCriteria.End-req.TimeCriteria.Start)
				assert.GreaterOrEqual(t, req.TimeCriteria.End, before)
				assert.LessOrEqual(t, req.TimeCriteria.End, time.Now().UnixMilli())
				_, _ = fmt.Fprint(w, `{"entities":[],"edges":[],"pageNum":0,"lastPage":true}`)
			}))
			defer server.Close()
			loader := &kg.FakeWriteLoader{Cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}
			args := append([]string{"MATCH (s:Service) RETURN s", "-o", "json"}, tt.flags...)
			out, _, err := runKgCommand(t, func() *cobra.Command { return kg.NewCypherCommand(loader) }, args, "")
			require.NoError(t, err)
			assert.Equal(t, 1, calls)
			assert.JSONEq(t, `{"entities":[],"edges":[],"pageNum":0,"lastPage":true}`, out)
		})
	}
}
