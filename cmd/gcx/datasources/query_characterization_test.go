package datasources_test

// Characterization tests for the generic `gcx datasources query` command.
//
// These pin the observable behaviour of every datasource kind the generic
// command handles today: which request each kind builds, which flags reach it,
// the order validation and I/O happen in, and how failures surface. They were
// written against the hand-maintained type switch and must keep passing
// unchanged across the routing-table refactor (#1137) — that is the whole point
// of them, so prefer fixing the production code over editing this file.

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/cmd/gcx/datasources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGrafana serves the two endpoints the generic query path touches: the
// datasource record lookup and the unified query POST. It records what it saw
// so a test can assert the request the command actually built.
type fakeGrafana struct {
	t *testing.T

	dsType   string
	jsonData map[string]any

	// failDatasourceGetAfter makes the Nth and later datasource GETs fail, so a
	// test can break InfluxDB's second lookup without breaking the first.
	failDatasourceGetAfter int
	queryStatus            int

	mu           sync.Mutex
	datasourceGe int
	postPath     string
	postBody     map[string]any
}

func (f *fakeGrafana) start() *httptest.Server {
	f.t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	f.t.Cleanup(srv.Close)

	return srv
}

func (f *fakeGrafana) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/bootdata":
		http.NotFound(w, r)

	case r.Method == http.MethodGet && r.URL.Path == "/api/datasources/uid/uid":
		f.mu.Lock()
		f.datasourceGe++
		n := f.datasourceGe
		f.mu.Unlock()

		if f.failDatasourceGetAfter > 0 && n >= f.failDatasourceGetAfter {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"datasource lookup exploded"}`))
			return
		}

		payload := map[string]any{"id": 1, "uid": "uid", "name": "test", "type": f.dsType}
		if f.jsonData != nil {
			payload["jsonData"] = f.jsonData
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			f.t.Errorf("encode datasource response: %v", err)
		}

	case r.Method == http.MethodPost:
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("decode query request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		f.mu.Lock()
		f.postPath = r.URL.Path
		f.postBody = body
		f.mu.Unlock()

		if f.queryStatus != 0 && f.queryStatus != http.StatusOK {
			w.WriteHeader(f.queryStatus)
			_, _ = w.Write([]byte(`{"message":"query exploded"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(f.successBody(r.URL.Path)))

	default:
		f.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (f *fakeGrafana) successBody(path string) string {
	switch {
	case strings.Contains(path, "/api/datasources/proxy/uid/"):
		return `{"flamebearer":{"names":[],"levels":[],"numTicks":0,"maxSelf":0}}`
	case f.dsType == "prometheus":
		return `{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"Time","type":"time"},` +
			`{"name":"Value","type":"number","labels":{"job":"grafana"}}]},` +
			`"data":{"values":[[1711893600000],[1]]}}]}}}`
	default:
		return `{"results":{"A":{"frames":[]}}}`
	}
}

// seenDatasourceGets reports how many times the datasource record was fetched.
func (f *fakeGrafana) seenDatasourceGets() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.datasourceGe
}

func (f *fakeGrafana) seenPost() (string, map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.postPath, f.postBody
}

// firstQuery returns queries[0] from the recorded unified-query POST body.
func firstQuery(t *testing.T, body map[string]any) map[string]any {
	t.Helper()

	require.NotNil(t, body, "no query POST was recorded")
	queries, ok := body["queries"].([]any)
	require.Truef(t, ok, "expected queries array, got %T", body["queries"])
	require.Len(t, queries, 1)

	q, ok := queries[0].(map[string]any)
	require.Truef(t, ok, "expected query object, got %T", queries[0])

	return q
}

// runGeneric executes the generic query command against the fake and returns
// the error plus whatever landed on stdout.
func runGeneric(t *testing.T, f *fakeGrafana, args ...string) (string, error) {
	t.Helper()

	srv := f.start()
	configFile := newConfigFileForServer(t, srv.URL)

	root := helperRoot(datasources.QueryCmd())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append(args, "--config", configFile))

	err := root.Execute()

	return stdout.String(), err
}

