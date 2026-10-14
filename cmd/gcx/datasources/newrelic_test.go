package datasources_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/datasources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenericNewRelicQuery(t *testing.T) {
	var payload map[string]any
	srv := newQueryCaptureServer(t, "nrgrafanaplugin-newrelic-datasource", func(_ string, body map[string]any) {
		payload = body
	})
	t.Cleanup(srv.Close)
	root := helperRoot(datasources.QueryCmd())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"query", "uid", "SELECT count(*) FROM Transaction", "-o", "json", "--config", newConfigFileForServer(t, srv.URL)})
	require.NoError(t, root.Execute())
	assert.Contains(t, out.String(), `"frames"`)
	queries, ok := payload["queries"].([]any)
	require.True(t, ok)
	require.Len(t, queries, 1)
	query, ok := queries[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "SELECT count(*) FROM Transaction", query["queryText"])
	assert.NotContains(t, query, "accountID")
	assert.NotContains(t, query, "timeoutSeconds")
}

func TestGenericNewRelicGraphError(t *testing.T) {
	srv := newQueryCaptureServer(t, "nrgrafanaplugin-newrelic-datasource", func(_ string, _ map[string]any) {})
	t.Cleanup(srv.Close)
	root := helperRoot(datasources.QueryCmd())
	root.SetArgs([]string{"query", "uid", "SELECT 1", "-o", "graph", "--config", newConfigFileForServer(t, srv.URL)})
	err := root.Execute()
	require.ErrorContains(t, err, "graph output is not supported for New Relic")
}
