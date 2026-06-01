package tempo_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	cmdtempo "github.com/grafana/gcx/internal/datasources/tempo"
	"github.com/grafana/gcx/internal/providers"
	querytempo "github.com/grafana/gcx/internal/query/tempo"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLabelsCmd_TagValuesLLM(t *testing.T) {
	var valuesCalls int

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case "/api/datasources/proxy/uid/tempo-uid/api/v2/search/tag/resource.service.name/values":
			valuesCalls++
			assert.Equal(t, querytempo.AcceptLLM, r.Header.Get("Accept"))
			w.Header().Set("Content-Type", "application/vnd.grafana.llm+json")
			_, err := w.Write([]byte(`{"tagValues":{"string":["frontend","backend"]},"metrics":{"inspectedBytes":123}}`))
			assert.NoError(t, err)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	cfgFile := writeTempoTestConfig(t, `
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
`)

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	cmd := cmdtempo.LabelsCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"tags", "-l", "service.name", "--scope", "resource", "--llm", "-o", "json"})

	err := root.Execute()
	require.NoError(t, err)
	assert.Equal(t, 1, valuesCalls)
	assert.Contains(t, stdout.String(), `"tagValues": {`)
	assert.Contains(t, stdout.String(), `"string": [`)
	assert.Contains(t, stdout.String(), `"frontend"`)
	assert.Empty(t, stderr.String())
}
