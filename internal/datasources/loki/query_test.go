package loki_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/datasources/loki"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oneLogLine is a Grafana Loki frame with a single log line.
const oneLogLine = `{"results":{"A":{"frames":[{"schema":{"fields":[` +
	`{"name":"labels","type":"other"},{"name":"Time","type":"time"},{"name":"Line","type":"string"}]},` +
	`"data":{"values":[[{"job":"varlogs"}],[1711893600000],["hello"]]}}]}}}`

// executeQuery runs the query command under a root with buffered streams, so
// output stays out of the test log. It returns stderr for assertions.
func executeQuery(loader *providers.ConfigLoader, args ...string) (string, error) {
	root := &cobra.Command{Use: "test"}
	root.AddCommand(loki.QueryCmd(loader))

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"query"}, args...))

	err := root.Execute()
	return stderr.String(), err
}

func TestQueryCmd_LimitValidation(t *testing.T) {
	t.Run("negative is rejected before config loading", func(t *testing.T) {
		_, err := executeQuery(&providers.ConfigLoader{}, "--limit", "-1", `{job="varlogs"}`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "--limit must be >= 0, got -1")
	})

	for _, tt := range []struct {
		name         string
		limit        string
		output       string
		wantMaxLines any
		wantHint     string // substring of the one expected stderr line; empty means none
	}{
		{name: "positive limit reaches maxLines", limit: "7", output: "json", wantMaxLines: float64(7)},
		{name: "zero omits maxLines and hints", limit: "0", output: "json", wantHint: "backend default limit"},
		{name: "zero with graph hints once", limit: "0", output: "graph", wantHint: "chart may not cover the whole range"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var gotQuery map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/bootdata":
					http.NotFound(w, r)
				case r.Method == http.MethodGet && r.URL.Path == "/api/datasources/uid/loki-uid":
					_, _ = w.Write([]byte(`{"id":1,"uid":"loki-uid","name":"Loki","type":"loki"}`))
				case r.Method == http.MethodPost:
					var body struct {
						Queries []map[string]any `json:"queries"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode query request: %v", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if len(body.Queries) != 1 {
						t.Errorf("queries length = %d, want 1", len(body.Queries))
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					gotQuery = body.Queries[0]
					_, _ = w.Write([]byte(oneLogLine))
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(testutils.CreateTempFile(t, fmt.Sprintf(`current-context: test
contexts:
  test:
    grafana:
      server: %s
      token: test-token
      org-id: 1
`, server.URL)))
			stderr, err := executeQuery(loader, "-d", "loki-uid", "--limit", tt.limit, "-o", tt.output, `{job="varlogs"}`)

			require.NoError(t, err)
			require.NotNil(t, gotQuery)
			if tt.wantMaxLines == nil {
				assert.NotContains(t, gotQuery, "maxLines")
			} else {
				assert.Equal(t, tt.wantMaxLines, gotQuery["maxLines"])
			}
			if tt.wantHint == "" {
				assert.NotContains(t, stderr, "limit")
				return
			}
			lines := strings.Split(strings.TrimSpace(stderr), "\n")
			require.Len(t, lines, 1, "want exactly one hint, got %q", stderr)
			assert.Contains(t, lines[0], tt.wantHint)
		})
	}
}
