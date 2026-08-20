//nolint:testpackage // Tests cover unexported seed-trace parsing and query builders.
package tempo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unixNanos converts a unix second to the uint64 nanosecond form used by the
// seed profile's time range.
func unixNanos(sec uint64) uint64 { return sec * 1_000_000_000 }

// newTestOpts returns baselineOpts with IO fully initialized (default codecs
// and format) via setup, so Validate() reaches the --window checks instead of
// failing earlier on an uninitialized IO.
func newTestOpts(window string) *baselineOpts {
	opts := &baselineOpts{}
	opts.setup(pflag.NewFlagSet("test", pflag.ContinueOnError))
	opts.Window = window
	return opts
}

func TestResolveWindow_SeedRangePaddedByWindow(t *testing.T) {
	opts := &baselineOpts{Window: "30m"}
	profile := seedProfile{
		HasTimeRange:  true,
		MinStartNanos: unixNanos(1_700_000_000),
		MaxEndNanos:   unixNanos(1_700_000_060), // +60s
	}
	start, end, err := opts.resolveWindow(profile, time.Now())
	require.NoError(t, err)
	// Padded 30m before the min start and 30m after the max end (past + future).
	assert.Equal(t, time.Unix(1_700_000_000, 0).Add(-30*time.Minute), start)
	assert.Equal(t, time.Unix(1_700_000_060, 0).Add(30*time.Minute), end)
}

func TestResolveWindow_ExplicitRangeOverridesSeed(t *testing.T) {
	opts := &baselineOpts{Window: "30m"}
	opts.From = "2023-11-14T00:00:00Z"
	opts.To = "2023-11-14T06:00:00Z"
	profile := seedProfile{HasTimeRange: true, MinStartNanos: unixNanos(1_700_000_000), MaxEndNanos: unixNanos(1_700_000_060)}
	start, end, err := opts.resolveWindow(profile, time.Now())
	require.NoError(t, err)
	wantStart, _ := dsquery.ParseTime("2023-11-14T00:00:00Z", time.Now())
	wantEnd, _ := dsquery.ParseTime("2023-11-14T06:00:00Z", time.Now())
	assert.Equal(t, wantStart, start)
	assert.Equal(t, wantEnd, end)
}

func TestResolveWindow_NoSeedTimestampsErrors(t *testing.T) {
	opts := &baselineOpts{Window: "30m"}
	_, _, err := opts.resolveWindow(seedProfile{HasTimeRange: false}, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--from/--to")
}

func TestValidate_RejectsNegativeAndInvalidWindow(t *testing.T) {
	require.NoError(t, newTestOpts("30m").Validate())

	err := newTestOpts("-1h").Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--window")

	err = newTestOpts("nonsense").Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--window")
}

func TestValidate_RejectsLimitBelowOne(t *testing.T) {
	for _, limit := range []int{0, -1} {
		opts := newTestOpts("30m")
		opts.Limit = limit

		err := opts.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--limit")
	}
}

func TestValidate_RejectsEmptyWindow(t *testing.T) {
	opts := newTestOpts("  ")

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--window")
}

func TestValidate_RejectsEmptyFilter(t *testing.T) {
	opts := newTestOpts("30m")
	opts.Filters = []string{`{ span.tenantID = "tenant-a" }`, "  "}

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--filter")
}

func TestSetup_FilterFlagIsRepeatable(t *testing.T) {
	opts := &baselineOpts{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.setup(flags)

	require.NoError(t, flags.Parse([]string{
		"--filter", `{ span.tenantID = "tenant-a" }`,
		"--filter", `{ name = "tempopb.Querier/SearchRecent" }`,
	}))
	assert.Equal(t, []string{
		`{ span.tenantID = "tenant-a" }`,
		`{ name = "tempopb.Querier/SearchRecent" }`,
	}, opts.Filters)
}

func TestBaselineCmd_ConstructsSearchRequestAndReportsTruncation(t *testing.T) {
	testutils.SandboxConfigEnv(t)

	var gotQuery, gotStart, gotEnd, gotLimit string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case "/api/datasources/proxy/uid/tempo-uid/api/v2/traces/seed-id":
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"trace": otlpTrace()}))
		case "/api/datasources/proxy/uid/tempo-uid/api/search":
			gotQuery = r.URL.Query().Get("q")
			gotStart = r.URL.Query().Get("start")
			gotEnd = r.URL.Query().Get("end")
			gotLimit = r.URL.Query().Get("limit")

			traces := make([]map[string]any, 0, 22)
			traces = append(traces, map[string]any{"traceID": "seed-id"})
			for i := 1; i <= 21; i++ {
				traces = append(traces, map[string]any{"traceID": fmt.Sprintf("candidate-%02d", i)})
			}
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"traces": traces}))
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	cfgFile := writeBaselineTestConfig(t, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
    datasources:
      tempo: tempo-uid
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	cmd := BaselineCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"baseline", "seed-id", "-o", "json"})

	require.NoError(t, root.Execute())
	assert.Equal(t,
		`{ trace:rootService = "checkout" && trace:rootName = "POST /checkout" } && { name = "POST /checkout" && span:status != error && nestedSetParent = -1 } && { resource.service.name = "postgres" }`,
		gotQuery,
	)
	assert.Equal(t, "1699998200", gotStart)
	assert.Equal(t, "1700001800", gotEnd)
	assert.Equal(t, "22", gotLimit) // --limit 20 + seed slot + truncation probe

	var result tempo.BaselineResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.Len(t, result.Candidates, 20)
	assert.Equal(t, "candidate-01", result.Candidates[0].TraceID)
	assert.Contains(t, stderr.String(), "showing first 20; more results are available")
}

