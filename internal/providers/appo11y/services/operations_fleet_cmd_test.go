package services //nolint:testpackage // Tests cover unexported fleet command opts/helpers.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
)

func TestFleetOperationsListOptsValidate(t *testing.T) {
	mk := func(o fleetOperationsListOpts) fleetOperationsListOpts {
		o.IO.OutputFormat = "json"
		if o.Since == "" {
			o.Since = defaultRedWindow
		}
		if o.Kind == "" {
			o.Kind = "inbound"
		}
		if o.KG.Mode == "" {
			o.KG.Mode = string(kgModeAuto)
		}
		if o.Limit == 0 {
			o.Limit = fleetOperationsDefaultLimit
		}
		return o
	}
	tests := []struct {
		name    string
		opts    fleetOperationsListOpts
		wantErr bool
	}{
		{name: "defaults ok", opts: mk(fleetOperationsListOpts{})},
		{name: "limit zero rejected", opts: func() fleetOperationsListOpts {
			o := mk(fleetOperationsListOpts{})
			o.Limit = 0
			return o
		}(), wantErr: true},
		{name: "limit negative rejected", opts: func() fleetOperationsListOpts {
			o := mk(fleetOperationsListOpts{})
			o.Limit = -1
			return o
		}(), wantErr: true},
		{name: "limit above cap rejected", opts: func() fleetOperationsListOpts {
			o := mk(fleetOperationsListOpts{})
			o.Limit = 501
			return o
		}(), wantErr: true},
		{name: "limit at cap ok", opts: func() fleetOperationsListOpts {
			o := mk(fleetOperationsListOpts{})
			o.Limit = 500
			return o
		}()},
	}
	cmd := &cobra.Command{Use: "list"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate(cmd)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestFleetOperationsListOptsValidate_LimitZeroIsRejected pins down the
// deliberate divergence from every other `list` command in this repo:
// --limit 0 means "unbounded" everywhere else, but is rejected here
// because the unbounded fleet shape is #services x #operations.
func TestFleetOperationsListOptsValidate_LimitZeroIsRejected(t *testing.T) {
	o := fleetOperationsListOpts{
		Since: defaultRedWindow, Kind: "inbound", MetricsMode: metricsModeAuto,
		Limit: 0, KG: kgFlags{Mode: string(kgModeAuto)},
	}
	o.IO.OutputFormat = "json"
	if err := o.Validate(&cobra.Command{Use: "list"}); err == nil {
		t.Error("expected --limit 0 to be rejected")
	}
}

func TestNewFleetOperationsListCommand_RegistersLimitFlag(t *testing.T) {
	cmd := newFleetOperationsListCommand(nil)
	f := cmd.Flags().Lookup("limit")
	if f == nil {
		t.Fatal("expected --limit flag to be registered")
	}
	if !strings.Contains(f.Usage, "500") {
		t.Errorf("--limit usage should document the 500 cap: %q", f.Usage)
	}
}

func TestNewOperationGetCommand_RequiresService(t *testing.T) {
	cmd := newOperationGetCommand(nil)
	cmd.SetArgs([]string{"GET /cart"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error when --service is not set")
	}
	if !strings.Contains(err.Error(), "service") {
		t.Errorf("error should mention the missing --service flag: %v", err)
	}
}

// TestAppendSpanNameMatcher_ThreadsIntoSingleServiceBuilder confirms the
// positional operation name reaches the existing single-service query
// builders as a span_name= matcher — `operations get` needs zero new
// PromQL, just this narrowing matcher on top of the per-service builders.
func TestAppendSpanNameMatcher_ThreadsIntoSingleServiceBuilder(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	matchers := appendSpanNameMatcher(nil, "GET /api/v1/carts")
	got, err := buildOperationsRateQuery(v3, "shop", "cart", "5m", []string{spanKindServer}, matchers, nil)
	if err != nil {
		t.Fatalf("build err = %v", err)
	}
	want := `sum by (span_name) (rate(traces_span_metrics_calls_total{job="shop/cart",span_kind=~"SPAN_KIND_SERVER",span_name="GET /api/v1/carts"}[5m]))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// TestAppendSpanNameMatcher_Escapes confirms an operation name with a
// PromQL-special character (a literal quote) is escaped the same way as
// every other matcher value — no injection via the positional argument.
func TestAppendSpanNameMatcher_Escapes(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	matchers := appendSpanNameMatcher(nil, `x"} or up{`)
	got, err := buildOperationsRateQuery(v3, "shop", "cart", "5m", []string{spanKindServer}, matchers, nil)
	if err != nil {
		t.Fatalf("build err = %v", err)
	}
	if !strings.Contains(got, `span_name="x\"} or up{"`) {
		t.Errorf("operation name not escaped: %s", got)
	}
}

func TestFilterFleetByNamespaceAndEnv(t *testing.T) {
	items := []FleetOperation{
		{Service: "checkout", Namespace: "billing", Operation: Operation{Labels: map[string]string{"deployment_environment": "production"}}},
		{Service: "cart", Namespace: "shop", Operation: Operation{Labels: map[string]string{"deployment_environment": "staging"}}},
	}

	if got := filterFleetByNamespaceAndEnv(items, "", ""); len(got) != 2 {
		t.Fatalf("no filter: len = %d, want 2", len(got))
	}
	if got := filterFleetByNamespaceAndEnv(items, "billing", ""); len(got) != 1 || got[0].Service != "checkout" {
		t.Fatalf("namespace filter = %+v", got)
	}
	if got := filterFleetByNamespaceAndEnv(items, "", "staging"); len(got) != 1 || got[0].Service != "cart" {
		t.Fatalf("env filter = %+v", got)
	}
}

// TestFetchFleetOperations_EnvFilterEndToEnd guards the --env fix through
// the real pipeline (query -> extractOperations -> mergeFleetOperations),
// not a hand-constructed Labels map: fleet rows are built via `sum by
// (job, span_name[, groupBy...])`, which drops deployment_environment
// unless it's explicitly part of the grouping. Without widening that
// grouping when env is set, every row's Labels would be missing the key
// filterFleetByNamespaceAndEnv reads, and --env would silently drop
// everything.
func TestFetchFleetOperations_EnvFilterEndToEnd(t *testing.T) {
	// Every fleet query (rate, error, avg, p50/p95/p99, total-time) gets the
	// same single-series Grafana dataframe response back — one job/span_name
	// pair labeled deployment_environment=production. This is the actual
	// wire shape client.Query parses (Grafana's /api/ds/query dataframe
	// contract), not the raw Prometheus /api/v1/query shape.
	const frameJSON = `{"results":{"A":{"frames":[{
		"schema":{"fields":[
			{"name":"Time","type":"time"},
			{"name":"Value","type":"number","labels":{"job":"billing/checkout","span_name":"GET /cart","deployment_environment":"production"}}
		]},
		"data":{"values":[[1700000000000],[5]]}
	}]}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(frameJSON))
	}))
	defer srv.Close()

	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}, Namespace: "stack-123"}
	client, err := prometheus.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient err = %v", err)
	}
	t.Run("env unset: default grouping is untouched", func(t *testing.T) {
		resp, err := fetchFleetOperations(context.Background(), client, "test-uid", "5m", []string{spanKindServer}, MetricsModeV3, nil, nil, 20, "")
		if err != nil {
			t.Fatalf("fetchFleetOperations err = %v", err)
		}
		if len(resp.Items) != 1 {
			t.Fatalf("len(Items) = %d, want 1", len(resp.Items))
		}
		if v, ok := resp.Items[0].Labels["deployment_environment"]; ok && v != "" {
			t.Errorf("Labels = %+v, deployment_environment must be absent when env filtering isn't requested", resp.Items[0].Labels)
		}
		if len(resp.GroupBy) != 0 {
			t.Errorf("GroupBy = %v, want empty (env labels must never leak into the display groupBy)", resp.GroupBy)
		}
	})

	t.Run("env set: row carries deployment_environment and the real filter keeps/drops correctly", func(t *testing.T) {
		resp, err := fetchFleetOperations(context.Background(), client, "test-uid", "5m", []string{spanKindServer}, MetricsModeV3, nil, nil, 20, "production")
		if err != nil {
			t.Fatalf("fetchFleetOperations err = %v", err)
		}
		if len(resp.Items) != 1 {
			t.Fatalf("len(Items) = %d, want 1", len(resp.Items))
		}
		if got := resp.Items[0].Labels["deployment_environment"]; got != "production" {
			t.Fatalf("Labels[deployment_environment] = %q, want %q — the real query/extract pipeline never populated it", got, "production")
		}
		if len(resp.GroupBy) != 0 {
			t.Errorf("GroupBy = %v, want empty (env labels used internally for filtering must not leak into the display groupBy)", resp.GroupBy)
		}

		kept := filterFleetByNamespaceAndEnv(resp.Items, "", "production")
		if len(kept) != 1 {
			t.Errorf("filtering by the matching env: len = %d, want 1", len(kept))
		}
		dropped := filterFleetByNamespaceAndEnv(resp.Items, "", "staging")
		if len(dropped) != 0 {
			t.Errorf("filtering by a non-matching env: len = %d, want 0", len(dropped))
		}
	})
}

// TestFleetOperationsList_KGAnnotationIsOneBulkCall guards the fix for the
// "up to ~1000 serial HTTP calls" finding: `operations list` must annotate
// rows via one bulk index() call (like `services list` already does), not
// a per-row lookup() — each lookup() costs two serial round trips with no
// caching, which at --limit 500 would turn a sub-second command into a
// multi-minute one.
func TestFleetOperationsList_KGAnnotationIsOneBulkCall(t *testing.T) {
	var searchHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == activationEndpoint:
			w.WriteHeader(http.StatusOK)
		case strings.Contains(r.URL.Path, "v1/stack/status"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"enabled":true,"status":"complete"}`))
		case strings.Contains(r.URL.Path, "v1/search"):
			searchHits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"entities":[{"type":"Service","name":"checkout"}],"lastPage":true}}`))
		case r.URL.Path == "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{
				"schema":{"fields":[
					{"name":"Time","type":"time"},
					{"name":"Value","type":"number","labels":{"job":"billing/checkout","span_name":"GET /cart"}}
				]},
				"data":{"values":[[1700000000000],[5]]}
			}]}}}`))
		}
	}))
	defer srv.Close()

	loader := newActivationTestLoader(t, srv.URL)
	root := OperationsCommands(loader)
	root.SilenceUsage = true
	root.SilenceErrors = true
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(io.Discard)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"list", "-d", "test-uid", "-o", "json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute err = %v, stdout = %s", err, stdout.String())
	}

	if searchHits != 1 {
		t.Errorf("KG search endpoint hit %d times, want exactly 1 (one bulk index() call, not one lookup() per row)", searchHits)
	}
	if !strings.Contains(stdout.String(), `"kg"`) {
		t.Errorf("expected a kg annotation in the output: %s", stdout.String())
	}
}

