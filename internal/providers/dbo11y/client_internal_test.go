package dbo11y

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

func testClient(t *testing.T, handler http.HandlerFunc) *apiClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := internalconfig.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "stacks-12345",
	}
	client, err := newAPIClient(cfg)
	if err != nil {
		t.Fatalf("newAPIClient() err = %v", err)
	}
	return client
}

type sample struct {
	Name string `json:"name"`
}

func TestGet_Success(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, apiBasePath+"/widgets"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("datasource"), "prom-uid"; got != want {
			t.Errorf("datasource query param = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"name":"widget-1"}}`)) //nolint:errcheck // test helper
	})

	got, err := apiGet[sample](context.Background(), client, "/widgets", url.Values{"datasource": {"prom-uid"}})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Name != "widget-1" {
		t.Errorf("got %+v", got)
	}
}

func TestGet_ArrayData(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"success","data":[{"name":"a"},{"name":"b"}]}`)) //nolint:errcheck // test helper
	})

	got, err := apiGet[[]sample](context.Background(), client, "/widgets", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Errorf("got %+v", got)
	}
}

func TestGet_StructuredError(t *testing.T) {
	// Real shape confirmed against the dbo11y-dev stack via `gcx api`.
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"status":"error","error":{"code":"instance_not_found","message":"instance not found"}}`)) //nolint:errcheck // test helper
	})

	_, err := apiGet[sample](context.Background(), client, "/widgets/missing", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *apiError, got %T", err)
	}
	if apiErr.Code != "instance_not_found" {
		t.Errorf("Code = %q, want %q", apiErr.Code, "instance_not_found")
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusNotFound)
	}
	if apiErr.APIServiceName() != "Database Observability" {
		t.Errorf("APIServiceName() = %q", apiErr.APIServiceName())
	}
	if apiErr.APIUserMessage() != "instance not found" {
		t.Errorf("APIUserMessage() = %q", apiErr.APIUserMessage())
	}
	if apiErr.HTTPStatusCode() != http.StatusNotFound {
		t.Errorf("HTTPStatusCode() = %d", apiErr.HTTPStatusCode())
	}
}

func TestGet_UnstructuredError(t *testing.T) {
	// A body that doesn't match the API's own error envelope still produces a
	// usable *apiError via the raw-body fallback.
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`plain text failure`)) //nolint:errcheck // test helper
	})

	_, err := apiGet[sample](context.Background(), client, "/widgets", nil)
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *apiError, got %T (%v)", err, err)
	}
	if apiErr.Code != "" {
		t.Errorf("Code = %q, want empty for an unstructured body", apiErr.Code)
	}
	if apiErr.APIUserMessage() != "plain text failure" {
		t.Errorf("APIUserMessage() = %q", apiErr.APIUserMessage())
	}
}

func TestGet_UnstructuredError_TrimsTrailingWhitespace(t *testing.T) {
	// Go's net/http router emits exactly this shape for an unknown
	// 404 or a 405 — the trailing newline shouldn't leak into the rendered
	// error message.
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 page not found\n")) //nolint:errcheck // test helper
	})

	_, err := apiGet[sample](context.Background(), client, "/widgets", nil)
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *apiError, got %T (%v)", err, err)
	}
	if apiErr.APIUserMessage() != "404 page not found" {
		t.Errorf("APIUserMessage() = %q, want trailing whitespace trimmed", apiErr.APIUserMessage())
	}
}

func TestGet_EmptyErrorBody(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, err := apiGet[sample](context.Background(), client, "/widgets", nil)
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *apiError, got %T", err)
	}
	if apiErr.Error() == "" {
		t.Error("expected a non-empty error message even with no body")
	}
}

func TestFetchInstances(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/plugins/grafana-dbo11y-app/resources/api/v1/instances"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("datasource"), "prom-uid"; got != want {
			t.Errorf("datasource = %q, want %q", got, want)
		}
		w.Write([]byte(`{"status":"success","data":[{"server_id":"s1","instance":"db-1","engine":"postgres"}]}`)) //nolint:errcheck // test helper
	})

	got, err := fetchInstances(context.Background(), client, "prom-uid", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0].ServerID != "s1" || got[0].Engine != "postgres" {
		t.Errorf("got %+v", got)
	}
	// The API's single `instance` field is the Prometheus instance label, and
	// the only identifier it returns — the output contract reports it as both
	// name and host.
	if got[0].Name != "db-1" || got[0].Host != "db-1" {
		t.Errorf("name = %q, host = %q, want both %q", got[0].Name, got[0].Host, "db-1")
	}
}

