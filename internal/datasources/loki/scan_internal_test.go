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
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	lokiclient "github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{name: "finite approval", body: `{"bytes":25000000000}`, flags: []string{"--approve-scan=25GB"}, wantQuery: true},
		{name: "insufficient", body: `{"bytes":25000000001}`, flags: []string{"--approve-scan=25GB"}, wantError: true},
		{name: "lower approval", body: `{"bytes":1001}`, flags: []string{"--approve-scan=1KB"}, wantError: true},
		{name: "unknown cannot bypass known", body: `{"bytes":10000000001}`, flags: []string{"--approve-unknown-scan"}, wantError: true},
		{name: "estimate only", body: `{"bytes":99999999999}`, flags: []string{"--estimate"}},
		{name: "zero", body: `{"bytes":0}`, wantQuery: true},
		{name: "missing", body: `{}`, wantError: true},
		{name: "null", body: `{"bytes":null}`, wantError: true},
		{name: "negative", body: `{"bytes":-1}`, wantError: true},
		{name: "overflow", body: `{"bytes":9223372036854775808}`, wantError: true},
		{name: "malformed", body: `no`, wantError: true},
		{name: "unavailable", status: 404, wantError: true},
		{name: "unavailable estimate", status: 404, flags: []string{"--estimate"}},
		{name: "unavailable approved", status: 404, flags: []string{"--approve-unknown-scan"}, wantQuery: true},
		{name: "numeric cannot approve unknown", status: 404, flags: []string{"--approve-scan=25GB"}, wantError: true},
		{name: "auth", status: 401, flags: []string{"--approve-unknown-scan"}, wantError: true},
		{name: "forbidden", status: 403, flags: []string{"--approve-unknown-scan"}, wantError: true},
		{name: "metric unknown", metric: true, wantError: true},
		{name: "metric approved", metric: true, flags: []string{"--approve-unknown-scan"}, wantQuery: true},
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
					if !tt.metric {
						assert.Equal(t, strconv.FormatInt(estimateStart/int64(time.Millisecond), 10), body["from"])
						assert.Equal(t, strconv.FormatInt(estimateEnd/int64(time.Millisecond), 10), body["to"])
					}
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
			root.SetIn(strings.NewReader("25GB\n"))
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
			if tt.wantQuery && !tt.metric {
				assert.Equal(t, []string{"estimate", "query"}, sequence)
			}
			if !tt.wantQuery && !tt.metric {
				assert.Equal(t, []string{"estimate"}, sequence)
			}
			if tt.metric {
				assert.NotContains(t, sequence, "estimate")
			}
		})
	}
}

func TestScanValidation(t *testing.T) {
	for _, value := range []string{"", "0GB", "-1GB", "NaNGB", "InfGB", "1e3GB", "unlimited", "1", "1GiB", "0.1B", "999999999999999TB"} {
		t.Run(value, func(t *testing.T) {
			o := &ScanOpts{}
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			o.Setup(flags)
			require.NoError(t, flags.Set("approve-scan", value))
			err := o.Validate("json")
			var detailed *gcxerrors.DetailedError
			require.ErrorAs(t, err, &detailed)
			assert.Equal(t, gcxerrors.ExitUsageError, *detailed.ExitCode)
		})
	}
	for _, value := range []string{"1B", "1.5KB", "0.001GB", "25GB", "1TB"} {
		_, err := parseScanBytes(value)
		require.NoError(t, err)
	}
	require.Error(t, (&ScanOpts{ApproveScan: "1GB", ApproveUnknown: true}).Validate("json"))
	require.Error(t, (&ScanOpts{Estimate: true}).Validate("raw"))
	require.Error(t, (&ScanOpts{Estimate: true}).Validate("graph"))
}

func TestScanPrompt(t *testing.T) {
	for _, tt := range []struct {
		answer  string
		unknown bool
		code    int
	}{
		{"25GB\n", false, 0}, {"1GB\n", false, 2}, {"\n", false, 5}, {"", false, 5},
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
