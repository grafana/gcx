//nolint:testpackage // White-box: structuredMissingFieldsError is unexported.
package login

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/agent"
	gcxerrors "github.com/grafana/gcx/internal/gcxerrors"
	internallogin "github.com/grafana/gcx/internal/login"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMissingServerWithTokenOmitsTheCloudOAuthRoute keeps the suggestions
// usable: --cloud --oauth conflicts with --token and --basic-auth.
func TestMissingServerWithTokenOmitsTheCloudOAuthRoute(t *testing.T) {
	t.Parallel()

	err := structuredMissingFieldsError(&internallogin.ErrNeedInput{Fields: []string{"server"}}, true)
	var det gcxerrors.DetailedError
	require.ErrorAs(t, err, &det)
	joined := strings.Join(det.Suggestions, "\n")
	assert.Contains(t, joined, "--server")
	assert.NotContains(t, joined, "--cloud --oauth")
	assert.NotContains(t, joined, "gcx signup")
}

// lockedBuffer lets the test read the command's stderr while it runs.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestLoginCloudOAuthWithoutServerStartsTheLauncher covers the agent path into
// the new journey: --cloud --oauth without a server starts the Grafana Cloud
// browser login instead of failing on the missing server. --oauth alone still
// asks for a server, and the error now names the --cloud --oauth route.
func TestLoginCloudOAuthWithoutServerStartsTheLauncher(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "true") // no browser launch; the URL is printed
	t.Setenv("GCX_KEYCHAIN", "off")
	for _, key := range []string{"GRAFANA_SERVER", "GRAFANA_TOKEN", "GRAFANA_CLOUD_API_URL", "GRAFANA_CLOUD_OAUTH_URL"} {
		unsetEnvForTest(t, key)
	}
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	t.Run("--cloud --oauth", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		var stderr lockedBuffer
		cmd := Command()
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"--cloud", "--oauth", "--config", path})

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- cmd.ExecuteContext(ctx) }()

		const launcher = "https://grafana.com/launch/a/grafana-assistant-app/cli/auth?"
		require.Eventually(t, func() bool { return strings.Contains(stderr.String(), launcher) },
			10*time.Second, 10*time.Millisecond, "stderr never showed the launcher URL: %q", stderr.String())
		require.Eventually(t, func() bool { return strings.Contains(stderr.String(), "sign in to Grafana Cloud, choose a stack") },
			10*time.Second, 10*time.Millisecond, "stderr never showed the launcher steps: %q", stderr.String())
		cancel()

		select {
		case err := <-done:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(10 * time.Second):
			t.Fatal("login did not stop after cancellation")
		}
	})

	t.Run("--oauth alone", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		cmd := Command()
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--oauth", "--config", path})

		err := cmd.ExecuteContext(t.Context())
		var det gcxerrors.DetailedError
		require.ErrorAs(t, err, &det)
		assert.Contains(t, det.Details, "server")
		joined := strings.Join(det.Suggestions, "\n")
		assert.Contains(t, joined, "--cloud --oauth")
		assert.Contains(t, joined, "gcx signup", "a person with no account needs the signup route")
		assert.NotContains(t, joined, "create a free account", "--cloud --oauth no longer promises account creation")
	})
}

// TestServerPromptPointsAtSignup keeps main's server prompt, where an empty
// answer signs in to Grafana Cloud, and adds the signup route for a person
// with no account. A credential bound to one server needs a URL instead.
func TestServerPromptPointsAtSignup(t *testing.T) {
	t.Parallel()

	open := serverPromptDescription(&internallogin.Options{})
	assert.Contains(t, open, "Leave empty to select your Grafana Cloud instance interactively")
	assert.Contains(t, open, "gcx signup")

	for _, opts := range []*internallogin.Options{
		{Inputs: internallogin.Inputs{GrafanaToken: "glsa_x"}},
		{Inputs: internallogin.Inputs{UseBasicAuth: true}},
	} {
		bound := serverPromptDescription(opts)
		assert.NotContains(t, bound, "Leave empty")
		assert.NotContains(t, bound, "gcx signup")
	}
}
