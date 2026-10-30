package opensearch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newSentinelCaptureServer starts a fake HTTP server that decodes the
// outgoing query, extracts a value from it via extract, stores it in
// *captured, and replies with an empty result — the request shape is what's
// under test, not the response. Shared by TestExecuteQuery_SentinelWiring and
// TestExecuteMetrics_SentinelWiring, which otherwise differ only in which
// field of the query they dig the sentinel value out of.
//
// extract must not call require/assert: it runs inside the handler's own
// goroutine, where FailNow (which require/assert call on failure) is not
// safe to invoke per the testing.T contract, so it degrades to an empty
// string on any unexpected shape instead of failing the test directly — the
// caller's own assertion on *captured after the request completes is what
// actually fails the test.
func newSentinelCaptureServer(t *testing.T, captured *string, decodeErr *error, extract func(q map[string]any) string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			*decodeErr = err
			return
		}
		queries, ok := body["queries"].([]any)
		if !ok || len(queries) != 1 {
			return
		}
		q, ok := queries[0].(map[string]any)
		if !ok {
			return
		}
		*captured = extract(q)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}
