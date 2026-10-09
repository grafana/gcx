package kg_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const cypherResult = `{"columns":["name","count","missing","path"],"rows":[["checkout",9007199254740993,null,{"nodes":[{"kind":"node","ref":"n0","labels":["Service"],"properties":{"name":"checkout"}}],"relationships":[]}]],"stats":{"rowCount":1,"columnCount":4,"elapsedMs":5}}`

func TestGraphQueryCommandOutput(t *testing.T) {
	for _, tt := range []struct {
		name  string
		flags []string
		agent bool
		want  string
	}{
		{name: "json", flags: []string{"-o", "json"}, want: `9007199254740993`},
		{name: "yaml", flags: []string{"-o", "yaml"}, want: `9007199254740993`},
		{name: "table", flags: []string{"-o", "table"}, want: `9007199254740993`},
		{name: "rows preserve integers", flags: []string{"--json", "rows"}, want: `9007199254740993`},
		{name: "fields", flags: []string{"--json", "columns,stats"}, want: `"columnCount": 4`},
		{name: "jq", flags: []string{"--jq", ".rows[0][0]"}, want: `checkout`},
		{name: "agent", agent: true, want: `9007199254740993`},
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
				assert.Equal(t, "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/query/cypher", r.URL.Path)
				var body map[string]any
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
					return
				}
				assert.Equal(t, map[string]any{"query": "RETURN 1 AS value LIMIT 1", "start": float64(1757000000000), "end": float64(1757003600000)}, body)
				_, _ = fmt.Fprint(w, cypherResult)
			}))
			defer server.Close()
			loader := &kg.FakeWriteLoader{Cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}
			args := append([]string{"RETURN 1 AS value LIMIT 1", "--from", "1757000000", "--to", "1757003600"}, tt.flags...)
			out, stderr, err := runKgCommand(t, func() *cobra.Command { return kg.NewGraphQueryCommand(loader) }, args, "")
			require.NoError(t, err)
			assert.Empty(t, stderr)
			assert.Equal(t, 1, calls)
			assert.Contains(t, out, tt.want)
			if tt.agent || tt.name == "json" {
				decodeSingleJSON(t, []byte(out))
				assert.Contains(t, out, `"nodes"`)
				assert.Contains(t, out, `null`)
			}
			if tt.name == "table" {
				assert.Contains(t, out, "name")
				assert.Contains(t, out, "checkout")
				assert.Contains(t, out, `"ref":"n0"`)
			}
		})
	}
}

func TestGraphQueryCommandTimeWindows(t *testing.T) {
	for _, tt := range []struct {
		name     string
		flags    []string
		duration time.Duration
	}{
		{name: "default hour", duration: time.Hour},
		{name: "since days", flags: []string{"--since", "7d"}, duration: 7 * 24 * time.Hour},
		{name: "relative", flags: []string{"--from", "now-2h", "--to", "now"}, duration: 2 * time.Hour},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pinHumanMode(t)
			before := time.Now().UnixMilli()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req kg.CypherQueryRequest
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
					return
				}
				assert.Equal(t, tt.duration.Milliseconds(), req.End-req.Start)
				assert.GreaterOrEqual(t, req.End, before)
				assert.LessOrEqual(t, req.End, time.Now().UnixMilli())
				_, _ = fmt.Fprint(w, `{"columns":["value"],"rows":[],"stats":{"columnCount":1,"rowCount":0,"elapsedMs":0}}`)
			}))
			defer server.Close()
			loader := &kg.FakeWriteLoader{Cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}
			args := append([]string{"RETURN 1 AS value LIMIT 0", "-o", "json"}, tt.flags...)
			out, stderr, err := runKgCommand(t, func() *cobra.Command { return kg.NewGraphQueryCommand(loader) }, args, "")
			require.NoError(t, err)
			assert.Empty(t, stderr)
			assert.Contains(t, out, `"rows": []`)
			assert.Contains(t, out, `"value"`)
		})
	}
}

