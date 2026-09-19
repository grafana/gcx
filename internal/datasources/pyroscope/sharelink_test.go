package pyroscope_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	dspyroscope "github.com/grafana/gcx/internal/datasources/pyroscope"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("output failed") }

type exploreLinkCase struct {
	name          string
	metrics       bool
	args          []string
	response      string
	fallback      bool
	failure       bool
	outputFailure bool
	noLink        bool
	warning       string
}

func TestCommandsExploreLinks(t *testing.T) {
	const flamegraph = `{"flamegraph":{"names":["total"],"levels":[{"values":["0","10","10","0"]}],"total":"10","maxSelf":"10"}}`
	for _, tt := range []exploreLinkCase{
		{name: "profile table", args: []string{"-o", "table"}, response: flamegraph},
		{name: "profile json", args: []string{"-o", "json"}, response: flamegraph},
		{name: "profile graph", args: []string{"-o", "graph"}, response: flamegraph},
		{name: "profile yaml", args: []string{"-o", "yaml"}, response: flamegraph},
		{name: "profile dot", args: []string{"-o", "dot"}, response: `{"dot":"digraph G { N1 [label=\"main\"]; }"}`},
		{name: "dot dual fallback", args: []string{"-o", "dot"}, response: flamegraph},
		{name: "dot v1 fallback", args: []string{"-o", "dot"}, response: flamegraph, fallback: true},
		{name: "dot empty", args: []string{"-o", "dot"}, response: `{}`},
		{name: "pprof", args: []string{"-o", "pprof"}},
		{name: "span selector", args: []string{"-o", "json", "--span-id", "00f067aa0ba902b7"}, response: flamegraph},
		{name: "profile filter warning", args: []string{"-o", "json", "--profile-id", "550e8400-e29b-41d4-a716-446655440000"}, response: flamegraph, warning: "--profile-id"},
		{name: "trace filter warning", args: []string{"-o", "json", "--trace-id", "4bf92f3577b34da6a3ce929d0e0e4736"}, response: flamegraph, warning: "--trace-id"},
		{name: "stack filter warning", args: []string{"-o", "json", "--stacktrace-selector", "main"}, response: flamegraph, warning: "--stacktrace-selector"},
		{name: "metrics json", metrics: true, args: []string{"-o", "json", "--since", "2h"}},
		{name: "metrics table", metrics: true, args: []string{"-o", "table"}},
		{name: "metrics graph", metrics: true, args: []string{"-o", "graph"}},
		{name: "metrics top", metrics: true, args: []string{"-o", "json", "--top"}, warning: "--top"},
		{name: "metrics aggregation", metrics: true, args: []string{"-o", "json", "--aggregation", "average"}, warning: "--aggregation"},
		{name: "metrics step", metrics: true, args: []string{"-o", "json", "--step", "1m"}, warning: "--step"},
		{name: "query failure", args: []string{"-o", "json"}, failure: true},
		{name: "metrics failure", metrics: true, args: []string{"-o", "json"}, failure: true},
		{name: "output failure", args: []string{"-o", "json"}, response: flamegraph, outputFailure: true},
		{name: "browser unavailable", args: []string{"-o", "json", "--open"}, response: flamegraph},
		{name: "no flags", args: []string{"-o", "json"}, response: flamegraph, noLink: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GCX_AGENT_MODE", "false")
			t.Setenv("GCX_KEYCHAIN", "off")
			if tt.name == "browser unavailable" {
				t.Setenv("PATH", t.TempDir())
			}
			calls := 0
			var request map[string]any
			srv := newExploreTestServer(t, tt, &calls, &request)
			defer srv.Close()
			cfgPath := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(cfgPath, fmt.Appendf(nil, `
version: 1
credentials:
  keychain: off
stacks:
  default:
    grafana:
      server: %s
      token: test-token
      org-id: 7
      tls:
        insecure-skip-verify: true
contexts:
  default:
    stack: default
    datasources:
      pyroscope: pyro-uid
current-context: default
`, srv.URL), 0o600))
			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(cfgPath)
			cmd := dspyroscope.QueryCmd(loader)
			if tt.metrics {
				cmd = dspyroscope.MetricsCmd(loader)
			}
			root := &cobra.Command{Use: "gcx", SilenceUsage: true, SilenceErrors: true}
			root.AddCommand(cmd)
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			if tt.outputFailure {
				root.SetOut(failingWriter{})
			}
			args := append([]string{cmd.Name(), `{service_name="frontend"}`, "--profile-type", profileType}, tt.args...)
			if !tt.noLink {
				args = append(args, "--share-link")
			}
			artifact := filepath.Join(t.TempDir(), "profile.pb.gz")
			if tt.name == "pprof" {
				args = append(args, "--pprof-path", artifact)
			}
			root.SetArgs(args)
			before := time.Now().Add(-time.Hour - time.Second).UnixMilli()
			err := root.Execute()
			if tt.failure || tt.outputFailure {
				require.Error(t, err)
				if tt.outputFailure {
					require.ErrorContains(t, err, "output failed")
				} else {
					require.ErrorContains(t, err, "query rejected")
				}
				assert.NotContains(t, stderr.String(), "Explore link:")
				return
			}
			require.NoError(t, err)
			wantCalls := 1
			if tt.fallback {
				wantCalls = 2
			}
			assert.Equal(t, wantCalls, calls)
			assert.NotContains(t, stdout.String(), "Explore link:")
			if tt.noLink {
				assert.NotContains(t, stderr.String(), "Explore link:")
				return
			}
			require.Equal(t, 1, strings.Count(stderr.String(), "Explore link:"))
			_, raw, ok := strings.Cut(stderr.String(), "Explore link: ")
			require.True(t, ok)
			u, pane := parseExploreURL(t, strings.Fields(raw)[0])
			assert.Equal(t, "/explore", u.Path)
			assert.Equal(t, "7", u.Query().Get("orgId"))
			assert.Equal(t, "pyro-uid", pane.Datasource)
			assert.Equal(t, profileType, pane.Queries[0]["profileTypeId"])
			assert.Equal(t, `{service_name="frontend"}`, pane.Queries[0]["labelSelector"])
			if tt.name == "pprof" {
				data, err := os.ReadFile(artifact)
				require.NoError(t, err)
				require.Greater(t, len(data), 2)
				assert.Equal(t, []byte{0x1f, 0x8b}, data[:2])
			} else {
				assert.Equal(t, request["start"], pane.Range["from"])
				assert.Equal(t, request["end"], pane.Range["to"])
			}
			if tt.name == "profile json" {
				start, err := strconv.ParseInt(pane.Range["from"], 10, 64)
				require.NoError(t, err)
				assert.GreaterOrEqual(t, start, before)
				assert.LessOrEqual(t, start, time.Now().Add(-time.Hour).UnixMilli())
				assert.JSONEq(t, flamegraph, stdout.String())
			}
			if tt.metrics {
				assert.Equal(t, "metrics", pane.Queries[0]["queryType"])
				assert.Equal(t, []any{"service_name"}, pane.Queries[0]["groupBy"])
				assert.InDelta(t, 10, pane.Queries[0]["limit"], 0)
			}
			if tt.name == "browser unavailable" {
				assert.Contains(t, stderr.String(), "could not open browser")
			}
			if tt.warning != "" {
				assert.Contains(t, stderr.String(), "Grafana Explore link does not preserve "+tt.warning)
			}
		})
	}
}

