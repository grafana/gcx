package status_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/instrumentation/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		},
	}, nil
}

func statusTestServer(t *testing.T, srvStatus int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/discovery.v1.DiscoveryService/RunK8sMonitoring" {
			w.WriteHeader(srvStatus)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestStatusCommand(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		srvStatus  int
		srvBody    string
		loadErr    error
		wantErr    bool
		wantErrMsg string
		wantOut    []string
		notWantOut []string
	}{
		{
			name:      "table output with multiple clusters",
			args:      []string{"-o", "table"},
			srvStatus: http.StatusOK,
			srvBody:   `{"clusters":[{"name":"prod-1","instrumentationStatus":"active"},{"name":"staging-2","instrumentationStatus":"inactive"}]}`,
			wantOut:   []string{"CLUSTER", "STATUS", "WORKLOADS", "PODS", "BEYLA ERRORS", "prod-1", "active", "staging-2", "inactive"},
		},
		{
			name:       "cluster filter shows only matching cluster",
			args:       []string{"-o", "table", "--cluster", "prod-1"},
			srvStatus:  http.StatusOK,
			srvBody:    `{"clusters":[{"name":"prod-1","instrumentationStatus":"active"},{"name":"staging-2","instrumentationStatus":"inactive"}]}`,
			wantOut:    []string{"prod-1", "active"},
			notWantOut: []string{"staging-2"},
		},
		{
			name:      "json output",
			args:      []string{"-o", "json"},
			srvStatus: http.StatusOK,
			srvBody:   `{"clusters":[{"name":"prod-1","instrumentationStatus":"active"}]}`,
			wantOut:   []string{`"name"`, `"prod-1"`, `"state"`, `"active"`},
		},
		{
			name:       "auth error is prefixed with instrumentation",
			args:       []string{"-o", "table"},
			loadErr:    errors.New("cloud token is required"),
			wantErr:    true,
			wantErrMsg: "instrumentation: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &mockLoader{loadErr: tt.loadErr}

			if tt.loadErr == nil {
				srv := statusTestServer(t, tt.srvStatus, tt.srvBody)
				defer srv.Close()
				loader.serverURL = srv.URL
			}

			cmd := status.NewCommandForTest(loader)
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true

			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
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

			out := stdout.String()
			for _, want := range tt.wantOut {
				assert.Contains(t, out, want, "output should contain %q", want)
			}
			for _, notWant := range tt.notWantOut {
				assert.NotContains(t, out, notWant, "output should NOT contain %q", notWant)
			}
		})
	}
}

func TestStatusCommand_EmptyClusters(t *testing.T) {
	srv := statusTestServer(t, http.StatusOK, `{}`)
	defer srv.Close()

	cmd := status.NewCommandForTest(&mockLoader{serverURL: srv.URL})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"-o", "table"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "CLUSTER")
	assert.Contains(t, out, "STATUS")
}

func TestStatusCommand_FleetAPIError(t *testing.T) {
	srv := statusTestServer(t, http.StatusInternalServerError, `{"error":"internal"}`)
	defer srv.Close()

	cmd := status.NewCommandForTest(&mockLoader{serverURL: srv.URL})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"-o", "table"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instrumentation: ")
}

func TestStatusTableCodec_Encode(t *testing.T) {
	codec := &status.TableCodec{}

	statuses := []status.ClusterStatus{
		{Name: "prod-1", State: "active", BeylaErrors: 3},
		{Name: "staging", State: "inactive", BeylaErrors: 0},
	}

	var buf bytes.Buffer
	err := codec.Encode(&buf, statuses)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "CLUSTER")
	assert.Contains(t, out, "BEYLA ERRORS")
	assert.Contains(t, out, "prod-1")
	assert.Contains(t, out, "active")
	assert.Contains(t, out, "3")
	assert.Contains(t, out, "staging")
}

func TestStatusTableCodec_WrongType(t *testing.T) {
	codec := &status.TableCodec{}

	var buf bytes.Buffer
	err := codec.Encode(&buf, "unexpected string")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []ClusterStatus")
}
