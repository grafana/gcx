package logs_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeListAPI serves three items from each Adaptive Logs list endpoint.
func fakeListAPI() http.HandlerFunc {
	items := func(extra string) string {
		parts := make([]string, 3)
		for i := range parts {
			parts[i] = fmt.Sprintf(`{"id":"id-%d",%s}`, i, extra)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/adaptive-logs/exemptions":
			fmt.Fprintf(w, `{"result":%s}`, items(`"stream_selector":"{app=\"x\"}"`))
		case "/adaptive-logs/segments":
			fmt.Fprint(w, items(`"name":"seg"`))
		case "/adaptive-logs/drop-rules":
			fmt.Fprint(w, items(`"name":"rule","segment_id":"__global__","version":1,"body":{"drop_rate":0.5}`))
		default:
			http.NotFound(w, r)
		}
	}
}

// TestLogsAdaptiveListEnvelope checks the list envelope of the adaptive logs
// list commands: the envelope is always present, list_meta is present only
// for a truncated page, and --json selects fields inside the items.
func TestLogsAdaptiveListEnvelope(t *testing.T) {
	commands := []string{"exemptions", "segments", "drop-rules"}
	tests := []struct {
		name      string
		args      []string
		wantLen   int
		wantMeta  bool
		wantField string
	}{
		{name: "json truncated", args: []string{"-o", "json", "--limit", "2"}, wantLen: 2, wantMeta: true},
		{name: "json complete", args: []string{"-o", "json", "--limit", "0"}, wantLen: 3},
		{name: "json field selection", args: []string{"--json", "id", "--limit", "1"}, wantLen: 1, wantMeta: true, wantField: "id"},
	}
	for _, command := range commands {
		for _, tc := range tests {
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				args := append([]string{command, "list"}, tc.args...)
				testutils.PinArgv(t, append([]string{"gcx", "logs", "adaptive"}, args...)...)
				loader := newContractLoader(t, fakeListAPI())

				res := runAdaptiveCmd(t, loader, false, "", args...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)

				page := testutils.DecodeListPage(t, res.stdout)
				require.Len(t, page.Items, tc.wantLen)
				assert.Equal(t, "id-0", page.Items[0]["id"])
				if tc.wantField != "" {
					assert.Len(t, page.Items[0], 1, "selection must apply to each item")
				}
				if tc.wantMeta {
					require.NotNil(t, page.ListMeta)
					assert.Equal(t, tc.wantLen, page.ListMeta.Returned)
					require.NotNil(t, page.ListMeta.Total)
					assert.Equal(t, 3, *page.ListMeta.Total)
					assert.Contains(t, res.stderr, "showing first")
				} else {
					assert.Nil(t, page.ListMeta)
				}
			})
		}
	}
}

// TestLogsAdaptiveDropRulesListDiscovery checks that --json list on drop-rules
// list lists the item fields and not the envelope keys.
func TestLogsAdaptiveDropRulesListDiscovery(t *testing.T) {
	loader := newContractLoader(t, fakeListAPI())
	res := runAdaptiveCmd(t, loader, false, "", "drop-rules", "list", "--json", "list")
	require.NoError(t, res.err, "stderr: %s", res.stderr)

	fields := strings.Fields(res.stdout)
	assert.Contains(t, fields, "id")
	assert.Contains(t, fields, "segment_id")
	assert.NotContains(t, fields, "items")
	assert.NotContains(t, fields, "list_meta")
}
