package dbo11y

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestGetOpts_Validate_Top(t *testing.T) {
	cmd := &cobra.Command{Use: "get"}
	cases := []struct {
		name    string
		top     int
		wantErr bool
	}{
		{"default is valid", defaultTopQueriesLimit, false},
		{"zero is valid", 0, false},
		{"negative is invalid", -1, true},
		// The API caps at 100 and the client clamps to it, so a larger value
		// is honoured as "give me the maximum" rather than rejected.
		{"at the API maximum is valid", maxTopQueries, false},
		{"above the API maximum is clamped, not rejected", 500, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := &getOpts{}
			opts.setup(pflag.NewFlagSet("test", pflag.ContinueOnError))
			opts.Top = c.top
			if err := opts.Validate(cmd); (err != nil) != c.wantErr {
				t.Errorf("Validate() with --top %d: err = %v, wantErr = %v", c.top, err, c.wantErr)
			}
		})
	}
}

func TestGetOpts_Validate_Since(t *testing.T) {
	cmd := &cobra.Command{Use: "get"}
	cases := []struct {
		name    string
		since   string
		wantErr bool
	}{
		{"positive duration is valid", "5m", false},
		{"empty is invalid", "", true},
		{"unparseable is invalid", "not-a-duration", true},
		{"zero is invalid", "0s", true},
		{"negative is invalid", "-5m", true},
		// The queries route enforces no minimum width — only from < to — so a
		// sub-minute value is accepted rather than rejected on the strength of
		// a floor that route does not have.
		{"sub-minute is valid", "30s", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := &getOpts{}
			opts.setup(pflag.NewFlagSet("test", pflag.ContinueOnError))
			opts.Since = c.since
			if err := opts.Validate(cmd); (err != nil) != c.wantErr {
				t.Errorf("Validate() with --since %q: err = %v, wantErr = %v", c.since, err, c.wantErr)
			}
		})
	}
}

func TestFormatSeconds(t *testing.T) {
	cases := []struct {
		name    string
		seconds float64
		has     bool
		want    string
	}{
		{"not reported", 0, false, "-"},
		{"microseconds", 0.0000006, true, "1µs"},
		{"milliseconds", 0.0025, true, "2.50ms"},
		{"seconds", 8.10119, true, "8.101s"},
		{"reported zero is not a dash", 0, true, "0µs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatSeconds(c.seconds, c.has); got != c.want {
				t.Errorf("formatSeconds(%v, %v) = %q, want %q", c.seconds, c.has, got, c.want)
			}
		})
	}
}

func TestSingleLineTruncate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"empty", "", 10, "-"},
		{"short", "SELECT 1", 20, "SELECT 1"},
		{"collapses whitespace", "SELECT\n\t1\n\tFROM t", 40, "SELECT 1 FROM t"},
		{"truncates long text", strings.Repeat("a", 10), 5, "aaaaa..."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := singleLineTruncate(c.in, c.max); got != c.want {
				t.Errorf("singleLineTruncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
			}
		})
	}
}

func TestNextTopSuggestion(t *testing.T) {
	// The suggested command must never be one Validate or the API rejects.
	cases := []struct {
		top  int
		want int
	}{
		{10, 20},
		{49, 98},
		{50, maxTopQueries},
		{maxTopQueries, maxTopQueries},
		{0, maxTopQueries},
	}
	for _, c := range cases {
		if got := nextTopSuggestion(c.top); got != c.want {
			t.Errorf("nextTopSuggestion(%d) = %d, want %d", c.top, got, c.want)
		}
		if got := nextTopSuggestion(c.top); got > maxTopQueries {
			t.Errorf("nextTopSuggestion(%d) = %d, above the API maximum", c.top, got)
		}
	}
}

