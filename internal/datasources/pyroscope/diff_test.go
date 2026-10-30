package pyroscope_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dspyroscope "github.com/grafana/gcx/internal/datasources/pyroscope"
	"github.com/grafana/gcx/internal/datasources/query/scenesfiltertest"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const simpleDiff = `{"flamegraph":{"names":["total","main.handler"],"leftTicks":"100","rightTicks":"200","levels":[{"values":["0","100","0","0","200","0","0"]},{"values":["0","100","100","0","200","200","1"]}]}}`

type testExplorePane struct {
	Queries []struct {
		Selector   string   `json:"labelSelector"`
		ProfileIDs []string `json:"profileIdSelector"`
	} `json:"queries"`
	Range map[string]string `json:"range"`
}

// Keep the serialized form too: Explore's pane order follows JSON properties.
func decodeExplorePanes(t *testing.T, link string) (map[string]testExplorePane, string) {
	t.Helper()
	u, err := url.Parse(link)
	require.NoError(t, err)
	assert.Equal(t, "/explore", u.Path)
	encoded := u.Query().Get("panes")
	var panes map[string]testExplorePane
	require.NoError(t, json.Unmarshal([]byte(encoded), &panes))
	for _, pane := range panes {
		require.Len(t, pane.Queries, 1)
	}
	return panes, encoded
}

func diffArgs() []string {
	return []string{`{service_name="frontend",version="v1"}`, `{service_name="frontend",version="v2"}`,
		"-d", "pyro-uid", "--profile-type", "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
		"--before-from", "2026-01-01T00:00:00Z", "--before-to", "2026-01-01T00:05:00Z",
		"--after-from", "2026-01-01T01:00:00Z", "--after-to", "2026-01-01T01:05:00Z"}
}

func profileCommandRunner(t *testing.T, host string, orgID int, build func(*providers.ConfigLoader) *cobra.Command) func(...string) (string, string, error) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, fmt.Appendf(nil, `contexts:
  test:
    grafana:
      server: %q
      token: test-token
      org-id: %d
      tls:
        insecure-skip-verify: true
current-context: test
`, host, orgID), 0600))
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(configPath)
	return func(args ...string) (string, string, error) {
		t.Helper()
		cmd := build(loader)
		root := &cobra.Command{Use: "gcx", SilenceUsage: true, SilenceErrors: true}
		root.AddCommand(cmd)
		var stdout, stderr bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs(append([]string{cmd.Name()}, args...))
		err := root.Execute()
		return stdout.String(), stderr.String(), err
	}
}

func executeProfileDiff(t *testing.T, host string, args []string) (string, string, error) {
	t.Helper()
	return profileCommandRunner(t, host, 7, dspyroscope.DiffCmd)(args...)
}

