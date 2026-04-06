package instrumentation_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/setup/instrumentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statusTestServer creates an httptest.Server that handles the RunK8sMonitoring
// endpoint with the given HTTP status code and response body.
func statusTestServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/discovery.v1.DiscoveryService/RunK8sMonitoring" {
			w.WriteHeader(status)
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
			name:       "auth error is prefixed with setup/instrumentation",
			args:       []string{"-o", "table"},
			loadErr:    errors.New("cloud token is required"),
			wantErr:    true,
			wantErrMsg: "setup/instrumentation: ",
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

			cmd := instrumentation.NewStatusCommand(loader)
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

// TestStatusCommand_EmptyClusters verifies that an empty RunK8sMonitoring
// response produces only the header row with no cluster data.
func TestStatusCommand_EmptyClusters(t *testing.T) {
	srv := statusTestServer(t, http.StatusOK, `{}`)
	defer srv.Close()

	cmd := instrumentation.NewStatusCommand(&mockLoader{serverURL: srv.URL})
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

// TestStatusCommand_FleetAPIError verifies that RunK8sMonitoring HTTP errors
// are propagated with the "setup/instrumentation: " prefix.
func TestStatusCommand_FleetAPIError(t *testing.T) {
	srv := statusTestServer(t, http.StatusInternalServerError, `{"error":"internal"}`)
	defer srv.Close()

	cmd := instrumentation.NewStatusCommand(&mockLoader{serverURL: srv.URL})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"-o", "table"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "setup/instrumentation: ")
}

// TestStatusTableCodec_Encode verifies that StatusTableCodec renders the expected
// columns and values.
func TestStatusTableCodec_Encode(t *testing.T) {
	codec := &instrumentation.StatusTableCodec{}

	statuses := []instrumentation.ClusterStatus{
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

// TestStatusTableCodec_WrongType verifies that StatusTableCodec returns an error
// for unexpected input types.
func TestStatusTableCodec_WrongType(t *testing.T) {
	codec := &instrumentation.StatusTableCodec{}

	var buf bytes.Buffer
	err := codec.Encode(&buf, "unexpected string")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []ClusterStatus")
}