func writeBaselineTestConfig(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/config.yaml"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// otlpTrace builds a minimal OTLP-shaped trace with two resources: a root span
// (no parent) in "checkout" and a child span in "postgres".
func otlpTrace() map[string]any {
	return map[string]any{
		"resourceSpans": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "checkout"}},
					},
				},
				"scopeSpans": []any{
					map[string]any{
						"spans": []any{
							map[string]any{
								"name":              "POST /checkout",
								"spanId":            "aaaa",
								"startTimeUnixNano": "1700000000000000000",
								"endTimeUnixNano":   "1700000000500000000",
							},
						},
					},
				},
			},
			map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "postgres"}},
					},
				},
				"scopeSpans": []any{
					map[string]any{
						"spans": []any{
							map[string]any{
								"name":              "SELECT",
								"spanId":            "bbbb",
								"parentSpanId":      "aaaa",
								"startTimeUnixNano": "1700000000100000000",
								"endTimeUnixNano":   "1700000000900000000",
							},
						},
					},
				},
			},
		},
	}
}

func TestParseSeedTrace(t *testing.T) {
	p := parseSeedTrace(otlpTrace())
	assert.Equal(t, "checkout", p.RootService)
	assert.Equal(t, "POST /checkout", p.RootOperation)
	assert.True(t, p.HasTimeRange)
	assert.Equal(t, uint64(1700000000000000000), p.MinStartNanos)
	assert.Equal(t, uint64(1700000000900000000), p.MaxEndNanos)
	assert.Equal(t, map[string]int{"checkout": 1, "postgres": 1}, p.ServiceSpans)
}

func TestParseSeedTrace_NoRootSpan(t *testing.T) {
	// Every span has a parent and no timestamps → no root, no time range.
	trace := map[string]any{
		"resourceSpans": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "svc"}},
					},
				},
				"scopeSpans": []any{
					map[string]any{
						"spans": []any{
							map[string]any{"name": "child", "parentSpanId": "xyz"},
						},
					},
				},
			},
		},
	}
	p := parseSeedTrace(trace)
	assert.Empty(t, p.RootService)
	assert.Empty(t, p.RootOperation)
	assert.False(t, p.HasTimeRange)
	assert.Equal(t, map[string]int{"svc": 1}, p.ServiceSpans)
}

