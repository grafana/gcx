package synth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// newTestClient builds a synth.Client wired to a local httptest server.
// Shared by per-resource tests in probes_test.go and checks_test.go.
func newTestClient(t *testing.T, handler http.Handler) *synth.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}
	client, err := synth.NewClient(cfg)
	require.NoError(t, err)
	return client
}
