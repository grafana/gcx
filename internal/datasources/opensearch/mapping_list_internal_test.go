package opensearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/opensearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// TestFetchMappingResult_IndexReachesClient pins the one thing
// TestListIndicesCmd_HasIndexFlag couldn't: that the --index value the flag
// captures is the same value that reaches Client.Mapping's request. Before
// this test, if opts.Index stopped being threaded into the client call —
// dropped, hardcoded, or read from the wrong field — the flag-existence test
// would still pass, since it only checks the flag is registered.
func TestFetchMappingResult_IndexReachesClient(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	client, err := opensearch.NewClient(config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	})
	require.NoError(t, err)

	spec := mappingListSpec{
		errNoun: "indices",
		result: func(indices []opensearch.IndexInfo, _ []opensearch.FieldInfo) any {
			return indices
		},
	}

	_, err = fetchMappingResult(context.Background(), client, "test-uid", &mappingListOpts{Index: "grafana-*"}, spec)
	require.NoError(t, err)

	decoded, err := url.PathUnescape(capturedPath)
	require.NoError(t, err)
	assert.Contains(t, decoded, "grafana-*/_mapping", "the --index value must reach the _mapping request path")
}