func TestFetchInstances_NilDataNormalisesToEmptySlice(t *testing.T) {
	// `null` and `[]` must not both be reachable in -o json: an unfiltered
	// empty fleet has to encode the same way a filtered-to-nothing one does.
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"success","data":null}`)) //nolint:errcheck // test helper
	})
	got, err := fetchInstances(context.Background(), client, "prom-uid", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil {
		t.Fatal("expected an empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected no items, got %+v", got)
	}
}

func TestFetchInstanceDetail(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/plugins/grafana-dbo11y-app/resources/api/v1/instances/s1"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		w.Write([]byte(`{"status":"success","data":{"server_id":"s1","instance":"db-1","engine":"postgres","metrics":{"qps":1.5,"avg_latency_ms":2.5,"error_rate":0.01,"cpu_percent":30}}}`)) //nolint:errcheck // test helper
	})

	got, err := fetchInstanceDetail(context.Background(), client, "prom-uid", "s1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.ServerID != "s1" || got.Metrics == nil || derefFloat(got.Metrics.QPS) != 1.5 {
		t.Errorf("got %+v", got)
	}
}

func TestFetchInstanceDetail_OmittedMetricsFieldsStayNil(t *testing.T) {
	// A sql_server instance on a recognised provider: RED fields are not
	// modelled (omitted), cpu_percent is (present). Distinguishing "omitted"
	// from "reported as 0" is the whole point of the *float64 fields.
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"success","data":{"server_id":"s1","instance":"db-1","engine":"sql_server","metrics":{"cpu_percent":12.5}}}`)) //nolint:errcheck // test helper
	})

	got, err := fetchInstanceDetail(context.Background(), client, "prom-uid", "s1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Metrics == nil {
		t.Fatal("expected Metrics to be present (cpu_percent applies)")
	}
	if got.Metrics.QPS != nil || got.Metrics.AvgLatencyMs != nil || got.Metrics.ErrorRate != nil {
		t.Errorf("expected RED fields to stay nil for an unmodelled engine, got %+v", got.Metrics)
	}
	if derefFloat(got.Metrics.CPUPercent) != 12.5 {
		t.Errorf("expected cpu_percent = 12.5, got %+v", got.Metrics.CPUPercent)
	}
}

func TestFetchTopQueries(t *testing.T) {
	var gotQuery url.Values
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/plugins/grafana-dbo11y-app/resources/api/v1/instances/s1/queries"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		gotQuery = r.URL.Query()
		w.Write([]byte(`{"status":"success","data":{"server_id":"s1","engine":"postgres","queries":[{"query_id":"q1","database":"payments","query_text":"SELECT 1","calls":10,"qps":0.5,"avg_latency_ms":3,"db_load":0.1,"rows_examined":45230}]}}`)) //nolint:errcheck // test helper
	})

	got, err := fetchTopQueries(context.Background(), client, "prom-uid", "loki-uid", "s1", "5m", 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0].QueryID != "q1" || got[0].QueryText != "SELECT 1" {
		t.Errorf("got %+v", got)
	}
	// Several fields are renamed or rescaled from what the API returns.
	q := got[0]
	if q.Datname != "payments" {
		t.Errorf("datname = %q, want %q (from the API's database)", q.Datname, "payments")
	}
	if q.CallsPerSecond != 0.5 {
		t.Errorf("calls_per_second = %v, want 0.5 (from the API's qps)", q.CallsPerSecond)
	}
	if q.TimePerSecond != 0.1 {
		t.Errorf("time_per_second = %v, want 0.1 (from the API's db_load)", q.TimePerSecond)
	}
	// 3ms, reported in seconds.
	if !q.HasMeanLatency || q.MeanLatencySeconds != 0.003 {
		t.Errorf("mean_latency_seconds = %v (has %v), want 0.003 (from avg_latency_ms 3)", q.MeanLatencySeconds, q.HasMeanLatency)
	}
	// 45230 rows over 10 calls.
	if !q.HasRowsPerCall || q.RowsPerCall != 4523 {
		t.Errorf("rows_per_call = %v (has %v), want 4523 (rows_examined over calls)", q.RowsPerCall, q.HasRowsPerCall)
	}
	if q.RowsExamined == nil || derefFloat(q.RowsExamined) != 45230 {
		t.Errorf("rows_examined = %v, want 45230", q.RowsExamined)
	}
	if gotQuery.Get("limit") != "10" {
		t.Errorf("limit query param = %q, want %q", gotQuery.Get("limit"), "10")
	}
	if gotQuery.Get("loki") != "loki-uid" {
		t.Errorf("loki query param = %q, want %q", gotQuery.Get("loki"), "loki-uid")
	}
	if gotQuery.Get("from") == "" || gotQuery.Get("to") == "" {
		t.Error("expected from/to query params to be set")
	}
}