func TestProfileDiffCmd_RequestOutputAndLinks(t *testing.T) {
	testutils.SetAgentMode(t, false)
	bodies := make(chan map[string]map[string]string, 8)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			http.NotFound(w, r)
		case "/api/datasources/uid/pyro-uid":
			_, _ = w.Write([]byte(`{"uid":"pyro-uid","type":"grafana-pyroscope-datasource"}`))
		case "/api/datasources/proxy/uid/pyro-uid/querier.v1.QuerierService/Diff":
			var body map[string]map[string]string
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			bodies <- body
			_, _ = w.Write([]byte(simpleDiff))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Run("relative windows share one whole-second clock", func(t *testing.T) {
		args := append(diffArgs(), "-o", "json", "--before-from", "now-1h", "--before-to", "now", "--after-from", "now-1h", "--after-to", "now")
		stdout, _, err := executeProfileDiff(t, server.URL, args)
		require.NoError(t, err)
		body := <-bodies
		assert.Equal(t, body["left"]["start"], body["right"]["start"])
		assert.Equal(t, body["left"]["end"], body["right"]["end"])
		assert.Equal(t, "50000", body["left"]["maxNodes"])
		start, err := strconv.ParseInt(body["left"]["start"], 10, 64)
		require.NoError(t, err)
		end, err := strconv.ParseInt(body["left"]["end"], 10, 64)
		require.NoError(t, err)
		assert.EqualValues(t, 3600000, end-start)
		assert.EqualValues(t, 0, end%1000)
		var result pyroscope.DiffResult
		require.NoError(t, json.Unmarshal([]byte(stdout), &result))
		assert.Equal(t, "profile_diff", result.Type)
		assert.Equal(t, 1, result.SchemaVersion)
		u, err := url.Parse(result.Links["comparison"])
		require.NoError(t, err)
		assert.Equal(t, body["left"]["end"], u.Query().Get("diffTo"))
	})
	for _, output := range []string{"json", "yaml", "table", "wide", "agents"} {
		t.Run(output, func(t *testing.T) {
			args := append(diffArgs(), "-o", output, "--max-nodes", "4096", "--share-link", "--drilldown-link")
			stdout, stderr, err := executeProfileDiff(t, server.URL, args)
			require.NoError(t, err)
			body := <-bodies
			assert.Equal(t, "1767225600000", body["left"]["start"])
			assert.Equal(t, "1767225900000", body["left"]["end"])
			assert.Equal(t, "1767229200000", body["right"]["start"])
			assert.Equal(t, "1767229500000", body["right"]["end"])
			assert.Equal(t, diffArgs()[0], body["left"]["labelSelector"])
			assert.Equal(t, diffArgs()[1], body["right"]["labelSelector"])
			assert.Equal(t, "4096", body["left"]["maxNodes"])
			assert.Equal(t, body["left"]["profileTypeID"], body["right"]["profileTypeID"])
			assert.Contains(t, stdout, "main.handler")
			assert.NotContains(t, stdout, "Profiles Drilldown link:")
			assert.Equal(t, 1, strings.Count(stderr, "Explore link:"))
			assert.Equal(t, 1, strings.Count(stderr, "Profiles Drilldown link:"))
			link, err := url.Parse(linkAfter(t, stderr, "Profiles Drilldown link: "))
			require.NoError(t, err)
			params := link.Query()
			assert.Equal(t, "diff-flame-graph", params.Get("explorationType"))
			assert.Equal(t, "7", params.Get("orgId"))
			assert.Equal(t, body["left"]["start"], params.Get("diffFrom"))
			assert.Equal(t, body["left"]["end"], params.Get("diffTo"))
			assert.Equal(t, body["right"]["start"], params.Get("diffFrom-2"))
			assert.Equal(t, body["right"]["end"], params.Get("diffTo-2"))
			// These custom app keys bypass Scenes' numeric URL date decoder.
			// Millisecond strings make the comparison timeline show Invalid date.
			assert.Equal(t, "2026-01-01T01:00:00Z", params.Get("comparisonFrom"))
			assert.Equal(t, "2026-01-01T01:05:00Z", params.Get("comparisonTo"))
			assert.Equal(t, "version|=|v1", params.Get("var-filtersBaseline"))
			assert.Equal(t, "version|=|v2", params.Get("var-filtersComparison"))
			if output == "json" {
				var result pyroscope.DiffResult
				require.NoError(t, json.Unmarshal([]byte(stdout), &result))
				assert.True(t, result.Complete)
				assert.EqualValues(t, 100, result.Symbols[0].Self.Delta)
				assert.Len(t, result.Links, 4)
				for _, side := range []string{"before", "after"} {
					u, err := url.Parse(result.Links[side])
					require.NoError(t, err)
					assert.Equal(t, "flame-graph", u.Query().Get("explorationType"))
					assert.Equal(t, "4096", u.Query().Get("maxNodes"))
				}
			}
		})
	}
	for _, tc := range []struct {
		name, before, after string
		allExplore          bool
	}{
		{"different services", diffArgs()[0], `{service_name="different"}`, false},
		{"regex service", diffArgs()[0], `{service_name=~"front.*"}`, false},
		{"no service", diffArgs()[0], `{env="prod"}`, false},
		{"quote in service", `{service_name="front\"end"}`, `{service_name="front\"end"}`, true},
		{"backslash in service", `{service_name="front\\end"}`, `{service_name="front\\end"}`, true},
		{"newline in service", `{service_name="front\nend"}`, `{service_name="front\nend"}`, true},
	} {
		t.Run("fallback/"+tc.name, func(t *testing.T) {
			args := diffArgs()
			args[0], args[1] = tc.before, tc.after
			stdout, stderr, err := executeProfileDiff(t, server.URL, append(args, "-o", "json", "--share-link", "--drilldown-link"))
			require.NoError(t, err)
			body := <-bodies
			assert.Equal(t, tc.before, body["left"]["labelSelector"])
			assert.Equal(t, tc.after, body["right"]["labelSelector"])
			assert.NotContains(t, stderr, "Profiles Drilldown link:")
			assert.Equal(t, 1, strings.Count(stderr, "Explore link:"))
			var result pyroscope.DiffResult
			require.NoError(t, json.Unmarshal([]byte(stdout), &result))
			assert.Empty(t, result.Links["comparison"])
			assert.NotEmpty(t, result.Warnings)
			panes, paneJSON := decodeExplorePanes(t, result.Links["explore"])
			require.Len(t, panes, 2)
			assert.Equal(t, args[0], panes["A"].Queries[0].Selector)
			assert.Equal(t, args[1], panes["B"].Queries[0].Selector)
			assert.Equal(t, "1767225600000", panes["A"].Range["from"])
			assert.Equal(t, "1767229200000", panes["B"].Range["from"])
			// Explore uses Object.entries order, not the semantic pane names.
			assert.Less(t, strings.Index(paneJSON, `"A":`), strings.Index(paneJSON, `"B":`))
			if tc.allExplore {
				for side, selector := range map[string]string{"before": tc.before, "after": tc.after} {
					profilePanes, _ := decodeExplorePanes(t, result.Links[side])
					assert.Equal(t, selector, profilePanes["gcx"].Queries[0].Selector)
				}
			}
		})
	}
}