func TestInstanceDetailCodec_Encode(t *testing.T) {
	detail := &InstanceDetail{
		Instance: Instance{
			ServerID:       "s1",
			Name:           "dev-postgres",
			Host:           "dev-postgres:5432",
			Engine:         "postgres",
			EngineVersion:  "17.9",
			ProviderName:   "aws",
			ProviderRegion: "us-east-2",
			Metrics:        &Metrics{QPS: new(11.58), AvgLatencyMs: new(11.53), ErrorRate: new(0.0007), CPUPercent: new(30.27)},
		},
		Window: "5m",
		TopQueries: []TopQuery{
			{QueryID: "q1", Datname: "books_store", QueryText: "SELECT * FROM rentals", CallsPerSecond: 0.6, TimePerSecond: 2.6, MeanLatencySeconds: 4.31033, HasMeanLatency: true},
		},
	}

	var buf bytes.Buffer
	if err := (&instanceDetailCodec{}).Encode(&buf, detail); err != nil {
		t.Fatalf("Encode() err = %v", err)
	}
	out := buf.String()

	// The describe block, the RED block and the top-queries headers.
	for _, want := range []string{"Name:", "dev-postgres", "Engine:", "postgres", "Version:", "17.9", "Provider:", "aws", "QPS:", "11.580", "CPU:", "30.3%", "QUERYID", "DATNAME", "CALLS/S", "TIME/S", "MEAN LATENCY", "books_store"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	for _, notWant := range []string{"Region:", "Server ID:", "ERROR RATE", "ROWS EXAMINED", "QUERY TEXT", "ROWS/CALL"} {
		if strings.Contains(out, notWant) {
			t.Errorf("narrow format should not show %q (wide-only), got:\n%s", notWant, out)
		}
	}
}

func TestInstanceDetailCodec_Encode_Wide(t *testing.T) {
	detail := &InstanceDetail{
		Instance: Instance{
			ServerID:           "s1",
			Name:               "db-1",
			Host:               "db-1:5432",
			ProviderName:       "aws",
			ProviderRegion:     "us-east-2",
			InstanceIdentifier: "rds-1",
		},
		Window: "5m",
	}

	var buf bytes.Buffer
	if err := (&instanceDetailCodec{Wide: true}).Encode(&buf, detail); err != nil {
		t.Fatalf("Encode() err = %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Region:", "us-east-2", "Host:", "db-1:5432", "Identifier:", "rds-1", "Server ID:", "s1"} {
		if !strings.Contains(out, want) {
			t.Errorf("wide format missing %q, got:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "No query activity found") {
		t.Errorf("expected the no-activity message for an empty TopQueries, got:\n%s", out)
	}
}

func TestInstanceDetailCodec_Encode_WideQueryColumns(t *testing.T) {
	// The new per-query data sits in the wide view. Each is absent for one
	// engine — RowsExamined for sql_server, ErrorRate for postgres — so both
	// must render "-" rather than 0 on a row that does not report them.
	detail := &InstanceDetail{
		Instance: Instance{Name: "db-1"},
		Window:   "5m",
		TopQueries: []TopQuery{
			{QueryID: "d1", Datname: "shop", TimePerSecond: 0.5, RowsPerCall: 12, HasRowsPerCall: true, RowsExamined: new(45230.0), ErrorRate: new(0.002), QueryText: "SELECT 1"},
			// Every other optional cell is populated, so rows_examined and
			// error_rate are the only "-" cells this row can produce.
			{QueryID: "d2", Datname: "shop", TimePerSecond: 0.1, MeanLatencySeconds: 0.002, HasMeanLatency: true, RowsPerCall: 3, HasRowsPerCall: true, QueryText: "SELECT 2"},
		},
	}

	var narrow bytes.Buffer
	if err := (&instanceDetailCodec{}).Encode(&narrow, detail); err != nil {
		t.Fatalf("Encode() err = %v", err)
	}
	for _, notWant := range []string{"ROWS EXAMINED", "ERROR RATE", "QUERY TEXT"} {
		if strings.Contains(narrow.String(), notWant) {
			t.Errorf("%q must not appear in the narrow table, got:\n%s", notWant, narrow.String())
		}
	}

	var buf bytes.Buffer
	if err := (&instanceDetailCodec{Wide: true}).Encode(&buf, detail); err != nil {
		t.Fatalf("Encode() err = %v", err)
	}
	out := buf.String()
	for _, want := range []string{"ROWS/CALL", "ROWS EXAMINED", "ERROR RATE", "QUERY TEXT", "45230", "0.200%", "SELECT 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("wide output missing %q\n---\n%s", want, out)
		}
	}
	for line := range strings.SplitSeq(out, "\n") {
		if !strings.Contains(line, "d2") {
			continue
		}
		dashes := 0
		for cell := range strings.FieldsSeq(line) {
			if cell == "-" {
				dashes++
			}
		}
		if dashes != 2 {
			t.Errorf("expected exactly two \"-\" cells (rows_examined, error_rate) on the d2 row, got %d in line: %q", dashes, line)
		}
	}
}

func TestInstanceDetailCodec_Encode_OmittedCPU(t *testing.T) {
	// A GCP instance: the API omits cpu_percent entirely (CPU is AWS-only, so
	// there's no field to key it on), which the codec must render as "-",
	// not a misleading "0.0%".
	detail := &InstanceDetail{
		Instance: Instance{
			ServerID:     "s1",
			ProviderName: "gcp",
			Metrics:      &Metrics{QPS: new(1.0)},
		},
		Window: "5m",
	}

	var buf bytes.Buffer
	if err := (&instanceDetailCodec{}).Encode(&buf, detail); err != nil {
		t.Fatalf("Encode() err = %v", err)
	}
	out := buf.String()
	var cpuLine string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "CPU:") {
			cpuLine = line
		}
	}
	if cpuLine == "" {
		t.Fatalf("no CPU line found in output:\n%s", out)
	}
	if !strings.HasSuffix(strings.TrimSpace(cpuLine), "-") {
		t.Errorf("expected the CPU line to render as \"-\" (omitted, not reported as zero), got %q", cpuLine)
	}
}

func TestFetchAndFilterTopQueries_OverfetchesForFilters(t *testing.T) {
	var gotLimit string
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotLimit = r.URL.Query().Get("limit")
		w.Write([]byte(`{"status":"success","data":{"queries":[` + //nolint:errcheck // test helper
			`{"query_id":"1","database":"payments"},` +
			`{"query_id":"2","database":"orders"},` +
			`{"query_id":"3","database":"payments"}]}}`))
	})

	filters, err := parseFilters([]string{"database=payments"})
	if err != nil {
		t.Fatalf("parseFilters() err = %v", err)
	}
	got, truncated, err := fetchAndFilterTopQueries(context.Background(), client, "prom-uid", "", "s1", "5m", 1, filters)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gotLimit != "100" {
		t.Errorf("expected the fetch to over-fetch to the API max when a filter is present, limit = %q", gotLimit)
	}
	if len(got) != 1 || got[0].QueryID != "1" {
		t.Errorf("expected exactly 1 filtered+truncated row, got %+v", got)
	}
	if !truncated {
		t.Error("expected truncated = true (2 payments rows matched, limit was 1)")
	}
}

