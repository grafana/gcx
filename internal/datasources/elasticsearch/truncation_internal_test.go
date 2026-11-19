package elasticsearch

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/elasticsearch"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTruncationResolved(t *testing.T, body string) *resolvedQuery {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	client, err := elasticsearch.NewClient(config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	})
	require.NoError(t, err)

	return &resolvedQuery{
		Expr:          "level:error",
		Cfg:           config.NamespacedRESTConfig{GrafanaURL: "https://example.grafana.net"},
		DatasourceUID: "test-uid",
		Start:         time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
		End:           time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC),
		Client:        client,
	}
}

func newTruncationCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	return cmd, &stdout, &stderr
}

// The server answers with limit+1 documents, as it does when more match than
// the limit allows; query must trim to the limit and say so on stderr.
func TestExecuteQuery_WarnsWhenMoreDocumentsMatch(t *testing.T) {
	resolved := newTruncationResolved(t, `{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"message","type":"string"}]},"data":{"values":[["a","b","c"]]}}]}}}`)

	cmd, stdout, stderr := newTruncationCmd()
	opts := &queryOpts{}
	opts.setup(cmd.Flags())
	opts.Limit = 2
	opts.Mode = modeDocuments

	require.NoError(t, executeQuery(cmd, opts, resolved, dsquery.ExploreLinkOpts{}))

	assert.Contains(t, stderr.String(), "showing the first 2 rows; more rows match")
	assert.NotContains(t, stdout.String(), `"c"`, "the sentinel row must not be printed")
}

func TestExecuteQuery_NoWarningWhenEverythingFits(t *testing.T) {
	resolved := newTruncationResolved(t, `{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"message","type":"string"}]},"data":{"values":[["a","b"]]}}]}}}`)

	cmd, _, stderr := newTruncationCmd()
	opts := &queryOpts{}
	opts.setup(cmd.Flags())
	opts.Limit = 2
	opts.Mode = modeDocuments

	require.NoError(t, executeQuery(cmd, opts, resolved, dsquery.ExploreLinkOpts{}))

	assert.NotContains(t, stderr.String(), "more rows match")
}
