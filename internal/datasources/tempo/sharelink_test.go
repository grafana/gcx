package tempo

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCmd_ShareLinkRequiresExplicitTimeRange(t *testing.T) {
	var traceCalls int
	var metadataCalls int

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case "/api/datasources/proxy/uid/tempo-uid/api/v2/traces/trace-123":
			traceCalls++
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"trace":{"traceID":"trace-123"}}`))
			require.NoError(t, err)
		case "/api/datasources/uid/tempo-uid":
			metadataCalls++
			http.Error(w, `{"message":"unexpected datasource lookup"}`, http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeTempoTestConfig(t, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
    datasources:
      tempo: tempo-uid
current-context: default
`))

	stdout, stderr, err := execTempoCmd(GetCmd(loader), []string{"get", "trace-123", "--share-link"})
	require.NoError(t, err)
	assert.Equal(t, 1, traceCalls)
	assert.Zero(t, metadataCalls)
	assert.Contains(t, stdout, `"traceID": "trace-123"`)
	assert.Contains(t, stderr, "Grafana Explore links require --since or --from/--to")
	assert.NotContains(t, stderr, "Explore link:")
}

func TestSearchCmd_ShareLinkRequiresExplicitTimeRange(t *testing.T) {
	var searchCalls int
	var metadataCalls int

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case "/api/datasources/proxy/uid/tempo-uid/api/search":
			searchCalls++
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"traces":[{"traceID":"trace-123","rootServiceName":"svc","rootTraceName":"op","startTimeUnixNano":"1","durationMs":10}]}`))
			require.NoError(t, err)
		case "/api/datasources/uid/tempo-uid":
			metadataCalls++
			http.Error(w, `{"message":"unexpected datasource lookup"}`, http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeTempoTestConfig(t, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
    datasources:
      tempo: tempo-uid
current-context: default
`))

	stdout, stderr, err := execTempoCmd(QueryCmd(loader), []string{"query", "--share-link", "-o", "json", `{ span.http.status_code >= 500 }`})
	require.NoError(t, err)
	assert.Equal(t, 1, searchCalls)
	assert.Zero(t, metadataCalls)
	assert.Contains(t, stdout, `"traceID": "trace-123"`)
	assert.Contains(t, stderr, "Grafana Explore links require --since or --from/--to")
	assert.NotContains(t, stderr, "Explore link:")
}

func TestSearchCmd_ShareLinkRejectsUnlimitedResultLinks(t *testing.T) {
	var searchCalls int
	var metadataCalls int

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case "/api/datasources/proxy/uid/tempo-uid/api/search":
			searchCalls++
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"traces":[{"traceID":"trace-123","rootServiceName":"svc","rootTraceName":"op","startTimeUnixNano":"1","durationMs":10}]}`))
			require.NoError(t, err)
		case "/api/datasources/uid/tempo-uid":
			metadataCalls++
			http.Error(w, `{"message":"unexpected datasource lookup"}`, http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeTempoTestConfig(t, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
    datasources:
      tempo: tempo-uid
current-context: default
`))

	stdout, stderr, err := execTempoCmd(QueryCmd(loader), []string{"query", "--share-link", "--since", "1h", "--limit", "0", "-o", "json", `{ span.http.status_code >= 500 }`})
	require.NoError(t, err)
	assert.Equal(t, 1, searchCalls)
	assert.Zero(t, metadataCalls)
	assert.Contains(t, stdout, `"traceID": "trace-123"`)
	assert.Contains(t, stderr, "Grafana Explore links do not support --limit 0")
	assert.NotContains(t, stderr, "Explore link:")
}

func execTempoCmd(cmd *cobra.Command, args []string) (string, string, error) {
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)

	err := root.Execute()
	return stdout.String(), stderr.String(), err
}