func newExploreTestServer(t *testing.T, tt exploreLinkCase, calls *int, request *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/bootdata":
			http.NotFound(w, r)
		case r.URL.Path == "/api/datasources/uid/pyro-uid":
			_, _ = io.WriteString(w, `{"uid":"pyro-uid","type":"grafana-pyroscope-datasource","jsonData":{"minStep":"15s"}}`)
		case strings.HasPrefix(r.URL.Path, "/api/datasources/proxy/uid/pyro-uid/querier.v1.QuerierService/"):
			(*calls)++
			if tt.failure {
				http.Error(w, `{"message":"query rejected"}`, http.StatusBadRequest)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/SelectMergeProfile") {
				_, _ = w.Write([]byte{0x32, 0})
				return
			}
			*request = nil
			assert.NoError(t, json.NewDecoder(r.Body).Decode(request))
			if tt.fallback && (*request)["format"] != nil {
				http.Error(w, `{"message":"dot format is only supported with the v2 query backend"}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if tt.metrics {
				_, _ = io.WriteString(w, `{"series":[{"labels":[{"name":"service_name","value":"frontend"}],"points":[{"timestamp":"1788256800000","value":10},{"timestamp":"1788256860000","value":20}]}]}`)
			} else {
				_, _ = io.WriteString(w, tt.response)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	return srv
}
