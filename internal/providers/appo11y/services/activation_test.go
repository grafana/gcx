package services

// Guards the App Observability activation pre-flight (#1309 PR1): every
// `services` subcommand must check plugin activation immediately after
// loading config and fail fast — via activation.NotActivatedError() — before
// issuing any PromQL/Tempo query, instead of silently querying a stack where
// the plugin isn't even enabled.

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/appo11y/activation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const activationEndpoint = "/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings"

// activationGatedServer starts a mock Grafana server that returns activationStatus
// for the plugin-settings activation check and records every subsequent
// query-ish request path so tests can assert no query was issued once
// activation fails. /bootdata is answered (like list_test.go's runListCmd)
// but excluded from the recorded paths: it's namespace/cloud-stack discovery
// that LoadContextAndConfig performs before the activation check ever runs,
// not a PromQL/Tempo query the gated commands issue.
func activationGatedServer(t *testing.T, activationStatus int) (*httptest.Server, *[]string) {
	t.Helper()
	var otherPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case activationEndpoint:
			w.WriteHeader(activationStatus)
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		default:
			otherPaths = append(otherPaths, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &otherPaths
}

func newActivationTestLoader(t *testing.T, srvURL string) *providers.ConfigLoader {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := "contexts:\n  default:\n    grafana:\n      server: \"" + srvURL + "\"\n      token: test-token\n      org-id: 1\ncurrent-context: default\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgPath)
	return loader
}

func runServicesCmd(loader *providers.ConfigLoader, args ...string) error {
	root := Commands(loader)
	root.SilenceUsage = true
	root.SilenceErrors = true
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	return root.Execute()
}

// TestServicesCommands_ActivationGate covers all five `services` subcommands:
// when the activation check returns 404, the command must fail with
// activation.NotActivatedError() and must not issue any query against the
// mock server.
func TestServicesCommands_ActivationGate(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "list", args: []string{"list", "-d", "test-uid"}},
		{name: "get", args: []string{"get", "checkoutservice", "-d", "test-uid"}},
		{name: "map", args: []string{"map", "checkoutservice", "-d", "test-uid"}},
		{name: "list-operations", args: []string{"list-operations", "checkoutservice", "-d", "test-uid"}},
		{name: "list-labels", args: []string{"list-labels", "checkoutservice", "-d", "test-uid"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, otherPaths := activationGatedServer(t, http.StatusNotFound)
			loader := newActivationTestLoader(t, srv.URL)

			err := runServicesCmd(loader, tc.args...)
			require.Error(t, err)
			assert.Equal(t, activation.NotActivatedError().Error(), err.Error())
			assert.Empty(t, *otherPaths, "no query should be issued once activation check reports not-activated")
		})
	}
}

// TestServicesCommands_ActivationInconclusive covers the "inconclusive"
// branch: a non-404 non-2xx status from the activation check must surface as
// an error too (not silently proceed), and must not issue any query either.
func TestServicesCommands_ActivationInconclusive(t *testing.T) {
	srv, otherPaths := activationGatedServer(t, http.StatusInternalServerError)
	loader := newActivationTestLoader(t, srv.URL)

	err := runServicesCmd(loader, "list", "-d", "test-uid")
	require.Error(t, err)
	assert.NotEqual(t, activation.NotActivatedError().Error(), err.Error())
	assert.Empty(t, *otherPaths, "no query should be issued while the activation check is inconclusive")
}
