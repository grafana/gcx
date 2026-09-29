package loki_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/datasources/loki"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryCmd_LimitValidation(t *testing.T) {
	t.Run("zero is rejected before config loading", func(t *testing.T) {
		cmd := loki.QueryCmd(&providers.ConfigLoader{})
		cmd.SetArgs([]string{"--limit", "0", `{job="varlogs"}`})

		err := cmd.Execute()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "--limit must be positive")
		assert.Contains(t, err.Error(), "gcx logs metrics")
	})

	t.Run("positive limit reaches maxLines", func(t *testing.T) {
		var gotMaxLines float64
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
				gotMaxLines, _ = body.Queries[0]["maxLines"].(float64)
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
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
		cmd := loki.QueryCmd(loader)
		cmd.SetArgs([]string{"-d", "loki-uid", "--limit", "7", "-o", "json", `{job="varlogs"}`})

		require.NoError(t, cmd.Execute())
		assert.InDelta(t, float64(7), gotMaxLines, 0)
	})
}
