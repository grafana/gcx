package k6_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/k6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTestRunsAPI serves a load test's runs the way k6 Cloud does: oldest
// first unless $orderby=created desc is sent, in pages of $top (at most
// 1,000) starting at $skip. It also asserts the client asks for newest first.
func fakeTestRunsAPI(t *testing.T, loadTestID, total int) http.Handler {
	t.Helper()
	base := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	runs := make([]map[string]any, total)
	for i := range runs {
		runs[i] = map[string]any{
			"id": i + 1, "test_id": loadTestID, "status": "completed", "result": "passed",
			"created": base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
		}
	}
	path := fmt.Sprintf("/cloud/v6/load_tests/%d/test_runs", loadTestID)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != path {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		q := r.URL.Query()
		ordered := runs
		if q.Get("$orderby") == "created desc" {
			ordered = slices.Clone(runs)
			slices.Reverse(ordered)
		} else {
			t.Errorf("expected $orderby=created desc, got %q", q.Get("$orderby"))
		}
		top, err := strconv.Atoi(q.Get("$top"))
		if err != nil || top < 1 || top > 1000 {
			t.Errorf("expected $top between 1 and 1000, got %q", q.Get("$top"))
			top = 1000
		}
		skip, err := strconv.Atoi(q.Get("$skip"))
		if err != nil || skip < 0 {
			t.Errorf("expected $skip >= 0, got %q", q.Get("$skip"))
			skip = 0
		}
		page := ordered[min(skip, len(ordered)):min(skip+top, len(ordered))]
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{"value": page})
	})
}

func TestListTestRuns_NewestFirstAndPaged(t *testing.T) {
	clients := map[string]func(t *testing.T, h http.Handler) k6.API{
		"ProxyClient": func(t *testing.T, h http.Handler) k6.API {
			t.Helper()
			return newAuthenticatedProxyClient(t, h)
		},
		"DirectClient": func(t *testing.T, h http.Handler) k6.API {
			t.Helper()
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)
			client := k6.NewDirectClient(t.Context(), srv.URL, nil)
			client.SetCachedAuth("test-token", 1, 999)
			return client
		},
	}
	tests := []struct {
		name  string
		total int
		limit int
		check func(t *testing.T, runs []k6.TestRunStatus)
	}{
		{
			name: "limit returns the newest runs", total: 5, limit: 2,
			check: func(t *testing.T, runs []k6.TestRunStatus) {
				t.Helper()
				require.Len(t, runs, 2)
				assert.Equal(t, 5, runs[0].ID)
				assert.Equal(t, 4, runs[1].ID)
			},
		},
		{
			name: "zero limit returns every run across pages, newest first", total: 1336, limit: 0,
			check: func(t *testing.T, runs []k6.TestRunStatus) {
				t.Helper()
				require.Len(t, runs, 1336)
				for i, run := range runs {
					require.Equal(t, 1336-i, run.ID, "run %d out of order", i)
				}
			},
		},
		{
			name: "no runs returns an empty list", total: 0, limit: 0,
			check: func(t *testing.T, runs []k6.TestRunStatus) {
				t.Helper()
				assert.NotNil(t, runs)
				assert.Empty(t, runs)
			},
		},
	}
	for clientName, newClient := range clients {
		for _, tt := range tests {
			t.Run(clientName+"/"+tt.name, func(t *testing.T) {
				client := newClient(t, fakeTestRunsAPI(t, 6, tt.total))
				runs, err := client.ListTestRuns(t.Context(), 6, tt.limit)
				require.NoError(t, err)
				tt.check(t, runs)
			})
		}
	}
}
