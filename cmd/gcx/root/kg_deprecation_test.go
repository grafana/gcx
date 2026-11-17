package root_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKGLegacyCypherDeprecationVerbosity(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the gcx binary")
	}
	bin := buildGcx(t)
	const response = `{"entities":[],"edges":[],"pageNum":0,"lastPage":true}`
	for _, tt := range []struct {
		name    string
		agent   string
		verbose bool
	}{
		{name: "human default", agent: "false"},
		{name: "human verbose", agent: "false", verbose: true},
		{name: "agent default", agent: "true"},
		{name: "agent verbose", agent: "true", verbose: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/search/cypher", r.URL.Path)
				_, _ = fmt.Fprint(w, response)
			}))
			defer server.Close()
			args := []string{"kg", "entities", "query", "MATCH (s:Service) RETURN s", "-o", "json"}
			if tt.verbose {
				args = append(args, "-v")
			}
			cmd := exec.CommandContext(t.Context(), bin, args...)
			cmd.Env = append(conformanceEnv(t.TempDir()), "GCX_AGENT_MODE="+tt.agent, "GRAFANA_SERVER="+server.URL, "GRAFANA_STACK_ID=12345")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			require.NoError(t, cmd.Run(), stderr.String())
			assertOneJSONValue(t, stdout.String())
			assert.JSONEq(t, response, stdout.String())
			if tt.verbose {
				assert.Contains(t, stderr.String(), "gcx kg entities query is deprecated")
				assert.Contains(t, stderr.String(), "gcx kg graph query")
				assert.Contains(t, stderr.String(), "supported through v1.x")
			} else {
				assert.NotContains(t, stderr.String(), "deprecated")
			}
		})
	}
}
