package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
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

// firstTermsSize digs bucketAggs[0].settings.size out of a decoded query
// object, the shape both the wire request and the Explore pane's query carry
// the terms-bucket group size in.
func firstTermsSize(t *testing.T, q map[string]any) string {
	t.Helper()
	bucketAggs, ok := q["bucketAggs"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, bucketAggs)
	b, ok := bucketAggs[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "terms", b["type"], "bucketAggs[0] must be the terms bucket when --group-by is set")
	settings, ok := b["settings"].(map[string]any)
	require.True(t, ok)
	size, _ := settings["size"].(string)
	return size
}

// TestExecuteMetrics_SentinelWiring is TestExecuteQuery_SentinelWiring's
// counterpart for the metrics path: a reviewer found that swapping
// sentinelReq for req at the client call (or the reverse at the Explore URL)
// in executeMetrics left the full suite green, since nothing asserted which
// request carried the +1. This builds a resolvedQuery directly against a fake
// HTTP server and checks both destinations in one test: the wire request must
// carry --group-size+1, while the Explore link must carry the user-facing
// --group-size so a shared link never leaks the sentinel.
func TestExecuteMetrics_SentinelWiring(t *testing.T) {
	var (
		capturedSize string
		decodeErr    error
	)
	srv := newSentinelCaptureServer(t, &capturedSize, &decodeErr, func(q map[string]any) string {
		bucketAggs, ok := q["bucketAggs"].([]any)
		if !ok || len(bucketAggs) == 0 {
			return ""
		}
		b, ok := bucketAggs[0].(map[string]any)
		if !ok {
			return ""
		}
		settings, ok := b["settings"].(map[string]any)
		if !ok {
			return ""
		}
		size, _ := settings["size"].(string)
		return size
	})

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

	opts := &metricsOpts{}
	opts.setup(cmd.Flags()) // registers IO/codecs at their flag defaults
	opts.GroupBy = "app.keyword"
	opts.GroupSize = 5
	opts.Agg = "count"

	err = executeMetrics(cmd, opts, resolved, dsquery.ExploreLinkOpts{ShareLink: true})
	require.NoError(t, err)
	require.NoError(t, decodeErr)

	assert.Equal(t, "6", capturedSize, "the wire request must ask for group-size+1, the sentinel group")

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

	assert.Equal(t, "5", firstTermsSize(t, query), "the Explore link must carry the user-facing group size, not the sentinel")
}
