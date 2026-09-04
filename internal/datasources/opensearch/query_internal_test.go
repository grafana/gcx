package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/opensearch"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// firstSettingsSize digs metrics[0].settings.size out of a decoded query object,
// the shape both the wire request and the Explore pane's query carry it in.
func firstSettingsSize(t *testing.T, q map[string]any) string {
	t.Helper()
	metrics, ok := q["metrics"].([]any)
	require.True(t, ok)
	require.Len(t, metrics, 1)
	m, ok := metrics[0].(map[string]any)
	require.True(t, ok)
	settings, ok := m["settings"].(map[string]any)
	require.True(t, ok)
	size, _ := settings["size"].(string)
	return size
}

// TestExecuteQuery_SentinelWiring pins the one thing TruncateRows alone can't:
// which request gets the +1. A reviewer found that swapping sentinelReq for
// req at the client call (or the reverse at the Explore URL) left the full
// suite green, since nothing asserted which value either destination received.
// This builds a resolvedQuery directly against a fake HTTP server — bypassing
// config loading and datasource resolution, which aren't what's under test —
// and checks both destinations in one test: the wire request must carry
// --limit+1, while the Explore link must carry the user-facing --limit so a
// shared link never leaks the sentinel.
func TestExecuteQuery_SentinelWiring(t *testing.T) {
	var (
		capturedSize string
		decodeErr    error
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if decodeErr = json.NewDecoder(r.Body).Decode(&body); decodeErr != nil {
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
		metrics, ok := q["metrics"].([]any)
		if !ok || len(metrics) != 1 {
			return
		}
		m, ok := metrics[0].(map[string]any)
		if !ok {
			return
		}
		settings, ok := m["settings"].(map[string]any)
		if !ok {
			return
		}
		capturedSize, _ = settings["size"].(string)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
	}))
	t.Cleanup(srv.Close)

	client, err := opensearch.NewClient(config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	})
	require.NoError(t, err)

	resolved := &resolvedQuery{
		Expr:          "level:error",
		Cfg:           config.NamespacedRESTConfig{GrafanaURL: "https://example.grafana.net"},
		DatasourceUID: "test-uid",
		Start:         time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
		End:           time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC),
		Client:        client,
	}

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	opts := &queryOpts{}
	opts.setup(cmd.Flags()) // registers IO/codecs at their flag defaults
	opts.Limit = 5
	opts.Mode = modeDocuments

	err = executeQuery(cmd, opts, resolved, dsquery.ExploreLinkOpts{ShareLink: true})
	require.NoError(t, err)
	require.NoError(t, decodeErr)

	assert.Equal(t, "6", capturedSize, "the wire request must ask for limit+1, the sentinel row")

	stderrOut := stderr.String()
	i := strings.Index(stderrOut, "https://")
	require.GreaterOrEqual(t, i, 0, "expected an Explore link in stderr: %s", stderrOut)
	exploreURL := strings.TrimSpace(stderrOut[i:])

	parsed, err := url.Parse(exploreURL)
	require.NoError(t, err)
	var panes map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(parsed.Query().Get("panes")), &panes))
	pane, ok := panes[dsquery.DefaultExplorePaneID]
	require.True(t, ok)
	queries, ok := pane["queries"].([]any)
	require.True(t, ok)
	require.Len(t, queries, 1)
	query, ok := queries[0].(map[string]any)
	require.True(t, ok)

	assert.Equal(t, "5", firstSettingsSize(t, query), "the Explore link must carry the user-facing limit, not the sentinel")
}