func TestProfileDiffCmd_InvalidInputBeforeIO(t *testing.T) {
	testutils.SetAgentMode(t, false)
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected I/O", http.StatusInternalServerError)
	}))
	defer server.Close()
	for _, tc := range []struct {
		name  string
		extra []string
		err   string
	}{
		{"no profile type", []string{"--profile-type", ""}, "--profile-type is required"},
		{"whitespace profile type", []string{"--profile-type", " "}, "--profile-type is required"},
		{"empty datasource", []string{"-d", ""}, "--datasource must not be empty"},
		{"zero node budget", []string{"--max-nodes", "0"}, "greater than 0"},
		{"negative node budget", []string{"--max-nodes", "-1"}, "greater than 0"},
		{"empty before bound", []string{"--before-from", ""}, "--before-from is required"},
		{"whitespace after bound", []string{"--after-to", " "}, "--after-to is required"},
		{"invalid time", []string{"--after-from", "garbage"}, "invalid --after-from"},
		{"reversed before", []string{"--before-from", "2026-01-02T00:00:00Z"}, "--before-from must be earlier"},
		{"reversed after", []string{"--after-to", "2025-01-01T00:00:00Z"}, "--after-from must be earlier"},
		{"unsupported format", []string{"-o", "pprof"}, "pprof"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := executeProfileDiff(t, server.URL, append(diffArgs(), tc.extra...))
			require.ErrorContains(t, err, tc.err)
			var usageErr *gcxerrors.UsageError
			require.ErrorAs(t, err, &usageErr)
			assert.Contains(t, usageErr.Expected, "diff BEFORE AFTER")
			assert.Zero(t, calls.Load())
		})
	}
	for _, args := range [][]string{nil, {"{}"}, {"{}", "{}", "{}"}, append([]string{"", "{}"}, diffArgs()[2:]...), append([]string{"{}", " "}, diffArgs()[2:]...)} {
		_, _, err := executeProfileDiff(t, server.URL, args)
		require.Error(t, err)
		assert.Zero(t, calls.Load())
	}
}

func TestProfilesDiffDrilldownURL(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, before, after string
		ok                  bool
	}{
		{"different filters", `{service_name="front/end",version="old",env!="test"}`, `{service_name="front/end",version="new",region=~"a,b|c#d"}`, true},
		{"different services", `{service_name="a"}`, `{service_name="b"}`, false},
		{"escaped quote in service", `{service_name="front\"end"}`, `{service_name="front\"end"}`, false},
		{"backslash in service", `{service_name="front\\end"}`, `{service_name="front\\end"}`, false},
		{"empty service", `{service_name=""}`, `{service_name=""}`, false},
		{"no shared service", `{}`, `{}`, false},
		{"invalid expression", `not a selector`, `{}`, false},
		{"unrepresentable escape", `{service_name="a",env="__gfh__"}`, `{service_name="a"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := pyroscope.DiffRequest{ProfileType: "cpu", MaxNodes: 1024,
				Before: pyroscope.ProfileSelection{Selector: tc.before, From: start, To: start.Add(time.Hour)},
				After:  pyroscope.ProfileSelection{Selector: tc.after, From: start.Add(2 * time.Hour), To: start.Add(3 * time.Hour)}}
			link, ok := dspyroscope.ProfilesDiffDrilldownURL("https://example.test/grafana/", "pyro", req)
			require.Equal(t, tc.ok, ok)
			if !ok {
				assert.Empty(t, link)
				return
			}
			u, err := url.Parse(link)
			require.NoError(t, err)
			assert.Equal(t, "/grafana/a/grafana-pyroscope-app/explore", u.Path)
			q := u.Query()
			assert.Equal(t, "front/end", q.Get("var-serviceName"))
			assert.Equal(t, "diff-flame-graph", q.Get("explorationType"))
			assert.Equal(t, "1024", q.Get("maxNodes"))
			assert.Equal(t, strconv.FormatInt(start.UnixMilli(), 10), q.Get("from"))
			assert.Equal(t, q.Get("from"), q.Get("diffFrom"))
			assert.Equal(t, q.Get("to"), q.Get("diffTo"))
			assert.Equal(t, req.After.From.Format(time.RFC3339), q.Get("comparisonFrom"))
			assert.Equal(t, req.After.To.Format(time.RFC3339), q.Get("comparisonTo"))
			assert.Equal(t, strconv.FormatInt(req.After.From.UnixMilli(), 10), q.Get("diffFrom-2"))
			assert.Equal(t, strconv.FormatInt(req.After.To.UnixMilli(), 10), q.Get("diffTo-2"))
			filters := q["var-filtersComparison"]
			require.Len(t, filters, 2)
			assert.Equal(t, scenesfiltertest.Filter{Key: "region", Operator: "=~", Value: "a,b|c#d"}, scenesfiltertest.Decode(filters[1]))
		})
	}
}
