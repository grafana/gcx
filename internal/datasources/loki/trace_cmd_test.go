package loki_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	dsloki "github.com/grafana/gcx/internal/datasources/loki"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const traceTestID = "0000000000000000237857c3f3e35e37"

// fakeLoki answers datasource lookup, label discovery and log queries. A query
// returns one log line only when its expression contains matchSubstr.
type fakeLoki struct {
	mu          sync.Mutex
	exprs       []string
	bodies      []map[string]any
	labels      []string
	matchSubstr string
	matchExact  string
	failExpr    string // queries containing this substring return a 500
}

func (f *fakeLoki) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/resources/labels"):
			b, err := json.Marshal(map[string]any{"status": "success", "data": f.labels})
			assert.NoError(t, err)
			_, _ = w.Write(b)
		case r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/query") || r.URL.Path == "/api/ds/query"):
			raw, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			var body map[string]any
			assert.NoError(t, json.Unmarshal(raw, &body))
			queries, _ := body["queries"].([]any)
			if len(queries) == 0 {
				http.Error(w, `{"message":"no queries"}`, http.StatusBadRequest)
				return
			}
			q, _ := queries[0].(map[string]any)
			expr, _ := q["expr"].(string)

			f.mu.Lock()
			f.exprs = append(f.exprs, expr)
			f.bodies = append(f.bodies, body)
			f.mu.Unlock()

			if f.failExpr != "" && strings.Contains(expr, f.failExpr) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"results":{"A":{"error":"boom","status":500}}}`))
				return
			}

			if (f.matchSubstr != "" && strings.Contains(expr, f.matchSubstr)) || (f.matchExact != "" && expr == f.matchExact) {
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"labels","type":"other"},{"name":"Time","type":"time"},{"name":"Line","type":"string"}]},"data":{"values":[[{"service_name":"app"}],[1711893600000],["hello trace"]]}}]}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
		default:
			// Datasource metadata lookups used for -d validation.
			_, _ = w.Write([]byte(`{"uid":"loki-uid","name":"loki","type":"loki"}`))
		}
	}
}

func execQueryTrace(t *testing.T, f *fakeLoki, args ...string) (string, string, error) {
	t.Helper()
	testutils.SetAgentMode(t, false)

	srv := httptest.NewTLSServer(f.handler(t))
	defer srv.Close()

	cfgFile := writePatternsTestConfig(t, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
current-context: default
`)

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	root := &cobra.Command{Use: "test"}
	root.AddCommand(dsloki.QueryCmd(loader))

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"query", "-d", "loki-uid"}, args...))

	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestQueryCmd_TraceWithService_ProbesUntilMatch(t *testing.T) {
	f := &fakeLoki{matchSubstr: `{job=~"(.*/)?(app)"} | logfmt | json | drop __error__ | trace_id="` + traceTestID + `"`}

	stdout, stderr, err := execQueryTrace(t, f, "--trace-id", traceTestID, "--trace-service", "app", "-o", "raw")
	require.NoError(t, err)

	// traceID x2 selectors, then trace_id on service_name, then trace_id on job (match).
	require.Len(t, f.exprs, 4)
	assert.Equal(t, `{service_name="app"} | logfmt | json | drop __error__ | traceID="`+traceTestID+`"`, f.exprs[0])
	assert.Equal(t, f.matchSubstr, f.exprs[3])
	assert.Contains(t, stdout, "hello trace")
	assert.Contains(t, stderr, "matched logs for trace "+traceTestID)

	// Instant queries only look back a minute; trace mode must default to a 1h range.
	body := f.bodies[0]
	from, err := strconv.ParseInt(body["from"].(string), 10, 64) //nolint:forcetypeassert // test fixture
	require.NoError(t, err, "trace mode must send a range, not an instant query")
	to, err := strconv.ParseInt(body["to"].(string), 10, 64) //nolint:forcetypeassert // test fixture
	require.NoError(t, err)
	assert.InDelta(t, time.Hour.Milliseconds(), to-from, 1000)
}

func TestQueryCmd_TraceNoMatchReportsFailedCandidates(t *testing.T) {
	f := &fakeLoki{failExpr: "trace_id"}

	_, stderr, err := execQueryTrace(t, f, "--trace-id", traceTestID, "--trace-service", "app")
	require.NoError(t, err)
	assert.Contains(t, stderr, "candidate queries failed)")
	assert.Contains(t, stderr, "results may be incomplete")
}

