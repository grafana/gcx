package search_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/providers/dashboards/search"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pagedSearchHandler serves total dashboards. It applies the limit query
// parameter like Grafana: without it, the page size is 50. It records each
// limit value that it receives.
func pagedSearchHandler(total int, limits *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("limit")
		*limits = append(*limits, raw)
		n := 50
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			n = v
		}
		n = min(n, total)
		resp := testServerResponse{TotalHits: int64(total)}
		for i := range n {
			resp.Hits = append(resp.Hits, testServerHit{Resource: "dashboards", Name: fmt.Sprintf("uid-%d", i), Title: "t"})
		}
		writeJSONResponse(w, resp)
	}
}

func TestSearch_ListTruncation(t *testing.T) {
	tests := []struct {
		name       string
		limit      string
		wantItems  int
		wantMeta   map[string]any
		wantLimits []string
	}{
		{
			name:      "truncated page reports the server total",
			limit:     "10",
			wantItems: 10,
			wantMeta: map[string]any{
				"truncated": true, "returned": float64(10), "total": float64(120),
				"continue": "gcx dashboards search cpu -o json --limit 0",
			},
			wantLimits: []string{"10"},
		},
		{
			name:       "default limit is truncated",
			limit:      "",
			wantItems:  50,
			wantMeta:   map[string]any{"truncated": true, "returned": float64(50), "total": float64(120), "continue": "gcx dashboards search cpu -o json --limit 0"},
			wantLimits: []string{"50"},
		},
		{
			name:       "limit 0 returns all matches",
			limit:      "0",
			wantItems:  120,
			wantLimits: []string{"", "120"},
		},
		{
			name:       "limit above the total is complete",
			limit:      "200",
			wantItems:  120,
			wantLimits: []string{"200"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SetAgentMode(t, false)
			args := []string{"cpu", "-o", "json"}
			if tc.limit != "" {
				args = append(args, "--limit", tc.limit)
			}
			testutils.PinArgv(t, append([]string{"gcx", "dashboards", "search"}, args...)...)

			var limits []string
			srv, loader := newTestServer(t, pagedSearchHandler(120, &limits))
			defer srv.Close()

			cmd := search.Commands(loader)
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(args)
			require.NoError(t, cmd.Execute())

			var got map[string]any
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
			items, ok := got["items"].([]any)
			require.True(t, ok)
			assert.Len(t, items, tc.wantItems)
			assert.Equal(t, tc.wantLimits, limits)

			if tc.wantMeta == nil {
				assert.NotContains(t, got, "list_meta")
				assert.NotContains(t, stderr.String(), "showing first")
				return
			}
			assert.Equal(t, tc.wantMeta, got["list_meta"])
			assert.Contains(t, stderr.String(),
				fmt.Sprintf("hint: showing first %d of 120. See all results with: %s", tc.wantItems, tc.wantMeta["continue"]))
		})
	}
}
