package instances //nolint:testpackage // Tests cover unexported builders and parsers.

import (
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
)

func TestBuildConnectionInfoQuery(t *testing.T) {
	got, err := buildConnectionInfoQuery(nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `database_observability_connection_info`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	got, err = buildConnectionInfoQuery([]Matcher{{Label: "service_name", Op: "=", Value: "quickpizza-db"}})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want = `database_observability_connection_info{service_name="quickpizza-db"}`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildConnectionsByStateQuery(t *testing.T) {
	got, err := buildConnectionsByStateQuery("quickpizza-db", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `sum by (state) (pg_stat_activity_count{service_name="quickpizza-db"})`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildWaitEventsQuery(t *testing.T) {
	got, err := buildWaitEventsQuery("quickpizza-db", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `sum by (wait_event_type, wait_event) (pg_stat_activity_count{service_name="quickpizza-db",wait_event!=""})`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildLongestTxQuery(t *testing.T) {
	got, err := buildLongestTxQuery("quickpizza-db", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `max(pg_stat_activity_max_tx_duration{service_name="quickpizza-db"})`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildTopQueriesRateQuery(t *testing.T) {
	got, err := buildTopQueriesRateQuery(pgStatStatementsCalls, "quickpizza-db", "5m", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `sum by (queryid, datname) (rate(pg_stat_statements_calls_total{service_name="quickpizza-db"}[5m]))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildTopQueriesRateQuery_RequiresName(t *testing.T) {
	if _, err := buildTopQueriesRateQuery(pgStatStatementsCalls, "", "5m", nil); err == nil {
		t.Fatal("expected error for empty instance name")
	}
}

func TestParseFilter(t *testing.T) {
	cases := []struct {
		raw  string
		want Matcher
	}{
		{`engine=postgres`, Matcher{Label: "engine", Op: "=", Value: "postgres"}},
		{`engine!=mysql`, Matcher{Label: "engine", Op: "!=", Value: "mysql"}},
		{`datname=~"pay.*"`, Matcher{Label: "datname", Op: "=~", Value: "pay.*"}},
	}
	for _, c := range cases {
		got, err := parseFilter(c.raw)
		if err != nil {
			t.Fatalf("parseFilter(%q) err = %v", c.raw, err)
		}
		if got != c.want {
			t.Errorf("parseFilter(%q) = %+v, want %+v", c.raw, got, c.want)
		}
	}
	if _, err := parseFilter("not-a-filter"); err == nil {
		t.Fatal("expected error for malformed filter")
	}
}

func sampleResponse(samples ...map[string]any) *prometheus.QueryResponse {
	resp := &prometheus.QueryResponse{}
	for _, s := range samples {
		metric, _ := s["metric"].(map[string]string)
		value, _ := s["value"].([]any)
		resp.Data.Result = append(resp.Data.Result, prometheus.Sample{Metric: metric, Value: value})
	}
	return resp
}

func TestParseInstancesResponse(t *testing.T) {
	resp := sampleResponse(map[string]any{
		"metric": map[string]string{
			"__name__":               "database_observability_connection_info",
			"service_name":           "quickpizza-db",
			"service_namespace":      "quickpizza",
			"engine":                 "postgres",
			"engine_version":         "18.1",
			"deployment_environment": "development",
			"instance":               "postgres-quickpizza",
			"db_instance_identifier": "unknown",
			"provider_name":          "unknown",
			"provider_account":       "unknown",
			"provider_region":        "unknown",
		},
		"value": []any{float64(1755000000), "1"},
	})

	items, err := parseInstancesResponse(resp)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	got := items[0]
	if got.Name != "quickpizza-db" || got.Namespace != "quickpizza" || got.Engine != "postgres" || got.EngineVersion != "18.1" {
		t.Errorf("got %+v", got)
	}
	if got.ProviderName != "" || got.InstanceIdentifier != "" || got.ProviderRegion != "" {
		t.Errorf("expected \"unknown\" provider fields to map to empty string, got %+v", got)
	}
	for _, promotedKey := range []string{"engine", "engine_version", "deployment_environment", "instance", "db_instance_identifier", "provider_name", "provider_account", "provider_region", "service_namespace"} {
		if _, dup := got.Labels[promotedKey]; dup {
			t.Errorf("expected label %q to be promoted to a typed field, not duplicated in Labels: %+v", promotedKey, got.Labels)
		}
	}
}

func TestParseInstancesResponse_SkipsSamplesWithoutServiceName(t *testing.T) {
	resp := sampleResponse(map[string]any{
		"metric": map[string]string{"__name__": "database_observability_connection_info"},
		"value":  []any{float64(1755000000), "1"},
	})
	items, err := parseInstancesResponse(resp)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(items))
	}
}

func TestSampleScalar_RejectsNaNAndInf(t *testing.T) {
	// postgres_exporter maps SQL NULL to NaN (e.g. pg_stat_activity_max_tx_duration
	// with no open transaction), which must read as "no data", not a real 0/NaN
	// value — a NaN reaching the JSON encoder fails the whole command.
	cases := []string{"NaN", "+Inf", "-Inf"}
	for _, v := range cases {
		sample := prometheus.Sample{Value: []any{float64(0), v}}
		if _, ok := sampleScalar(sample); ok {
			t.Errorf("sampleScalar(%q) = ok, want !ok", v)
		}
	}
}

func TestParseConnectionsByState(t *testing.T) {
	resp := sampleResponse(
		map[string]any{"metric": map[string]string{"state": "active"}, "value": []any{float64(0), "3"}},
		map[string]any{"metric": map[string]string{"state": "idle"}, "value": []any{float64(0), "12"}},
	)
	got := parseConnectionsByState(resp)
	if len(got) != 2 || got[0].State != "idle" || got[0].Count != 12 {
		t.Errorf("got %+v", got)
	}
}

func TestParseWaitEvents(t *testing.T) {
	resp := sampleResponse(
		map[string]any{"metric": map[string]string{"wait_event_type": "Lock", "wait_event": "relation"}, "value": []any{float64(0), "2"}},
	)
	got := parseWaitEvents(resp)
	if len(got) != 1 || got[0].Type != "Lock" || got[0].Event != "relation" || got[0].Count != 2 {
		t.Errorf("got %+v", got)
	}
}

func TestMergeTopQueries(t *testing.T) {
	calls := map[queryKey]float64{
		{queryID: "1", datname: "db"}: 10,
		{queryID: "2", datname: "db"}: 1,
	}
	seconds := map[queryKey]float64{
		{queryID: "1", datname: "db"}: 1,   // 0.1s mean, but low time share
		{queryID: "2", datname: "db"}: 0.9, // 0.9s mean, high time share despite fewer calls
	}
	rows := map[queryKey]float64{
		{queryID: "1", datname: "db"}: 100,
	}

	got, truncated := mergeTopQueries(calls, seconds, rows, 0)
	if truncated {
		t.Error("expected truncated = false when limit is 0 (unlimited)")
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	// query 2 has higher time share (0.9 > 1.0 is false... recompute: q1 time=1, q2 time=0.9)
	if got[0].QueryID != "1" {
		t.Errorf("expected query 1 (time share 1.0) ranked first, got %+v", got)
	}
	if !got[0].HasMeanLatency || got[0].MeanLatencySeconds != 0.1 {
		t.Errorf("expected mean latency 0.1s for query 1, got %+v", got[0])
	}
	if !got[0].HasRowsPerCall || got[0].RowsPerCall != 10 {
		t.Errorf("expected 10 rows/call for query 1, got %+v", got[0])
	}
	if got[1].HasRowsPerCall {
		t.Errorf("query 2 has no rows sample, expected HasRowsPerCall=false, got %+v", got[1])
	}
}

func TestMergeTopQueries_Limit(t *testing.T) {
	calls := map[queryKey]float64{
		{queryID: "1"}: 1,
		{queryID: "2"}: 1,
		{queryID: "3"}: 1,
	}
	seconds := map[queryKey]float64{
		{queryID: "1"}: 3,
		{queryID: "2"}: 2,
		{queryID: "3"}: 1,
	}
	got, truncated := mergeTopQueries(calls, seconds, nil, 2)
	if !truncated {
		t.Error("expected truncated = true when 3 rows exceed limit 2")
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].QueryID != "1" || got[1].QueryID != "2" {
		t.Errorf("expected top-2 by time share [1,2], got %+v", got)
	}
}