func TestGenericQueryCharacterization_PrometheusTimeAndStep(t *testing.T) {
	f := &fakeGrafana{t: t, dsType: "prometheus"}

	_, err := runGeneric(t, f,
		"query", "uid", `up{job="grafana"}`,
		"--from", "now-1h", "--to", "now", "--step", "30s", "-o", "json")
	require.NoError(t, err)

	path, body := f.seenPost()
	assert.Contains(t, path, "/query.grafana.app/")

	q := firstQuery(t, body)
	assert.Equal(t, `up{job="grafana"}`, q["expr"])
	assert.InDelta(t, float64((30 * time.Second).Milliseconds()), q["intervalMs"], 0,
		"--step must reach intervalMs")

	start := parseUnixMillisField(t, body, "from")
	end := parseUnixMillisField(t, body, "to")
	assert.WithinDuration(t, end.Add(-time.Hour), start, time.Second)
}

func TestGenericQueryCharacterization_LokiLimit(t *testing.T) {
	f := &fakeGrafana{t: t, dsType: "loki"}

	_, err := runGeneric(t, f,
		"query", "uid", `{job="varlogs"}`,
		"--from", "now-1h", "--to", "now", "--limit", "7", "-o", "json")
	require.NoError(t, err)

	_, body := f.seenPost()
	q := firstQuery(t, body)
	assert.Equal(t, `{job="varlogs"}`, q["expr"])
	assert.InDelta(t, float64(7), q["maxLines"], 0, "--limit must reach maxLines")
}