func TestParseSeedTrace_SkipsZeroTimestamps(t *testing.T) {
	// The first span has no start timestamp (only an end); a later span carries
	// the real earliest start. MinStartNanos must reflect the real earliest
	// timestamp, not be poisoned to 0 (which would push the window back to 1970).
	trace := map[string]any{
		"resourceSpans": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "svc"}},
					},
				},
				"scopeSpans": []any{
					map[string]any{
						"spans": []any{
							// Root span missing startTimeUnixNano.
							map[string]any{"name": "root", "endTimeUnixNano": "1700000000900000000"},
							// Child carries the real earliest start.
							map[string]any{"name": "child", "parentSpanId": "x", "startTimeUnixNano": "1700000000200000000", "endTimeUnixNano": "1700000000500000000"},
						},
					},
				},
			},
		},
	}
	p := parseSeedTrace(trace)
	assert.True(t, p.HasTimeRange)
	assert.Equal(t, uint64(1700000000200000000), p.MinStartNanos)
	assert.Equal(t, uint64(1700000000900000000), p.MaxEndNanos)
}

func TestParseSeedTrace_RootElectionPrefersTimestampedSpan(t *testing.T) {
	missingStart := map[string]any{
		"name":            "missing-start-root",
		"endTimeUnixNano": "1700000000900000000",
	}
	timestamped := map[string]any{
		"name":              "timestamped-root",
		"startTimeUnixNano": "1700000000000000000",
		"endTimeUnixNano":   "1700000000500000000",
	}

	for _, tc := range []struct {
		name  string
		spans []any
	}{
		{name: "missing start before timestamped root", spans: []any{missingStart, timestamped}},
		{name: "missing start after timestamped root", spans: []any{timestamped, missingStart}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trace := map[string]any{
				"resourceSpans": []any{
					map[string]any{
						"resource": map[string]any{
							"attributes": []any{
								map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "checkout"}},
							},
						},
						"scopeSpans": []any{
							map[string]any{"spans": tc.spans},
						},
					},
				},
			}

			profile := parseSeedTrace(trace)
			assert.Equal(t, "checkout", profile.RootService)
			assert.Equal(t, "timestamped-root", profile.RootOperation)
		})
	}
}

func TestFoldTimeRange(t *testing.T) {
	var p seedProfile
	p.foldTimeRange(0, 0) // both absent → no range established
	assert.False(t, p.HasTimeRange)

	p.foldTimeRange(200, 500)
	p.foldTimeRange(0, 900) // zero start ignored; 900 extends the max
	p.foldTimeRange(100, 0) // 100 extends the min; zero end ignored
	assert.True(t, p.HasTimeRange)
	assert.Equal(t, uint64(100), p.MinStartNanos)
	assert.Equal(t, uint64(900), p.MaxEndNanos)
}

func TestBuildBaselineQuery(t *testing.T) {
	q := buildBaselineQuery("checkout", "POST /checkout", nil, nil)
	assert.Equal(t,
		`{ trace:rootService = "checkout" && trace:rootName = "POST /checkout" } && { name = "POST /checkout" && span:status != error && nestedSetParent = -1 }`,
		q,
	)
}

func TestBuildBaselineQuery_TopologyFingerprint(t *testing.T) {
	q := buildBaselineQuery("checkout", "POST /checkout", []string{"payments", "postgres"}, nil)
	assert.Equal(t,
		`{ trace:rootService = "checkout" && trace:rootName = "POST /checkout" } && { name = "POST /checkout" && span:status != error && nestedSetParent = -1 } && { resource.service.name = "payments" } && { resource.service.name = "postgres" }`,
		q,
	)
}

func TestBuildBaselineQuery_Filters(t *testing.T) {
	q := buildBaselineQuery("checkout", "POST /checkout", []string{"postgres"}, []string{
		`{ span.tenantID = "tenant-a" }`,
		`{ resource.k8s.cluster.name = "prod" }`,
	})
	assert.Equal(t,
		`{ trace:rootService = "checkout" && trace:rootName = "POST /checkout" } && { name = "POST /checkout" && span:status != error && nestedSetParent = -1 } && ({ span.tenantID = "tenant-a" }) && ({ resource.k8s.cluster.name = "prod" }) && { resource.service.name = "postgres" }`,
		q,
	)
}

