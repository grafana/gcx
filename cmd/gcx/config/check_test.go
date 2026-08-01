package config_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	assert.Equal(t, 2, strings.Count(output, "Resource discovery:"), output)
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

func TestCheckCommandExitStatusTracksConnectivityVersionAndDiscovery(t *testing.T) {
	clearConfigCheckEnvironment(t)

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		wantOutput string
		wantExtra  string
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
			name: "health request failure",
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
			wantExtra:  "Resource discovery: skipped",
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
			if tc.wantExtra != "" {
				assert.Contains(t, output, tc.wantExtra)
			}
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

// requestObservation records a single HTTP request observed by the fake
// Grafana server so tests can assert on probe order, request counts, and the
// bearer credential sent to each endpoint.
type requestObservation struct {
	path string
	auth string
}

// newCheckProbeServer returns an httptest server whose /api/health and /apis
// behavior are driven by the given handlers, recording every request. It is
// safe for concurrent HTTP handler goroutines.
func newCheckProbeServer(t *testing.T, health, apis func(http.ResponseWriter)) (*httptest.Server, func() []requestObservation) {
	t.Helper()
	var mu sync.Mutex
	var observations []requestObservation
	record := func(r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		observations = append(observations, requestObservation{
			path: r.URL.Path,
			auth: r.Header.Get("Authorization"),
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/health":
			if health != nil {
				health(w)
				return
			}
		case "/apis":
			if apis != nil {
				apis(w)
				return
			}
		case "/api":
			_, _ = w.Write([]byte(`{"kind":"APIVersions","apiVersion":"v1","versions":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	getObservations := func() []requestObservation {
		mu.Lock()
		defer mu.Unlock()
		return append([]requestObservation(nil), observations...)
	}
	return server, getObservations
}

func writeCheckProbeConfig(t *testing.T, serverURL string) string {
	t.Helper()
	return writeCheckConfig(t, fmt.Sprintf(`version: 1
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
`, serverURL))
}

func TestCheckHealthProbePrecedesResourceDiscovery(t *testing.T) {
	clearConfigCheckEnvironment(t)

	tests := []struct {
		name          string
		health        func(http.ResponseWriter)
		apis          func(http.ResponseWriter)
		wantErr       bool
		wantExit      int
		wantOutputs   []string
		wantHealth    int
		wantDiscovery int
	}{
		{
			name: "health ok version 12 discovery 401",
			health: func(w http.ResponseWriter) {
				_, _ = w.Write([]byte(`{"version":"12.0.0"}`))
			},
			apis: func(w http.ResponseWriter) {
				http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			},
			wantErr:  true,
			wantExit: gcxerrors.ExitAuthFailure,
			wantOutputs: []string{
				"Connectivity: online",
				"Grafana version: 12.0.0",
				"Resource discovery: unavailable",
				"Unauthorized - code 401",
				"Make sure that the configured credentials have enough permissions",
			},
			wantHealth:    1,
			wantDiscovery: 1,
		},
		{
			name: "fully successful",
			health: func(w http.ResponseWriter) {
				_, _ = w.Write([]byte(`{"version":"12.0.0"}`))
			},
			apis: func(w http.ResponseWriter) {
				_, _ = w.Write([]byte(`{"kind":"APIGroupList","apiVersion":"v1","groups":[]}`))
			},
			wantErr: false,
			wantOutputs: []string{
				"Connectivity: online",
				"Grafana version: 12.0.0",
				"Resource discovery: available",
			},
			wantHealth:    1,
			wantDiscovery: 1,
		},
		{
			name: "health failure skips discovery",
			health: func(w http.ResponseWriter) {
				http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			},
			wantErr:  true,
			wantExit: gcxerrors.ExitGeneralError,
			wantOutputs: []string{
				"Connectivity:",
				"Grafana version: skipped",
				"Resource discovery: skipped",
			},
			wantHealth:    1,
			wantDiscovery: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GRAFANA_TOKEN", "test-token")
			server, getObs := newCheckProbeServer(t, tc.health, tc.apis)
			path := writeCheckProbeConfig(t, server.URL)

			output, err := runConfigCmd(t, "check", "--config", path)
			if tc.wantErr {
				require.ErrorIs(t, err, gcxerrors.ErrAlreadyReported)
				exitCode, ok := gcxerrors.AlreadyReportedExitCode(err)
				require.True(t, ok, "failure must retain the already-reported exit code: %v", err)
				assert.Equal(t, tc.wantExit, exitCode)
			} else {
				require.NoError(t, err)
			}
			for _, want := range tc.wantOutputs {
				assert.Contains(t, output, want)
			}

			obs := getObs()
			healthCount := 0
			discoveryCount := 0
			seenHealth := false
			discoveryBeforeHealth := false
			for _, o := range obs {
				switch o.path {
				case "/api/health":
					healthCount++
					seenHealth = true
					assert.Equal(t, "Bearer test-token", o.auth, "health must be bearer-authenticated")
				case "/api":
					assert.Equal(t, "Bearer test-token", o.auth, "core resource discovery must be bearer-authenticated")
					if !seenHealth {
						discoveryBeforeHealth = true
					}
				case "/apis":
					discoveryCount++
					assert.Equal(t, "Bearer test-token", o.auth, "discovery must be bearer-authenticated")
					if !seenHealth {
						discoveryBeforeHealth = true
					}
				}
			}
			assert.Equal(t, tc.wantHealth, healthCount, "unexpected /api/health request count")
			assert.Equal(t, tc.wantDiscovery, discoveryCount, "unexpected /apis request count")
			assert.False(t, discoveryBeforeHealth, "resource discovery probed before /api/health")
		})
	}
}