func TestFetchTopQueries_EmptyLokiUIDOmitsParam(t *testing.T) {
	// Empty means "let the API fall back to its own default
	// (grafanacloud-logs)" — the param must be left off entirely, not sent
	// as ?loki=.
	var gotQuery url.Values
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte(`{"status":"success","data":{"queries":[]}}`)) //nolint:errcheck // test helper
	})
	if _, err := fetchTopQueries(context.Background(), client, "prom-uid", "", "s1", "5m", 10); err != nil {
		t.Fatalf("err = %v", err)
	}
	if gotQuery.Has("loki") {
		t.Errorf("expected no loki query param when lokiDatasourceUID is empty, got %q", gotQuery.Get("loki"))
	}
}

// derefFloat dereferences an optional metric value for comparison in tests,
// panicking on nil (a nil is always asserted separately first).
func derefFloat(v *float64) float64 {
	return *v
}

func TestFetchTopQueries_LimitClamping(t *testing.T) {
	cases := []struct {
		name      string
		limit     int
		wantLimit string
	}{
		{"zero clamps to max", 0, "100"},
		{"negative clamps to max", -5, "100"},
		{"over-max clamps to max", 500, "100"},
		{"within range passes through", 25, "25"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotLimit string
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotLimit = r.URL.Query().Get("limit")
				w.Write([]byte(`{"status":"success","data":{"queries":[]}}`)) //nolint:errcheck // test helper
			})
			if _, err := fetchTopQueries(context.Background(), client, "prom-uid", "", "s1", "5m", c.limit); err != nil {
				t.Fatalf("err = %v", err)
			}
			if gotLimit != c.wantLimit {
				t.Errorf("limit = %q, want %q", gotLimit, c.wantLimit)
			}
		})
	}
}

func TestFetchTopQueries_InvalidSince(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make a request when --since fails to parse")
	})
	if _, err := fetchTopQueries(context.Background(), client, "prom-uid", "", "s1", "not-a-duration", 10); err == nil {
		t.Fatal("expected an error for an invalid --since value")
	}
}

func TestFetchWaitEvents_SendsRequestedWindow(t *testing.T) {
	// The route falls back to its own five-minute default when from/to are
	// absent, so the caller's --since has to reach it. Left off, the breakdown
	// silently covers a different span from the top queries printed beside it.
	var gotQuery url.Values
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte(`{"status":"success","data":{"wait_events":[{"type":"IO Wait","wait_seconds":12}]}}`)) //nolint:errcheck // test helper
	})
	got, err := fetchWaitEvents(context.Background(), client, "prom-uid", "loki-uid", "s1", "1h")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0].Type != "IO Wait" || got[0].WaitSeconds != 12 {
		t.Errorf("got %+v", got)
	}
	from, err := time.Parse(time.RFC3339, gotQuery.Get("from"))
	if err != nil {
		t.Fatalf("from = %q: %v", gotQuery.Get("from"), err)
	}
	to, err := time.Parse(time.RFC3339, gotQuery.Get("to"))
	if err != nil {
		t.Fatalf("to = %q: %v", gotQuery.Get("to"), err)
	}
	if width := to.Sub(from); width != time.Hour {
		t.Errorf("window width = %v, want 1h (the requested --since)", width)
	}
	if gotQuery.Get("loki") != "loki-uid" {
		t.Errorf("loki query param = %q, want %q", gotQuery.Get("loki"), "loki-uid")
	}
}