func TestQueryCmd_TraceMatchReportsFailedCandidates(t *testing.T) {
	// The first candidate (traceID) errors; the second (trace_id) matches.
	f := &fakeLoki{failExpr: `traceID="`, matchSubstr: `trace_id="`}

	_, stderr, err := execQueryTrace(t, f, "--trace-id", traceTestID, `{cluster="prod"}`)
	require.NoError(t, err)
	assert.Contains(t, stderr, "matched logs for trace")
	assert.Contains(t, stderr, "(1 candidate query failed)")
}

func TestQueryCmd_EmptyTraceFlagsRejected(t *testing.T) {
	for _, flag := range []string{"--trace-id", "--span-id", "--trace-service"} {
		t.Run(flag, func(t *testing.T) {
			f := &fakeLoki{}
			_, _, err := execQueryTrace(t, f, `{a="b"}`, flag, "")
			require.ErrorContains(t, err, "value is empty")
			assert.Empty(t, f.exprs)
		})
	}
}

func TestQueryCmd_TraceWithSelectorAndSpan(t *testing.T) {
	f := &fakeLoki{matchExact: `{cluster="prod"} |= "` + traceTestID + `"`}

	_, _, err := execQueryTrace(t, f, `{cluster="prod"}`, "--trace-id", traceTestID, "--span-id", "46be9af5ba3e6a15")
	require.NoError(t, err)

	require.NotEmpty(t, f.exprs)
	assert.Equal(t,
		`{cluster="prod"} | logfmt | json | drop __error__ | traceID="`+traceTestID+`" | spanID="46be9af5ba3e6a15"`,
		f.exprs[0])
	// Span-scoped candidates come first, followed by the trace-only fallback.
	last := f.exprs[len(f.exprs)-1]
	assert.Equal(t, `{cluster="prod"} |= "`+traceTestID+`"`, last)
}

func TestQueryCmd_TraceDiscoversSelectorFromLabels(t *testing.T) {
	f := &fakeLoki{labels: []string{"pod", "job", "service_name"}, matchSubstr: `{job=~".+"} |= "` + traceTestID + `"`}

	_, _, err := execQueryTrace(t, f, "--trace-id", traceTestID)
	require.NoError(t, err)

	require.NotEmpty(t, f.exprs)
	assert.Equal(t, `{service_name=~".+"} | logfmt | json | drop __error__ | traceID="`+traceTestID+`"`, f.exprs[0])
	assert.Equal(t, f.matchSubstr, f.exprs[len(f.exprs)-1])
}

func TestQueryCmd_TraceNoMatchHintsAndSucceeds(t *testing.T) {
	f := &fakeLoki{}

	_, stderr, err := execQueryTrace(t, f, "--trace-id", traceTestID, "--trace-service", "app")
	require.NoError(t, err)
	assert.Contains(t, stderr, "no logs found for trace "+traceTestID)
	assert.Len(t, f.exprs, 14)
}

func TestQueryCmd_TraceNoMatchWithErrorOnEmptyFails(t *testing.T) {
	f := &fakeLoki{}

	_, _, err := execQueryTrace(t, f, "--trace-id", traceTestID, "--trace-service", "app", "--error-on-empty")
	require.Error(t, err)
}

func TestQueryCmd_TraceFlagValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"pipeline expr rejected", []string{`{a="b"} |= "x"`, "--trace-id", traceTestID}, "bare stream selector"},
		{"service with expr", []string{`{a="b"}`, "--trace-id", traceTestID, "--trace-service", "app"}, "--trace-service cannot be combined"},
		{"bad trace id", []string{"--trace-id", "not-hex!"}, "invalid --trace-id"},
		{"span without trace", []string{`{a="b"}`, "--span-id", "abc"}, "--span-id requires --trace-id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeLoki{}
			_, _, err := execQueryTrace(t, f, tc.args...)
			require.ErrorContains(t, err, tc.wantErr)
			assert.Empty(t, f.exprs, "no query must be sent on invalid flags")
		})
	}
}

func TestQueryCmd_ExprStillRequiredWithoutTrace(t *testing.T) {
	f := &fakeLoki{}
	_, _, err := execQueryTrace(t, f)
	require.ErrorContains(t, err, "expression is required")
}
