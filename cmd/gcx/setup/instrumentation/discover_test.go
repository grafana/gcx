package instrumentation_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/setup/instrumentation"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLoader implements fleet.ConfigLoader for testing.
type mockLoader struct {
	serverURL string
	loadErr   error
}

func (m *mockLoader) LoadCloudConfig(_ context.Context) (providers.CloudRESTConfig, error) {
	if m.loadErr != nil {
		return providers.CloudRESTConfig{}, m.loadErr
	}
	return providers.CloudRESTConfig{
		Token: "test-token",
		Stack: cloud.StackInfo{
			AgentManagementInstanceURL: m.serverURL,
			AgentManagementInstanceID:  1234,
			HMInstancePromID:           5678,
			HMInstancePromClusterID:    42,
			HMInstancePromURL:          "https://prometheus-prod.grafana.net",
			HLInstanceID:               9012,
			HLInstanceURL:              "https://logs-prod.grafana.net",
			HTInstanceID:               3456,
			HTInstanceURL:              "https://tempo-prod.grafana.net",
			HPInstanceID:               7890,
			HPInstanceURL:              "https://profiles-prod.grafana.net",
		},
	}, nil
}

// discoverTestServer starts an httptest.Server that handles SetupK8sDiscovery
// and RunK8sDiscovery endpoints with the given responses.
func discoverTestServer(t *testing.T, setupStatus, runStatus int, runBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/discovery.v1.DiscoveryService/SetupK8sDiscovery":
			w.WriteHeader(setupStatus)
			_, _ = w.Write([]byte(`{}`))
		case "/discovery.v1.DiscoveryService/RunK8sDiscovery":
			w.WriteHeader(runStatus)
			_, _ = w.Write([]byte(runBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestDiscoverCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		useServer   bool
		setupStatus int
		runStatus   int
		runBody     string
		wantErr     bool
		wantErrMsg  string
		wantStdout  string
		wantStderr  string
	}{
		{
			name:        "discover with workloads - table output",
			args:        []string{"--cluster", "prod-1", "-o", "table"},
			useServer:   true,
			setupStatus: http.StatusOK,
			runStatus:   http.StatusOK,
			runBody:     `{"items":[{"clusterName":"prod-1","namespace":"default","name":"web-abc","displayName":"web","workloadType":"deployment","instrumentationStatus":"active"}]}`,
			wantStdout:  "NAMESPACE",
		},
		{
			name:       "missing --cluster flag returns error",
			args:       []string{},
			wantErr:    true,
			wantErrMsg: "setup/instrumentation: --cluster is required",
		},
		{
			name:        "empty cluster prints informational message",
			args:        []string{"--cluster", "empty-cluster"},
			useServer:   true,
			setupStatus: http.StatusOK,
			runStatus:   http.StatusOK,
			runBody:     `{}`,
			wantStderr:  `No workloads discovered in cluster "empty-cluster"`,
		},
		{
			name:        "json output",
			args:        []string{"--cluster", "prod-1", "-o", "json"},
			useServer:   true,
			setupStatus: http.StatusOK,
			runStatus:   http.StatusOK,
			runBody:     `{"items":[{"clusterName":"prod-1","namespace":"default","name":"web-abc","displayName":"web","workloadType":"deployment","instrumentationStatus":"active"}]}`,
			wantStdout:  `"items"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &mockLoader{}

			if tt.useServer {
				srv := discoverTestServer(t, tt.setupStatus, tt.runStatus, tt.runBody)
				defer srv.Close()
				loader.serverURL = srv.URL
			}

			cmd := instrumentation.NewDiscoverCommand(loader)
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrMsg != "" {
					assert.Contains(t, err.Error(), tt.wantErrMsg)
				}
				return
			}

			require.NoError(t, err)

			if tt.wantStdout != "" {
				assert.Contains(t, stdout.String(), tt.wantStdout)
			}
			if tt.wantStderr != "" {
				assert.Contains(t, stderr.String(), tt.wantStderr)
			}
		})
	}
}