func TestGraphQueryCommandInvalidInputsBeforeConfig(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"blank", []string{"  "}, "must not be blank"},
		{"empty since", []string{"RETURN 1 LIMIT 1", "--since="}, "--since must not be empty"},
		{"empty from", []string{"RETURN 1 LIMIT 1", "--from="}, "--from must not be empty"},
		{"empty to", []string{"RETURN 1 LIMIT 1", "--to="}, "--to must not be empty"},
		{"negative", []string{"RETURN 1 LIMIT 1", "--since=-1h"}, "must be positive"},
		{"zero", []string{"RETURN 1 LIMIT 1", "--since=0s"}, "must be positive"},
		{"bad duration", []string{"RETURN 1 LIMIT 1", "--since=1junkd"}, "invalid --since"},
		{"from only", []string{"RETURN 1 LIMIT 1", "--from=now-1h"}, "supplied together"},
		{"to only", []string{"RETURN 1 LIMIT 1", "--to=now"}, "supplied together"},
		{"conflict", []string{"RETURN 1 LIMIT 1", "--since=1h", "--from=now-1h", "--to=now"}, "mutually exclusive"},
		{"equal", []string{"RETURN 1 LIMIT 1", "--from=now", "--to=now"}, "start before end"},
		{"reverse", []string{"RETURN 1 LIMIT 1", "--from=now", "--to=now-1h"}, "start before end"},
		{"epoch zero", []string{"RETURN 1 LIMIT 1", "--from=1970-01-01T00:00:00Z", "--to=now"}, "positive epoch"},
		{"legacy page", []string{"RETURN 1 LIMIT 1", "--page=1"}, "unknown flag: --page"},
		{"legacy insights", []string{"RETURN 1 LIMIT 1", "--insights-only"}, "unknown flag: --insights-only"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pinHumanMode(t)
			loader := &kg.FakeWriteLoader{CfgErr: errors.New("configuration should not be loaded")}
			out, _, err := runKgCommand(t, func() *cobra.Command { return kg.NewGraphQueryCommand(loader) }, tt.args, "")
			require.ErrorContains(t, err, tt.want)
			assert.Empty(t, out)
		})
	}
}

func TestCypherQueryErrors(t *testing.T) {
	for _, tt := range []struct {
		status int
		code   string
	}{
		{400, "CYPHER_INVALID_REQUEST"}, {404, "NOT_FOUND"}, {422, "CYPHER_LIMIT_REQUIRED"}, {429, "CYPHER_CONCURRENCY_LIMIT"}, {503, "CYPHER_BACKEND_UNAVAILABLE"},
	} {
		t.Run(tt.code, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tt.status)
				_, _ = fmt.Fprintf(w, `{"code":%q,"message":"query refused"}`, tt.code)
			}))
			defer server.Close()
			_, err := newTestClient(t, server).CypherQuery(t.Context(), kg.CypherQueryRequest{Query: "RETURN 1", Start: 1, End: 2})
			var apiErr *kg.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, tt.status, apiErr.StatusCode)
			assert.Equal(t, tt.code, apiErr.Code)
			assert.Contains(t, apiErr.APIUserMessage(), tt.code)
			assert.Equal(t, 1, calls, "must not fall back to legacy endpoint")
		})
	}
}

func TestCypherRowsTableCodec(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
	}{
		{"empty", `{"columns":["name"],"rows":[]}`, "name"},
		{"duplicate columns", `{"columns":["x","x"],"rows":[[true,false],[null,0]]}`, "false"},
		{"nested", `{"columns":["list","map"],"rows":[[[1,2],{"a":null}]]}`, `{"a":null}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			forceNoColor(t)
			var result kg.CypherQueryResponse
			require.NoError(t, json.Unmarshal([]byte(tt.body), &result))
			var out bytes.Buffer
			err := (&kg.CypherRowsTableCodec{}).Encode(&out, &result)
			require.NoError(t, err)
			assert.Contains(t, strings.TrimSpace(out.String()), tt.want)
		})
	}
}

func TestCypherQueryPreservesGraphValues(t *testing.T) {
	for _, tt := range []struct{ name, response string }{
		{"graph values", `{"columns":["node","edge","path","values"],"rows":[[{"kind":"node","ref":"n0","labels":["Service"],"properties":{"name":"cart","large":9223372036854775807}},{"kind":"relationship","ref":"e0","type":"CALLS","startRef":"n0","endRef":"n1","properties":{}},{"nodes":[{"kind":"node","ref":"n0","labels":["Service"],"properties":{"name":"cart"}},{"kind":"node","ref":"n1","labels":["Service"],"properties":{"name":"db"}}],"relationships":[{"kind":"relationship","ref":"e0","type":"CALLS","startRef":"n0","endRef":"n1","properties":{}}]},[true,null,0,1.25,{"message":"ok"}]]],"stats":{"columnCount":4,"rowCount":1,"elapsedMs":2}}`},
		{"empty rows retain columns", `{"columns":["name"],"rows":[],"stats":{"columnCount":1,"rowCount":0,"elapsedMs":0}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, tt.response) }))
			defer server.Close()
			got, err := newTestClient(t, server).CypherQuery(t.Context(), kg.CypherQueryRequest{Query: "RETURN 1 LIMIT 1", Start: 1, End: 2})
			require.NoError(t, err)
			encoded, err := json.Marshal(got)
			require.NoError(t, err)
			var expected, actual any
			original := json.NewDecoder(strings.NewReader(tt.response))
			original.UseNumber()
			require.NoError(t, original.Decode(&expected))
			roundTrip := json.NewDecoder(bytes.NewReader(encoded))
			roundTrip.UseNumber()
			require.NoError(t, roundTrip.Decode(&actual))
			assert.Equal(t, expected, actual, "cell values and numeric precision must survive transport unchanged")
		})
	}
}
