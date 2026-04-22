package setup_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/instrumentation/setup"
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

// setupTestServerCapture creates a test server that captures the SetK8SInstrumentation request body.
// The captured body can be decoded from capturedBody after cmd.Execute().
func setupTestServerCapture(t *testing.T, setStatus int) (*httptest.Server, *[]byte, *sync.Mutex) {
	t.Helper()
	var captured []byte
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/SetK8SInstrumentation") {
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			captured = body
			mu.Unlock()
			w.WriteHeader(setStatus)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	return srv, &captured, &mu
}

func setupTestServer(t *testing.T, setStatus int) *httptest.Server {
	t.Helper()
	srv, _, _ := setupTestServerCapture(t, setStatus)
	return srv
}

func TestSetupCommand_Defaults(t *testing.T) {
	srv := setupTestServer(t, http.StatusOK)
	defer srv.Close()

	cmd := setup.NewCommandForTest(&mockLoader{serverURL: srv.URL})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"prod-1", "--defaults"})

	err := cmd.Execute()
	require.NoError(t, err)

	// F8: helm hint goes to stderr, not stdout
	out := stderr.String()
	assert.Contains(t, out, "prod-1")
	assert.Contains(t, out, "helm")
	// Access policy token scope hint must be present (U1 gap closure)
	assert.Contains(t, out, "metrics:read")
	assert.Contains(t, out, "set:alloy-data-write")
}

// TestSetupCommand_NonInteractiveRequiresCluster verifies that --defaults without
// a cluster positional argument produces a clear error (cluster name can only be
// collected via the interactive prompt).
func TestSetupCommand_NonInteractiveRequiresCluster(t *testing.T) {
	cmd := setup.NewCommandForTest(&mockLoader{})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--defaults"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster name required")
}

// TestSetupCommand_TooManyArgs verifies the command still rejects >1 positional arg.
func TestSetupCommand_TooManyArgs(t *testing.T) {
	cmd := setup.NewCommandForTest(&mockLoader{})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"one", "two"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts at most 1 arg")
}

func TestSetupCommand_APIError(t *testing.T) {
	srv := setupTestServer(t, http.StatusInternalServerError)
	defer srv.Close()

	cmd := setup.NewCommandForTest(&mockLoader{serverURL: srv.URL})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"bad-cluster", "--defaults"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instrumentation: ")
}

// The interactive wizard is driven by charmbracelet/huh; its form runner is a
// TUI (bubbletea) program that is not meaningfully testable from unit tests.
// Non-interactive (--defaults) coverage above exercises the full apply path.
// The wizard itself is exercised manually and via accessible-mode fallback in
// piped shells.
