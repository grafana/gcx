package fail_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/mcpserver"
	assistantmcp "github.com/grafana/gcx/internal/assistant/mcpservers"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// The unavailable marker must not change how MCP list failures render:
// commands such as `gcx assistant mcp-servers list` surface this error directly.
func TestMCPServerListFailureRenderingIgnoresUnavailableMarker(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		unavailable bool
	}{
		{name: "not found", status: http.StatusNotFound, unavailable: true},
		{name: "server error", status: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("collection unavailable"))
			}))
			t.Cleanup(server.Close)
			base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"})
			require.NoError(t, err)
			crud := mcpserver.NewTypedCRUDForClient(assistantmcp.NewClient(base), "default")

			_, err = crud.ListFn(t.Context(), 0)
			require.Error(t, err)
			assert.Equal(t, tc.unavailable, errors.Is(err, adapter.ErrUnavailable))

			converted := fail.ErrorToDetailedError(err)
			require.NotNil(t, converted)
			assert.Equal(t, "Failed to list MCP servers", converted.Summary)
			require.Error(t, converted.Parent)
			assert.Equal(t, fmt.Sprintf("request failed with status %d: collection unavailable", tc.status), converted.Parent.Error())
		})
	}
}
