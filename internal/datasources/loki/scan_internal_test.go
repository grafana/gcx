package loki

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	lokiclient "github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestScanCommands(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "true")
	agent.SetFlag(true)
	t.Cleanup(agent.ResetForTesting)
	tests := []struct {
		name      string
		body      string
		status    int
		flags     []string
		metric    bool
		wantQuery bool
		wantError bool
	}{
		{name: "below", body: `{"bytes":9999999999}`, wantQuery: true},
		{name: "boundary", body: `{"bytes":10000000000}`, wantQuery: true},
		{name: "above blocks even auto approve", body: `{"bytes":10000000001}`, wantError: true},
		{name: "yes false blocks", body: `{"bytes":25000000000}`, flags: []string{"--yes=false"}, wantError: true},
		{name: "estimate never executes with yes", body: `{"bytes":25000000000}`, flags: []string{"--estimate-scan", "--yes"}},
		{name: "yes approval", body: `{"bytes":25000000000}`, flags: []string{"--yes"}, wantQuery: true},
		{name: "unknown cannot bypass known", body: `{"bytes":10000000001}`, flags: []string{"--approve-unknown-scan"}, wantError: true},
		{name: "estimate only", body: `{"bytes":99999999999}`, flags: []string{"--estimate-scan"}},
		{name: "zero", body: `{"bytes":0}`, wantQuery: true},
		{name: "missing", body: `{}`, wantError: true},
		{name: "null", body: `{"bytes":null}`, wantError: true},
		{name: "negative", body: `{"bytes":-1}`, wantError: true},
		{name: "overflow", body: `{"bytes":9223372036854775808}`, wantError: true},
		{name: "malformed", body: `no`, wantError: true},
		{name: "unavailable", status: 404, wantError: true},
		{name: "unavailable estimate", status: 404, flags: []string{"--estimate-scan"}},
		{name: "unavailable approved", status: 404, flags: []string{"--approve-unknown-scan"}, wantQuery: true},
		{name: "yes cannot approve unknown", status: 404, flags: []string{"--yes"}, wantError: true},
		{name: "auth", status: 401, flags: []string{"--approve-unknown-scan"}, wantError: true},
		{name: "forbidden", status: 403, flags: []string{"--approve-unknown-scan"}, wantError: true},
		{name: "metric below", metric: true, body: `{"bytes":123}`, wantQuery: true},
		{name: "metric boundary", metric: true, body: `{"bytes":10000000000}`, wantQuery: true},
		{name: "metric above", metric: true, body: `{"bytes":10000000001}`, wantError: true},
		{name: "metric known approval", metric: true, body: `{"bytes":10000000001}`, flags: []string{"--yes"}, wantQuery: true},
		{name: "metric estimate only", metric: true, body: `{"bytes":123}`, flags: []string{"--estimate-scan"}},
		{name: "metric unknown", metric: true, status: 404, wantError: true},
		{name: "metric approved", metric: true, status: 404, flags: []string{"--approve-unknown-scan"}, wantQuery: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sequence []string
			var estimateStart, estimateEnd int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/bootdata":
					http.NotFound(w, r)
				case r.URL.Path == "/api/datasources/uid/uid":
					_, _ = w.Write([]byte(`{"uid":"uid","type":"loki"}`))
				case strings.HasSuffix(r.URL.Path, "/resources/index/stats"):
					sequence = append(sequence, "estimate")
					assert.Equal(t, `{app="test"}`, r.URL.Query().Get("query"))
					estimateStart, _ = strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
					estimateEnd, _ = strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
					if tt.status != 0 {
						w.WriteHeader(tt.status)
					}
					_, _ = w.Write([]byte(tt.body))
				case r.Method == http.MethodPost:
					sequence = append(sequence, "query")
					var body map[string]any
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					queryStart := estimateStart
					if tt.metric {
						queryStart += int64(5 * time.Minute)
					}
					assert.Equal(t, strconv.FormatInt(queryStart/int64(time.Millisecond), 10), body["from"])
					assert.Equal(t, strconv.FormatInt(estimateEnd/int64(time.Millisecond), 10), body["to"])
					_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(testutils.CreateTempFile(t, fmt.Sprintf("current-context: test\nstacks:\n  test:\n    grafana:\n      server: %s\n      token: test-token\n      org-id: 1\ncontexts:\n  test:\n    stack: test\n", server.URL)))
			cmd := QueryCmd(loader)
			expr := `{app="test"} |= "error"`
			if tt.metric {
				cmd = MetricsCmd(loader)
				expr = `count_over_time({app="test"}[5m])`
			}
			root := &cobra.Command{Use: "logs", SilenceUsage: true, SilenceErrors: true}
			root.AddCommand(cmd)
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetIn(strings.NewReader("yes\n"))
			args := []string{cmd.Name(), expr, "-d", "uid", "--since=1h", "-o=json"}
			root.SetArgs(append(args, tt.flags...))
			err := root.Execute()
			if tt.wantError {
				require.Error(t, err)
				if tt.status == 401 || tt.status == 403 {
					var apiErr *queryerror.APIError
					require.ErrorAs(t, err, &apiErr)
					assert.Equal(t, tt.status, apiErr.StatusCode)
				} else {
					var detailed *gcxerrors.DetailedError
					require.ErrorAs(t, err, &detailed)
					assert.Equal(t, gcxerrors.ExitUsageError, *detailed.ExitCode)
				}
			} else {
				require.NoError(t, err)
				assert.True(t, json.Valid(stdout.Bytes()), stdout.String())
			}
			assert.Equal(t, tt.wantQuery, strings.Contains(strings.Join(sequence, ","), "query"))
			if tt.wantQuery {
				assert.Equal(t, []string{"estimate", "query"}, sequence)
			}
			if !tt.wantQuery {
				assert.Equal(t, []string{"estimate"}, sequence)
			}
		})
	}
}

