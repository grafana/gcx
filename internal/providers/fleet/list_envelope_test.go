package fleet //nolint:testpackage // Tests unexported Fleet command constructors.

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCollectorsServer serves n collectors from the list endpoint.
func newCollectorsServer(t *testing.T, n int) *httptest.Server {
	t.Helper()
	collectors := make([]map[string]any, n)
	for i := range collectors {
		collectors[i] = map[string]any{
			"id": fmt.Sprintf("collector-%d", i), "name": fmt.Sprintf("c%d", i),
			"collectorType": "COLLECTOR_TYPE_ALLOY", "enabled": true,
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, pathListCollectors) {
			writeContractJSON(t, w, map[string]any{"collectors": collectors})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestCollectorListEnvelope checks the list envelope of fleet collectors list
// in each output format.
func TestCollectorListEnvelope(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantLen   int
		wantMeta  bool
		wantField string // the only key of each item after --json selection
	}{
		{name: "json truncated", args: []string{"-o", "json", "--limit", "2"}, wantLen: 2, wantMeta: true},
		{name: "json complete", args: []string{"-o", "json", "--limit", "0"}, wantLen: 3},
		{name: "json default limit is complete", args: []string{"-o", "json"}, wantLen: 3},
		{
			name: "json field selection truncated", args: []string{"--json", "spec.id", "--limit", "1"},
			wantLen: 1, wantMeta: true, wantField: "spec.id",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutils.PinArgv(t, append([]string{"gcx", "fleet", "collectors", "list"}, tc.args...)...)
			srv := newCollectorsServer(t, 3)
			cmd := (&fleetHelper{loader: &fakeRESTLoader{url: srv.URL}}).newCollectorListCommand()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(tc.args)
			require.NoError(t, cmd.Execute())

			page := testutils.DecodeListPage(t, stdout.String())
			require.Len(t, page.Items, tc.wantLen)
			if tc.wantField != "" {
				for _, item := range page.Items {
					assert.Len(t, item, 1)
					assert.Contains(t, item, tc.wantField)
				}
			} else {
				spec, ok := page.Items[0]["spec"].(map[string]any)
				require.True(t, ok, "items keep their resource shape")
				assert.Equal(t, "collector-0", spec["id"])
			}
			if tc.wantMeta {
				require.NotNil(t, page.ListMeta)
				assert.Equal(t, tc.wantLen, page.ListMeta.Returned)
				require.NotNil(t, page.ListMeta.Total)
				assert.Equal(t, 3, *page.ListMeta.Total)
				assert.Contains(t, page.ListMeta.Continue, "gcx fleet collectors list")
				assert.Contains(t, stderr.String(), "showing first", "the stderr hint stays")
			} else {
				assert.Nil(t, page.ListMeta)
				assert.NotContains(t, stderr.String(), "showing first")
			}
		})
	}
}

// TestCollectorListTableUnchanged checks that the table does not show the
// envelope.
func TestCollectorListTableUnchanged(t *testing.T) {
	testutils.PinArgv(t, "gcx", "fleet", "collectors", "list", "-o", "table", "--limit", "2")
	srv := newCollectorsServer(t, 3)
	cmd := (&fleetHelper{loader: &fakeRESTLoader{url: srv.URL}}).newCollectorListCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"-o", "table", "--limit", "2"})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, stdout.String(), "collector-1")
	assert.NotContains(t, stdout.String(), "collector-2")
	assert.NotContains(t, stdout.String(), "items")
	assert.NotContains(t, stdout.String(), "list_meta")
	assert.Contains(t, stderr.String(), "showing first 2 of 3")
}
