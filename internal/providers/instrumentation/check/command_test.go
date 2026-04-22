package check_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/instrumentation/check"
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
		},
	}, nil
}

func checkTestServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/instrumentation.v1.InstrumentationService/GetK8SInstrumentation" {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestCheckCommand_AllChecksPass(t *testing.T) {
	srv := checkTestServer(t, http.StatusOK, `{"cluster":{"name":"prod-1","costmetrics":true}}`)
	defer srv.Close()

	cmd := check.NewCommandForTest(&mockLoader{serverURL: srv.URL})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"prod-1"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "cluster agent registered")
	assert.Contains(t, stdout.String(), "OK")
}

func TestCheckCommand_AllChecksPass_JSON(t *testing.T) {
	srv := checkTestServer(t, http.StatusOK, `{"cluster":{"name":"prod-1","costmetrics":true}}`)
	defer srv.Close()

	cmd := check.NewCommandForTest(&mockLoader{serverURL: srv.URL})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"-o", "json", "prod-1"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), `"check": "cluster agent registered"`)
	assert.Contains(t, stdout.String(), `"status": "OK"`)
}

func TestCheckCommand_CheckFails(t *testing.T) {
	srv := checkTestServer(t, http.StatusNotFound, `{}`)
	defer srv.Close()

	cmd := check.NewCommandForTest(&mockLoader{serverURL: srv.URL})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"unknown-cluster"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, stdout.String(), "cluster agent registered")
	assert.Contains(t, stdout.String(), "FAIL")
	assert.Contains(t, err.Error(), "instrumentation: ")
}

func TestCheckCommand_CheckFails_JSON(t *testing.T) {
	srv := checkTestServer(t, http.StatusNotFound, `{}`)
	defer srv.Close()

	cmd := check.NewCommandForTest(&mockLoader{serverURL: srv.URL})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"-o", "json", "unknown-cluster"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, stdout.String(), `"check": "cluster agent registered"`)
	assert.Contains(t, stdout.String(), `"status": "FAIL"`)
	assert.Contains(t, err.Error(), "instrumentation: ")
}

func TestCheckCommand_MissingArg(t *testing.T) {
	cmd := check.NewCommandForTest(&mockLoader{})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg(s)")
}
