package config_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:usetesting // t.Setenv cannot represent an absent variable or restore it as absent.
func clearConfigCheckEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GRAFANA_SERVER",
		"GRAFANA_USER",
		"GRAFANA_PASSWORD",
		"GRAFANA_TOKEN",
		"GRAFANA_PROXY_ENDPOINT",
		"GRAFANA_ORG_ID",
		"GRAFANA_STACK_ID",
		"GRAFANA_TLS_CERT_FILE",
		"GRAFANA_TLS_KEY_FILE",
		"GRAFANA_TLS_CA_FILE",
	} {
		value, existed := os.LookupEnv(key)
		require.NoError(t, os.Unsetenv(key))
		t.Cleanup(func() {
			if existed {
				require.NoError(t, os.Setenv(key, value))
				return
			}
			require.NoError(t, os.Unsetenv(key))
		})
	}
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())
}

func writeCheckConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func TestCheckCommandReportsEveryInvalidContextBeforeFailing(t *testing.T) {
	clearConfigCheckEnvironment(t)
	path := writeCheckConfig(t, `version: 1
contexts:
  first: {}
  second: {}
current-context: first
`)

	output, err := runConfigCmd(t, "check", "--config", path)

	require.ErrorIs(t, err, gcxerrors.ErrAlreadyReported)
	require.ErrorContains(t, err, "2 configuration check(s) failed")
	assert.Contains(t, output, "Context: first")
	assert.Contains(t, output, "Context: second")
	assert.Equal(t, 2, strings.Count(output, "Configuration:"), output)
	assert.Equal(t, 2, strings.Count(output, "Connectivity:"), output)
	assert.Equal(t, 2, strings.Count(output, "Grafana version:"), output)
}

func TestCheckCommandPreservesCancellation(t *testing.T) {
	clearConfigCheckEnvironment(t)
	t.Setenv("GRAFANA_TOKEN", "test-token")
	path := writeCheckConfig(t, `version: 1
stacks:
  target:
    grafana:
      server: http://127.0.0.1:1
      org-id: 1
      auth-method: token
contexts:
  target:
    stack: target
current-context: target
`)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := runConfigCmdContext(t, ctx, "check", "--config", path)

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled, "cancellation must not be aggregated into an already-reported failure: %v", err)
	assert.NotErrorIs(t, err, gcxerrors.ErrAlreadyReported)
}

func TestCheckCommandExitStatusTracksConnectivityAndVersion(t *testing.T) {
	clearConfigCheckEnvironment(t)

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		wantOutput string
	}{
		{
			name: "success",
			handler: checkServerHandler(func(w http.ResponseWriter) {
				_, _ = w.Write([]byte(`{"version":"12.0.0"}`))
			}),
			wantOutput: "Grafana version: 12.0.0",
		},
		{
			name: "connectivity failure",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			},
			wantErr:    true,
			wantOutput: "Connectivity:",
		},
		{
			name: "version request failure",
			handler: checkServerHandler(func(w http.ResponseWriter) {
				http.Error(w, `{"message":"health unavailable"}`, http.StatusServiceUnavailable)
			}),
			wantErr:    true,
			wantOutput: "Grafana version:",
		},
		{
			name: "incompatible version",
			handler: checkServerHandler(func(w http.ResponseWriter) {
				_, _ = w.Write([]byte(`{"version":"11.6.0"}`))
			}),
			wantErr:    true,
			wantOutput: "gcx requires Grafana 12.0.0 or later",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GRAFANA_TOKEN", "test-token")
			server := httptest.NewServer(tc.handler)
			t.Cleanup(server.Close)
			path := writeCheckConfig(t, fmt.Sprintf(`version: 1
stacks:
  target:
    grafana:
      server: %q
      org-id: 1
      auth-method: token
contexts:
  target:
    stack: target
current-context: target
`, server.URL))

			output, err := runConfigCmd(t, "check", "--config", path)
			if tc.wantErr {
				require.ErrorIs(t, err, gcxerrors.ErrAlreadyReported)
			} else {
				require.NoError(t, err)
			}
			assert.Contains(t, output, tc.wantOutput)
		})
	}
}

func checkServerHandler(health func(http.ResponseWriter)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/health":
			health(w)
		case "/api":
			_, _ = w.Write([]byte(`{"kind":"APIVersions","apiVersion":"v1","versions":[]}`))
		case "/apis":
			_, _ = w.Write([]byte(`{"kind":"APIGroupList","apiVersion":"v1","groups":[]}`))
		default:
			http.NotFound(w, r)
		}
	}
}