func TestBuildBaselineQuery_QuotesSpecialChars(t *testing.T) {
	q := buildBaselineQuery("svc\"x", "op\\y", []string{"weird\"svc"}, nil)
	// strconv.Quote escapes embedded quotes/backslashes so the TraceQL stays valid.
	assert.Contains(t, q, `trace:rootService = "svc\"x"`)
	assert.Contains(t, q, `name = "op\\y"`)
	assert.Contains(t, q, `resource.service.name = "weird\"svc"`)
}

func TestDownstreamServices(t *testing.T) {
	seed := map[string]int{
		"checkout": 10, // root, excluded
		"payments": 5,
		"postgres": 8,
		"cache":    2,
		"":         3, // no service name, excluded
	}
	// Ordered by span count desc, name tie-break; root and "" excluded.
	assert.Equal(t, []string{"postgres", "payments", "cache"}, downstreamServices(seed, "checkout", 3))
	// n caps the result.
	assert.Equal(t, []string{"postgres", "payments"}, downstreamServices(seed, "checkout", 2))
	// Single-service trace → no downstream.
	assert.Empty(t, downstreamServices(map[string]int{"checkout": 4}, "checkout", 3))
}

func TestExcludeTrace(t *testing.T) {
	resp := &tempo.SearchResponse{Traces: []tempo.SearchTrace{
		{TraceID: "seed"},
		{TraceID: "cand1"},
		{TraceID: "cand2"},
	}}
	out := excludeTrace(resp, "seed")
	require.Len(t, out.Traces, 2)
	for _, tr := range out.Traces {
		assert.NotEqual(t, "seed", tr.TraceID)
	}
}

func TestExcludeTrace_Nil(t *testing.T) {
	assert.Nil(t, excludeTrace(nil, "seed"))
}

func TestLimitTraces(t *testing.T) {
	resp := &tempo.SearchResponse{Traces: []tempo.SearchTrace{
		{TraceID: "a"}, {TraceID: "b"}, {TraceID: "c"},
	}}

	got := limitTraces(resp, 2)
	require.Len(t, got.Traces, 2)
	assert.Equal(t, "a", got.Traces[0].TraceID)
	assert.Equal(t, "b", got.Traces[1].TraceID)

	assert.Len(t, limitTraces(resp, 5).Traces, 3) // n >= len: no-op
	assert.Len(t, limitTraces(resp, 0).Traces, 3) // n <= 0: no cap
	assert.Nil(t, limitTraces(nil, 2))            // nil is safe
}

func TestBuildBaselineResult_EmptyCandidatesSerializeAsArray(t *testing.T) {
	result := buildBaselineResult("seed-id", nil, nil, "{ }")

	payload, err := json.Marshal(result)
	require.NoError(t, err)
	assert.Contains(t, string(payload), `"candidates":[]`)
}

func TestBuildBaselineResult(t *testing.T) {
	seed := map[string]int{"checkout": 4, "postgres": 2} // 6 spans, 2 services
	resp := &tempo.SearchResponse{Traces: []tempo.SearchTrace{
		{TraceID: "exact", RootServiceName: "checkout", RootTraceName: "POST /x", DurationMs: 30,
			ServiceStats: map[string]tempo.ServiceStats{"checkout": {SpanCount: 4}, "postgres": {SpanCount: 2}}},
		{TraceID: "far", RootServiceName: "checkout", RootTraceName: "POST /x", DurationMs: 90,
			ServiceStats: map[string]tempo.ServiceStats{"checkout": {SpanCount: 40}}},
	}}

	result := buildBaselineResult("seed-id", seed, resp, "{ }")

	assert.Equal(t, "seed-id", result.SeedTraceID)
	assert.Equal(t, 6, result.SeedSpanCount)
	assert.Equal(t, 2, result.SeedServiceCount)
	require.Len(t, result.Candidates, 2)

	first := result.Candidates[0]
	assert.Equal(t, "exact", first.TraceID)
	assert.Equal(t, 6, first.SpanCount)
	assert.Equal(t, 2, first.ServiceCount)

	second := result.Candidates[1]
	assert.Equal(t, 40, second.SpanCount)
	assert.Equal(t, 1, second.ServiceCount)
}