func TestGenericQueryCharacterization_PyroscopeRequiresProfileType(t *testing.T) {
	f := &fakeGrafana{t: t, dsType: "grafana-pyroscope-datasource"}

	_, err := runGeneric(t, f, "query", "uid", `{service_name="frontend"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--profile-type is required for pyroscope queries")
	assert.Equal(t, 1, f.seenDatasourceGets(),
		"the guard must fire after exactly one datasource lookup and before any query")
}

func TestGenericQueryCharacterization_PyroscopeMaxNodes(t *testing.T) {
	f := &fakeGrafana{t: t, dsType: "grafana-pyroscope-datasource"}

	_, err := runGeneric(t, f,
		"query", "uid", `{service_name="frontend"}`,
		"--profile-type", "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
		"--max-nodes", "33",
		"--from", "now-1h", "--to", "now", "-o", "json")
	require.NoError(t, err)

	_, body := f.seenPost()
	require.NotNil(t, body)
	assert.Equal(t, `{service_name="frontend"}`, body["labelSelector"])
	assert.Equal(t, "process_cpu:cpu:nanoseconds:cpu:nanoseconds", body["profileTypeID"])
	assert.Equal(t, strconv.Itoa(33), body["maxNodes"], "--max-nodes must reach the request")
}

// InfluxDB is the only kind that fetches the datasource record twice: once to
// detect the type, once more to read jsonData.version for the query language.
func TestGenericQueryCharacterization_InfluxDBModeDetection(t *testing.T) {
	tests := []struct {
		name        string
		jsonData    map[string]any
		wantRawQuer bool
	}{
		{
			name:        "no jsonData defaults to InfluxQL",
			jsonData:    nil,
			wantRawQuer: true,
		},
		{
			name:        "Flux mode omits rawQuery",
			jsonData:    map[string]any{"version": "Flux"},
			wantRawQuer: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeGrafana{t: t, dsType: "influxdb", jsonData: tt.jsonData}

			_, err := runGeneric(t, f,
				"query", "uid", "SELECT 1",
				"--from", "now-1h", "--to", "now", "-o", "json")
			require.NoError(t, err)

			assert.Equal(t, 2, f.seenDatasourceGets(),
				"influxdb resolves the type and then the query-language mode")

			q := firstQuery(t, f.mustBody(t))
			assert.Equal(t, "SELECT 1", q["query"])
			if tt.wantRawQuer {
				assert.Equal(t, true, q["rawQuery"])
				assert.Equal(t, "table", q["resultFormat"])
			} else {
				assert.NotContains(t, q, "rawQuery")
			}
		})
	}
}

func TestGenericQueryCharacterization_InfluxDBModeLookupFailureIsWrapped(t *testing.T) {
	f := &fakeGrafana{t: t, dsType: "influxdb", failDatasourceGetAfter: 2}

	_, err := runGeneric(t, f, "query", "uid", "SELECT 1", "--from", "now-1h", "--to", "now")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to detect influxdb mode")
}

func TestGenericQueryCharacterization_ClickHouseLimitAndInterval(t *testing.T) {
	f := &fakeGrafana{t: t, dsType: "grafana-clickhouse-datasource"}

	_, err := runGeneric(t, f,
		"query", "uid", "SELECT 1",
		"--from", "now-1h", "--to", "now", "--step", "45s", "-o", "json")
	require.NoError(t, err)

	q := firstQuery(t, f.mustBody(t))
	assert.Equal(t, "SELECT 1 LIMIT 100", q["rawSql"],
		"the generic path enforces the default clickhouse limit")
	assert.InDelta(t, float64((45 * time.Second).Milliseconds()), q["intervalMs"], 0)
}

// A reachable query failure: the datasource lookup succeeds, so the client is
// built, and the unified query POST is what fails.
func TestGenericQueryCharacterization_QueryFailureIsWrapped(t *testing.T) {
	f := &fakeGrafana{t: t, dsType: "prometheus", queryStatus: http.StatusInternalServerError}

	_, err := runGeneric(t, f, "query", "uid", "up", "--from", "now-1h", "--to", "now")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query failed")
}

// The command owns encoding: handlers return a value and the command encodes it
// exactly once, so an encode failure propagates instead of being swallowed.
func TestGenericQueryCharacterization_EncodeErrorPropagates(t *testing.T) {
	f := &fakeGrafana{t: t, dsType: "prometheus"}
	srv := f.start()
	configFile := newConfigFileForServer(t, srv.URL)

	root := helperRoot(datasources.QueryCmd())
	root.SetOut(failingWriter{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{
		"query", "uid", "up", "--from", "now-1h", "--to", "now", "-o", "json",
		"--config", configFile,
	})

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdout is closed")
}

func TestGenericQueryCharacterization_EmitsOneJSONValueOnStdout(t *testing.T) {
	f := &fakeGrafana{t: t, dsType: "prometheus"}

	stdout, err := runGeneric(t, f,
		"query", "uid", "up", "--from", "now-1h", "--to", "now", "-o", "json")
	require.NoError(t, err)

	dec := json.NewDecoder(strings.NewReader(stdout))
	var first any
	require.NoError(t, dec.Decode(&first), "stdout must carry one JSON value")
	assert.IsType(t, map[string]any{}, first)

	var second any
	assert.ErrorContains(t, decodeNext(dec, &second), "EOF",
		"stdout must carry exactly one JSON value")
}

// Unknown kinds keep today's ordering: the expression is resolved and the times
// parsed before the unsupported-type verdict is reached.
func TestGenericQueryCharacterization_UnknownKindOrdering(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "no expression fails on the expression before the type verdict",
			args:    []string{"query", "uid"},
			wantErr: "expression is required",
		},
		{
			name:    "invalid step fails on parsing before the type verdict",
			args:    []string{"query", "uid", "whatever", "--step", "nonsense"},
			wantErr: "invalid --step duration",
		},
		{
			name:    "a fully valid request reaches the unsupported-type verdict",
			args:    []string{"query", "uid", "whatever"},
			wantErr: `datasource type "tempo" is not supported`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeGrafana{t: t, dsType: "tempo"}

			_, err := runGeneric(t, f, tt.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func (f *fakeGrafana) mustBody(t *testing.T) map[string]any {
	t.Helper()

	_, body := f.seenPost()
	require.NotNil(t, body, "no query POST was recorded")

	return body
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stdout is closed") }

func decodeNext(dec *json.Decoder, v any) error {
	if err := dec.Decode(v); err != nil {
		return err
	}

	return errors.New("a second JSON value was present")
}
