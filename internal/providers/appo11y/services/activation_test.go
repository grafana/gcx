package services //nolint:testpackage // Tests cover unexported activation-gate wiring (runServicesCmd, activationGatedServer).

// Guards the App Observability activation pre-flight (#1309 PR1): every
// `services` subcommand runs activation.Gate immediately after loading
// config, but Gate never blocks — target_info/span-metrics queries need
// only the datasource, not the App Observability Grafana app plugin itself.
// A definitive or inconclusive activation result surfaces as a stderr
// warning, and the command's own PromQL/Tempo query always proceeds.

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/appo11y/activation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const activationEndpoint = "/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings"

// activationGatedServer starts a mock Grafana server that returns
// activationStatus for the plugin-settings activation check and records
// every subsequent query-ish request path so tests can assert the query
// still ran despite the activation result. /bootdata is answered (like
// list_test.go's runListCmd) but excluded from the recorded paths: it's
// namespace/cloud-stack discovery LoadContextAndConfig performs before the
// activation check ever runs, not a PromQL/Tempo query the gated commands
// issue. Guarded by a mutex: runList fans one goroutine out per target_info
// metric, so more than one can hit this handler concurrently.
func activationGatedServer(t *testing.T, activationStatus int) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var otherPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case activationEndpoint:
			w.WriteHeader(activationStatus)
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		default:
			mu.Lock()
			otherPaths = append(otherPaths, r.URL.Path)
			mu.Unlock()
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

func runServicesCmd(loader *providers.ConfigLoader, args ...string) (string, error) {
	root := Commands(loader)
	root.SilenceUsage = true
	root.SilenceErrors = true
	var stdout, stderrBuf bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderrBuf)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	err := root.Execute()
	return stderrBuf.String(), err
}

// TestServicesCommands_ActivationNeverBlocks covers all five `services`
// subcommands against both a definitive-not-activated (404) and an
// inconclusive (500) activation result: neither blocks the command, and
// both still issue the command's own query.
func TestServicesCommands_ActivationNeverBlocks(t *testing.T) {
	tests := []struct {
		name             string
		args             []string
		activationStatus int
		wantWarnContains string
	}{
		{name: "list/not-activated", args: []string{"list", "-d", "test-uid"}, activationStatus: http.StatusNotFound, wantWarnContains: activation.NotActivatedError().Error()},
		{name: "get/not-activated", args: []string{"get", "checkoutservice", "-d", "test-uid"}, activationStatus: http.StatusNotFound, wantWarnContains: activation.NotActivatedError().Error()},
		{name: "map/not-activated", args: []string{"map", "checkoutservice", "-d", "test-uid"}, activationStatus: http.StatusNotFound, wantWarnContains: activation.NotActivatedError().Error()},
		{name: "list-operations/not-activated", args: []string{"list-operations", "checkoutservice", "-d", "test-uid"}, activationStatus: http.StatusNotFound, wantWarnContains: activation.NotActivatedError().Error()},
		{name: "list-labels/not-activated", args: []string{"list-labels", "checkoutservice", "-d", "test-uid"}, activationStatus: http.StatusNotFound, wantWarnContains: activation.NotActivatedError().Error()},
		{name: "list/inconclusive", args: []string{"list", "-d", "test-uid"}, activationStatus: http.StatusInternalServerError, wantWarnContains: "activation check failed"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, otherPaths := activationGatedServer(t, tc.activationStatus)
			loader := newActivationTestLoader(t, srv.URL)

			stderr, _ := runServicesCmd(loader, tc.args...)
			assert.NotEmpty(t, *otherPaths, "the command's own query must still be issued regardless of the activation result")
			assert.Contains(t, stderr, tc.wantWarnContains)
		})
	}
}
