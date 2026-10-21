package loki //nolint:testpackage // white-box: exercises the unexported guard and policy fetch directly

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/mcp-grafana/lokiguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newPolicyServer(t *testing.T, body string, status int, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPolicySource_Fetch(t *testing.T) {
	const tenant = `{"status":"success","data":{"mode":"shadow","maxBytes":1024,"maxRangeSeconds":60,"source":"tenant"}}`
	now := time.Now()

	t.Run("uses the served policy", func(t *testing.T) {
		var hits atomic.Int32
		srv := newPolicyServer(t, tenant, http.StatusOK, &hits)
		p, reason := policySource{url: srv.URL, client: srv.Client()}.fetch(t.Context(), now)
		assert.Empty(t, reason)
		assert.Equal(t, queryPolicy{Mode: modeShadow, MaxBytes: 1024, MaxRangeSeconds: 60, Source: "tenant"}, p)
	})

	for name, tc := range map[string]struct {
		body   string
		status int
	}{
		"server error":  {`{}`, http.StatusInternalServerError},
		"not found":     {`{}`, http.StatusNotFound},
		"malformed":     {`nope`, http.StatusOK},
		"unknown mode":  {`{"status":"success","data":{"mode":"yolo"}}`, http.StatusOK},
		"negative size": {`{"status":"success","data":{"mode":"enforce","maxBytes":-1}}`, http.StatusOK},
		"bare object":   {`{"mode":"enforce"}`, http.StatusOK},
	} {
		t.Run("falls back to the built-in default on "+name, func(t *testing.T) {
			var hits atomic.Int32
			srv := newPolicyServer(t, tc.body, tc.status, &hits)
			p, reason := policySource{url: srv.URL, client: srv.Client()}.fetch(t.Context(), now)
			assert.NotEmpty(t, reason)
			assert.Equal(t, defaultQueryPolicy(), p)
			assert.Equal(t, queryPolicy{Mode: modeEnforce, MaxBytes: 50 << 30, MaxRangeSeconds: 86400, Source: "default"}, p)
		})
	}

	t.Run("falls back when no endpoint is reachable", func(t *testing.T) {
		p, reason := policySource{}.fetch(t.Context(), now)
		assert.NotEmpty(t, reason)
		assert.Equal(t, defaultQueryPolicy(), p)
	})

	t.Run("caches within the TTL and refetches after it", func(t *testing.T) {
		var hits atomic.Int32
		srv := newPolicyServer(t, tenant, http.StatusOK, &hits)
		src := policySource{url: srv.URL, client: srv.Client(), cacheTTL: 5 * time.Minute, cacheDir: t.TempDir()}

		_, _ = src.fetch(t.Context(), now)
		p, reason := src.fetch(t.Context(), now.Add(time.Minute))
		assert.Empty(t, reason)
		assert.Equal(t, modeShadow, p.Mode)
		assert.EqualValues(t, 1, hits.Load(), "second call inside the TTL must come from the cache")

		_, _ = src.fetch(t.Context(), now.Add(6*time.Minute))
		assert.EqualValues(t, 2, hits.Load(), "an expired entry must be refetched")
	})

	t.Run("does not cache the fallback default", func(t *testing.T) {
		var hits atomic.Int32
		srv := newPolicyServer(t, `{}`, http.StatusInternalServerError, &hits)
		src := policySource{url: srv.URL, client: srv.Client(), cacheTTL: 5 * time.Minute, cacheDir: t.TempDir()}
		_, _ = src.fetch(t.Context(), now)
		_, _ = src.fetch(t.Context(), now)
		assert.EqualValues(t, 2, hits.Load())
	})
}

func TestResolveGuardTarget(t *testing.T) {
	selfHosted := &config.Context{Grafana: &config.GrafanaConfig{Server: "http://localhost:3000", APIToken: "t"}}
	cloud := &config.Context{Grafana: &config.GrafanaConfig{Server: "https://mystack.grafana.net", APIToken: "t"}}

	t.Run("self-hosted targets are not guarded", func(t *testing.T) {
		t.Setenv(policyURLEnv, "")
		_, ok := resolveGuardTarget(selfHosted, config.NamespacedRESTConfig{})
		assert.False(t, ok)
		_, ok = resolveGuardTarget(nil, config.NamespacedRESTConfig{})
		assert.False(t, ok)
	})

	t.Run("cloud token targets use the plugin proxy route", func(t *testing.T) {
		t.Setenv(policyURLEnv, "")
		src, ok := resolveGuardTarget(cloud, config.NamespacedRESTConfig{})
		require.True(t, ok)
		assert.Equal(t, "https://mystack.grafana.net"+policyPathPlugin, src.url)
		assert.Positive(t, src.cacheTTL)
	})

	t.Run("cloud oauth targets use the assistant proxy endpoint", func(t *testing.T) {
		t.Setenv(policyURLEnv, "")
		oauth := &config.Context{Grafana: &config.GrafanaConfig{
			Server: "https://mystack.grafana.net", ProxyEndpoint: "https://assistant.example/", OAuthToken: "gat_x", OAuthRefreshToken: "gar_x",
		}}
		src, ok := resolveGuardTarget(oauth, config.NamespacedRESTConfig{})
		require.True(t, ok)
		assert.Equal(t, "https://assistant.example"+policyPathCLI, src.url)
	})

	t.Run("the POC override forces the guard on without cache", func(t *testing.T) {
		t.Setenv(policyURLEnv, "http://127.0.0.1:1/policy")
		src, ok := resolveGuardTarget(selfHosted, config.NamespacedRESTConfig{})
		require.True(t, ok)
		assert.Equal(t, "http://127.0.0.1:1/policy", src.url)
		assert.Zero(t, src.cacheTTL)
	})
}

// statsServer answers index/stats with bytes chosen by selector content.
func statsClient(t *testing.T, bytesFor func(query string) (int, bool)) (*loki.Client, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.True(t, strings.HasSuffix(r.URL.Path, "/resources/index/stats"), r.URL.Path)
		assert.Equal(t, "Source=gcx", r.Header.Get("X-Query-Tags"))
		n, ok := bytesFor(r.URL.Query().Get("query"))
		if !ok {
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"entries":1,"bytes":` + strconv.Itoa(n) + `}`))
	}))
	t.Cleanup(srv.Close)
	client, err := loki.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}, Namespace: "default"})
	require.NoError(t, err)
	return client, &calls
}

func TestQueryGuard_Check(t *testing.T) {
	cheap := func(string) (int, bool) { return 1 << 20, true }
	costly := func(q string) (int, bool) {
		if strings.Contains(q, "firehose") {
			return 200 << 20, true
		}
		return 1 << 20, true
	}
	failing := func(string) (int, bool) { return 0, false }

	now := time.Now()

	tests := []struct {
		name      string
		mode      policyMode
		agent     bool
		expr      string
		window    time.Duration
		stats     func(string) (int, bool)
		wantBlock bool
		wantWarn  string // substring expected on stderr
	}{
		{name: "cheap query is allowed", mode: modeEnforce, expr: `{app="api"}`, window: time.Hour, stats: cheap},
		{name: "over the byte budget blocks", mode: modeEnforce, expr: `{app="firehose"}`, window: time.Hour, stats: costly, wantBlock: true},
		{name: "catch-all selector blocks", mode: modeEnforce, expr: `{job=~".+"}`, window: time.Hour, stats: cheap, wantBlock: true},
		{name: "range cap blocks", mode: modeEnforce, expr: `{app="api"}`, window: 48 * time.Hour, stats: cheap, wantBlock: true},
		{name: "shadow never blocks", mode: modeShadow, expr: `{app="firehose"}`, window: time.Hour, stats: costly},
		{name: "off never blocks", mode: modeOff, expr: `{job=~".+"}`, window: time.Hour, stats: costly},
		{name: "unparseable blocks in agent mode", mode: modeEnforce, agent: true, expr: `not logql`, window: time.Hour, stats: cheap, wantBlock: true},
		{name: "unparseable warns in a terminal", mode: modeEnforce, expr: `not logql`, window: time.Hour, stats: cheap, wantWarn: "could not estimate"},
		{name: "failed estimate blocks in agent mode", mode: modeEnforce, agent: true, expr: `{app="api"}`, window: time.Hour, stats: failing, wantBlock: true},
		{name: "failed estimate warns in a terminal", mode: modeEnforce, expr: `{app="api"}`, window: time.Hour, stats: failing, wantWarn: "index/stats estimate failed"},
		{name: "unknown in shadow mode is silent in agent mode", mode: modeShadow, agent: true, expr: `not logql`, window: time.Hour, stats: cheap},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			body := `{"status":"success","data":{"mode":"` + string(tc.mode) + `","maxBytes":52428800,"maxRangeSeconds":86400,"source":"tenant"}}`
			srv := newPolicyServer(t, body, http.StatusOK, &hits)
			client, _ := statsClient(t, tc.stats)

			var stderr bytes.Buffer
			g := &queryGuard{
				policy: policySource{url: srv.URL, client: srv.Client()}, client: client, stderr: &stderr,
				agent: tc.agent, now: time.Now, timeout: 5 * time.Second,
			}
			err := g.check(context.Background(), "uid", tc.expr, true, now.Add(-tc.window), now, now)

			if !tc.wantBlock {
				require.NoError(t, err)
				if tc.wantWarn == "" {
					assert.Empty(t, stderr.String())
				} else {
					assert.Contains(t, stderr.String(), tc.wantWarn)
				}
				return
			}
			require.Error(t, err)
			var de gcxerrors.DetailedError
			require.ErrorAs(t, err, &de)
			assert.Equal(t, gcxerrors.SummaryQueryBlocked, de.Summary)
			assert.NotEmpty(t, de.Details)
			assert.NotEmpty(t, de.Suggestions)
			assert.Empty(t, stderr.String(), "a block is reported through the error, not stderr")
		})
	}

	t.Run("a blocked selector costs no stats round trip", func(t *testing.T) {
		var hits atomic.Int32
		srv := newPolicyServer(t, `{"status":"success","data":{"mode":"enforce","maxBytes":52428800,"maxRangeSeconds":86400}}`, http.StatusOK, &hits)
		client, calls := statsClient(t, cheap)
		g := &queryGuard{policy: policySource{url: srv.URL, client: srv.Client()}, client: client, stderr: &bytes.Buffer{}, now: time.Now, timeout: time.Second}
		require.Error(t, g.check(context.Background(), "uid", `{job=~".+"}`, true, now.Add(-time.Hour), now, now))
		assert.Zero(t, calls.Load())
	})

	t.Run("instant queries are sized by their range vector alone", func(t *testing.T) {
		var hits atomic.Int32
		srv := newPolicyServer(t, `{"status":"success","data":{"mode":"enforce","maxBytes":52428800,"maxRangeSeconds":86400}}`, http.StatusOK, &hits)
		client, _ := statsClient(t, cheap)
		g := &queryGuard{policy: policySource{url: srv.URL, client: srv.Client()}, client: client, stderr: &bytes.Buffer{}, now: time.Now, timeout: time.Second}
		require.NoError(t, g.check(context.Background(), "uid", `rate({app="api"}[5m])`, false, time.Time{}, time.Time{}, now))
		require.Error(t, g.check(context.Background(), "uid", `rate({app="api"}[7d])`, false, time.Time{}, time.Time{}, now))
	})
}

func TestSplitGuidance(t *testing.T) {
	got := splitGuidance("query blocked: too big. Rewrite and retry: narrow it (e.g. namespace and app), then retry. Prefer metrics (e.g. count_over_time). Done.")
	assert.Equal(t, []string{
		"query blocked: too big",
		"Rewrite and retry: narrow it (e.g. namespace and app), then retry",
		"Prefer metrics (e.g. count_over_time)",
		"Done",
	}, got)
	assert.Empty(t, splitGuidance(""))
}

func TestUnknownExplanation(t *testing.T) {
	d := lokiguard.Decision{Verdict: lokiguard.Unknown, Cause: lokiguard.CauseEstimateFailed, Err: errors.New("boom")}
	assert.Contains(t, unknownExplanation(d), "boom")
	assert.Contains(t, unknownExplanation(lokiguard.Decision{Cause: lokiguard.CauseUnparseable}), "no stream selector")
}