func TestFetchWaitEvents_InvalidSince(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make a request when --since fails to parse")
	})
	if _, err := fetchWaitEvents(context.Background(), client, "prom-uid", "", "s1", "not-a-duration"); err == nil {
		t.Fatal("expected an error for an invalid --since value")
	}
}

func TestFetchInstances_PushesFiltersAsLabelMatchers(t *testing.T) {
	// Any parameter other than `datasource` is an exact-match label matcher on
	// the inventory metric, so filters go on the wire rather than being
	// applied after the fetch.
	var gotQuery url.Values
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte(`{"status":"success","data":[]}`)) //nolint:errcheck // test helper
	})

	filters := url.Values{"engine": {"postgres"}, "deployment_environment": {"prod"}}
	if _, err := fetchInstances(context.Background(), client, "prom-uid", filters); err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := gotQuery.Get("engine"); got != "postgres" {
		t.Errorf("engine param = %q, want %q", got, "postgres")
	}
	if got := gotQuery.Get("deployment_environment"); got != "prod" {
		t.Errorf("deployment_environment param = %q, want %q", got, "prod")
	}
	if got := gotQuery.Get("datasource"); got != "prom-uid" {
		t.Errorf("datasource param = %q, want it preserved", got)
	}
}

func TestFetchInstances_FilterCannotShadowDatasource(t *testing.T) {
	// A matcher labelled "datasource" would otherwise redirect the whole
	// request at another datasource.
	var gotQuery url.Values
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte(`{"status":"success","data":[]}`)) //nolint:errcheck // test helper
	})

	filters := url.Values{"datasource": {"someone-elses-uid"}}
	if _, err := fetchInstances(context.Background(), client, "prom-uid", filters); err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := gotQuery["datasource"]; len(got) != 1 || got[0] != "prom-uid" {
		t.Errorf("datasource = %v, want only the resolved UID", got)
	}
}

func TestFetchInstances_FlattensCloudProvider(t *testing.T) {
	// The API nests provider identity under whichever of aws/azure/gcp
	// applies; the output reports four flat provider_* fields. Azure is the
	// awkward one: it has no region, so resource_group stands in.
	cases := []struct {
		name                        string
		block                       string
		wantProvider, wantRegion    string
		wantAccount, wantInstanceID string
	}{
		{
			"aws", `{"aws":{"account_id":"111","db_instance_id":"rds-1","region":"us-east-2"}}`,
			"aws", "us-east-2", "111", "rds-1",
		},
		{
			"azure", `{"azure":{"db_instance_id":"sql-2","resource_group":"rg-1","subscription_id":"222"}}`,
			"azure", "rg-1", "222", "sql-2",
		},
		{
			"gcp", `{"gcp":{"db_instance_id":"cs-3","project_id":"proj","region":"us-central1"}}`,
			"gcp", "us-central1", "proj", "cs-3",
		},
		{"self-hosted", `{}`, "", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{"status":"success","data":[{"server_id":"s1","instance":"db","cloud_provider":` + c.block + `}]}`)) //nolint:errcheck // test helper
			})
			got, err := fetchInstances(context.Background(), client, "prom-uid", nil)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d instances", len(got))
			}
			i := got[0]
			if i.ProviderName != c.wantProvider || i.ProviderRegion != c.wantRegion ||
				i.ProviderAccount != c.wantAccount || i.InstanceIdentifier != c.wantInstanceID {
				t.Errorf("provider fields = %q/%q/%q/%q, want %q/%q/%q/%q",
					i.ProviderName, i.ProviderRegion, i.ProviderAccount, i.InstanceIdentifier,
					c.wantProvider, c.wantRegion, c.wantAccount, c.wantInstanceID)
			}
		})
	}
}