func TestScanValidation(t *testing.T) {
	require.Error(t, (&ScanOpts{Estimate: true}).Validate("raw"))
	require.Error(t, (&ScanOpts{Estimate: true}).Validate("graph"))
}

func TestScanPrompt(t *testing.T) {
	for _, tt := range []struct {
		answer  string
		unknown bool
		code    int
	}{
		{"y\n", false, 0}, {"yes\n", false, 0}, {"no\n", false, 5}, {"25GB\n", false, 5}, {"\n", false, 5}, {"", false, 5},
		{"yes\n", true, 0}, {"no\n", true, 5}, {"\n", true, 5},
	} {
		e := &lokiclient.ScanEstimate{}
		if !tt.unknown {
			e.Bytes = new(int64(25_000_000_000))
		}
		err := (&ScanOpts{}).prompt(e, strings.NewReader(tt.answer), &bytes.Buffer{})
		if tt.code == 0 {
			require.NoError(t, err)
		} else {
			var detailed *gcxerrors.DetailedError
			require.ErrorAs(t, err, &detailed)
			assert.Equal(t, tt.code, *detailed.ExitCode)
		}
	}
}

func TestScanRecoveryHints(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(agent.ResetForTesting)
	for _, tt := range []struct {
		name, expr, body string
		status           int
		want, absent     string
	}{
		{"unsupported", `rate({app="test"}[5m])+rate({app="other"}[5m])`, `{"bytes":1}`, 200, "Simplify to a supported", "retry with --estimate-scan"},
		{"malformed", `count_over_time({app="test"}[5m])`, `{}`, 200, "Resolve the index-statistics failure", "narrow --since"},
		{"unavailable", `count_over_time({app="test"}[5m])`, `{}`, 404, "Resolve the index-statistics failure", "narrow --since"},
		{"large", `count_over_time({app="test"}[5m])`, `{"bytes":10000000001}`, 200, "Narrow the time range", "--approve-unknown-scan"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, http.MethodGet, r.Method)
				assert.True(t, strings.HasSuffix(r.URL.Path, "/resources/index/stats"))
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)
			client, err := lokiclient.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
			require.NoError(t, err)
			end := time.Date(2026, 9, 29, 7, 15, 0, 0, time.UTC)
			req := lokiclient.QueryRequest{Query: tt.expr, Start: end.Add(-5 * time.Minute), End: end}
			var stderr bytes.Buffer
			_, err = (&ScanOpts{}).Run(t.Context(), client, "uid", req, true, strings.NewReader(""), &stderr)
			var detailed *gcxerrors.DetailedError
			require.ErrorAs(t, err, &detailed)
			hints := strings.Join(detailed.Suggestions, " ")
			assert.Contains(t, hints, tt.want)
			assert.NotContains(t, hints, tt.absent)
			if tt.name == "unsupported" {
				assert.Equal(t, 0, calls)
			} else {
				assert.Equal(t, 1, calls)
				assert.Contains(t, detailed.Details, "Scan range: 2026-09-29T07:05:00Z to 2026-09-29T07:15:00Z")
			}
		})
	}
}