// TestFleetOperationsList_GroupByLimitTruncates guards the fix for the
// "--limit doesn't bound rows under --group-by" finding: the server-side
// topk(limit, ...) ranks (job, span_name) pairs, but "and on (job,
// span_name)" fans each ranked pair out into one row per distinct
// group-label value — 2 ranked pairs x 3 clusters returns 6 rows even
// though --limit asked for 3. The client-side truncation added to
// runFleetOperationsList must cap the final row count regardless.
func TestFleetOperationsList_GroupByLimitTruncates(t *testing.T) {
	// Every fleet query gets the same 6-series response back: 2
	// (job, span_name) pairs, each split across 3 k8s_cluster_name values —
	// the --group-by fanout that used to defeat --limit.
	frames := make([]string, 0, 6)
	i := 0
	for _, job := range []string{"billing/checkout", "billing/cart"} {
		for _, cluster := range []string{"a", "b", "c"} {
			i++
			frames = append(frames, fmt.Sprintf(`{
				"schema":{"fields":[
					{"name":"Time","type":"time"},
					{"name":"Value","type":"number","labels":{"job":%q,"span_name":"GET /x","k8s_cluster_name":%q}}
				]},
				"data":{"values":[[1700000000000],[%d]]}
			}`, job, cluster, i))
		}
	}
	body := fmt.Sprintf(`{"results":{"A":{"frames":[%s]}}}`, strings.Join(frames, ","))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == activationEndpoint:
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}
	}))
	defer srv.Close()

	loader := newActivationTestLoader(t, srv.URL)
	root := OperationsCommands(loader)
	root.SilenceUsage = true
	root.SilenceErrors = true
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"list", "-d", "test-uid", "-o", "json", "--group-by", "k8s_cluster_name", "--limit", "3", "--kg", "off"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute err = %v, stdout = %s, stderr = %s", err, stdout.String(), stderr.String())
	}

	var resp FleetOperationsResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v\n%s", err, stdout.String())
	}
	if len(resp.Items) != 3 {
		t.Errorf("len(Items) = %d, want 3 (the fanout produced 6 rows for 2 ranked pairs x 3 clusters)", len(resp.Items))
	}
	if !strings.Contains(stderr.String(), "showing top 3 ranked operations") {
		t.Errorf("stderr missing the truncation hint: %s", stderr.String())
	}
}