func TestFetchAndFilterTopQueries_Truncated(t *testing.T) {
	// `truncated` must mean "rows were dropped", not "the page came back
	// full". When the fetch asked for the API maximum the set in hand is
	// complete, so only a strict overflow counts; when the server did the
	// clipping below its cap, a full page is genuinely indistinguishable from
	// a cut one and has to be reported as possibly truncated.
	rows := func(n int) string {
		parts := make([]string, 0, n)
		for i := range n {
			parts = append(parts, `{"query_id":"`+string(rune('a'+i))+`"}`)
		}
		return `{"status":"success","data":{"queries":[` + strings.Join(parts, ",") + `]}}`
	}
	cases := []struct {
		name          string
		returned      int
		limit         int
		wantTruncated bool
		wantRows      int
	}{
		{"server-limited full page is possibly truncated", 10, 10, true, 10},
		{"server-limited partial page is complete", 4, 10, false, 4},
		{"at the API maximum a full page is complete", maxTopQueries, maxTopQueries, false, maxTopQueries},
		{"at the API maximum a partial page is complete", 3, maxTopQueries, false, 3},
		{"--top 0 means the maximum, so a full page is complete", maxTopQueries, 0, false, maxTopQueries},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(rows(c.returned))) //nolint:errcheck // test helper
			})
			got, truncated, err := fetchAndFilterTopQueries(context.Background(), client, "prom-uid", "", "s1", "5m", c.limit, nil)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if truncated != c.wantTruncated {
				t.Errorf("truncated = %v, want %v (%d rows returned, limit %d)", truncated, c.wantTruncated, c.returned, c.limit)
			}
			if len(got) != c.wantRows {
				t.Errorf("len(rows) = %d, want %d", len(got), c.wantRows)
			}
		})
	}
}

func TestResolveInstance_ByServerID(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/instances/s1") {
			t.Errorf("unexpected path %q — a server_id should resolve in one request", r.URL.Path)
		}
		w.Write([]byte(`{"status":"success","data":{"server_id":"s1","instance":"db-1","engine":"postgres"}}`)) //nolint:errcheck // test helper
	})

	got, err := resolveInstance(context.Background(), client, "prom-uid", "s1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.ServerID != "s1" || got.Name != "db-1" {
		t.Errorf("got %+v", got)
	}
}

func TestResolveInstance_ByName(t *testing.T) {
	// A name resolves as well as a server_id: the server_id lookup 404s, and
	// the inventory is then searched by name before giving up.
	var detailCalls int
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/instances/db-1"):
			detailCalls++
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"status":"error","error":{"code":"instance_not_found","message":"instance not found"}}`)) //nolint:errcheck // test helper
		case strings.HasSuffix(r.URL.Path, "/instances/s1"):
			detailCalls++
			w.Write([]byte(`{"status":"success","data":{"server_id":"s1","instance":"db-1","engine":"postgres","metrics":{"qps":2}}}`)) //nolint:errcheck // test helper
		default:
			w.Write([]byte(`{"status":"success","data":[{"server_id":"s1","instance":"db-1","engine":"postgres"}]}`)) //nolint:errcheck // test helper
		}
	})

	got, err := resolveInstance(context.Background(), client, "prom-uid", "db-1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.ServerID != "s1" {
		t.Errorf("server_id = %q, want %q", got.ServerID, "s1")
	}
	// Re-fetched by server_id: the inventory route never returns metrics.
	if got.Metrics == nil || derefFloat(got.Metrics.QPS) != 2 {
		t.Errorf("expected the detail route's metrics after resolving by name, got %+v", got.Metrics)
	}
	if detailCalls != 2 {
		t.Errorf("expected the detail route to be called twice (miss, then hit), got %d", detailCalls)
	}
}

func TestResolveInstance_NotFound(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/instances/") {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"status":"error","error":{"code":"instance_not_found","message":"instance not found"}}`)) //nolint:errcheck // test helper
			return
		}
		w.Write([]byte(`{"status":"success","data":[{"server_id":"s1","instance":"other"}]}`)) //nolint:errcheck // test helper
	})

	_, err := resolveInstance(context.Background(), client, "prom-uid", "nope")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "instance not found") {
		t.Errorf("expected the sentinel not-found error, got %v", err)
	}
}

func TestResolveInstance_AmbiguousName(t *testing.T) {
	// Two instances sharing a name cannot be resolved to one server_id, and
	// the error has to hand over the identifiers that would work.
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/instances/") {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"status":"error","error":{"code":"instance_not_found","message":"instance not found"}}`)) //nolint:errcheck // test helper
			return
		}
		w.Write([]byte(`{"status":"success","data":[` + //nolint:errcheck // test helper
			`{"server_id":"s1","instance":"db-1"},` +
			`{"server_id":"s2","instance":"db-1"}]}`))
	})

	_, err := resolveInstance(context.Background(), client, "prom-uid", "db-1")
	if err == nil {
		t.Fatal("expected an ambiguity error")
	}
	for _, want := range []string{"matches 2 instances", "s1", "s2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should contain %q, got %v", want, err)
		}
	}
}

func TestResolveInstance_ByNameOutsideTheServerIDCharset(t *testing.T) {
	// The detail route answers 400 invalid_server_id — not 404 — for an
	// argument outside the charset it accepts for an id. An instance name
	// legitimately falls outside it, so the name lookup has to run anyway.
	const name = "my (db)/one"
	var sawDetail bool
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/instances/s1"):
			w.Write([]byte(`{"status":"success","data":{"server_id":"s1","instance":"my (db)/one","engine":"postgres"}}`)) //nolint:errcheck // test helper
		case strings.Contains(r.URL.Path, "/instances/"):
			sawDetail = true
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status":"error","error":{"code":"invalid_server_id","message":"invalid server_id format"}}`)) //nolint:errcheck // test helper
		default:
			w.Write([]byte(`{"status":"success","data":[{"server_id":"s1","instance":"my (db)/one"}]}`)) //nolint:errcheck // test helper
		}
	})

	got, err := resolveInstance(context.Background(), client, "prom-uid", name)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !sawDetail {
		t.Error("expected the server_id lookup to be attempted first")
	}
	if got.ServerID != "s1" {
		t.Errorf("server_id = %q, want %q", got.ServerID, "s1")
	}
}

func TestResolveInstance_OtherBadRequestsAreNotRetried(t *testing.T) {
	// Only invalid_server_id means "that was not an id". Any other 400 is a
	// real error and must not be retried as a name lookup.
	var listCalls int
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/instances/") {
			listCalls++
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"error","error":{"code":"invalid_datasource","message":"invalid datasource uid"}}`)) //nolint:errcheck // test helper
	})

	_, err := resolveInstance(context.Background(), client, "prom-uid", "whatever")
	if err == nil {
		t.Fatal("expected the error to be reported")
	}
	if !strings.Contains(err.Error(), "invalid datasource uid") {
		t.Errorf("expected the API's own error, got %v", err)
	}
	if listCalls != 0 {
		t.Errorf("expected no name lookup for an unrelated 400, got %d", listCalls)
	}
}
