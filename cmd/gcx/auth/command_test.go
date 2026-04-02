package auth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"

	authcmd "github.com/grafana/gcx/cmd/gcx/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogin_missingServer(t *testing.T) {
	cfg := `current-context: test
contexts:
  test:
    grafana: {}`

	configFile := testutils.CreateTempFile(t, cfg)

	tc := testutils.CommandTestCase{
		Cmd:     authcmd.Command(),
		Command: []string{"login", "--config", configFile},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandErrorContains("grafana.server is not configured"),
		},
	}
	tc.Run(t)
}

func TestLogin_noContext(t *testing.T) {
	configFile := testutils.CreateTempFile(t, "contexts:")

	tc := testutils.CommandTestCase{
		Cmd:     authcmd.Command(),
		Command: []string{"login", "--config", configFile},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandErrorContains("grafana.server is not configured"),
		},
	}
	tc.Run(t)
}

// syncBuffer is a goroutine-safe bytes.Buffer for capturing output
// from a command running in a separate goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestLogin_successWritesTokensToConfig(t *testing.T) {
	t.Skip("Flaky: localhost callback server race condition causes 'connection refused' in CI (see #327)")

	// Mock exchange server that returns tokens.
	exchangeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/v1/auth/exchange" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"data": map[string]string{
				"token":              "gat_test_token",
				"email":              "user@example.com",
				"api_endpoint":       "http://127.0.0.1",
				"expires_at":         time.Now().Add(time.Hour).Format(time.RFC3339),
				"refresh_token":      "gar_test_refresh",
				"refresh_expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
		})
	}))
	defer exchangeSrv.Close()

	cfg := `current-context: test
contexts:
  test:
    grafana:
      server: ` + exchangeSrv.URL

	configFile := testutils.CreateTempFile(t, cfg)

	cmd := authcmd.Command()
	cmd.SilenceErrors = true

	stdout := &syncBuffer{}
	stderr := &syncBuffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"login", "--config", configFile})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	// Wait for the callback server to print the auth URL.
	var port, state string
	callbackRe := regexp.MustCompile(`callback_port=(\d+)&state=([0-9a-f]+)`)
	require.Eventually(t, func() bool {
		matches := callbackRe.FindStringSubmatch(stderr.String())
		if matches == nil {
			return false
		}
		port = matches[1]
		state = matches[2]
		return true
	}, 5*time.Second, 50*time.Millisecond, "callback server did not start")

	// Simulate the browser redirect to the callback server.
	callbackURL := "http://127.0.0.1:" + port + "/callback?state=" + state + "&code=test_code&endpoint=" + exchangeSrv.URL
	callbackReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, callbackURL, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(callbackReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Wait for the command to finish.
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("login command did not complete")
	}

	// Verify tokens were written to config.
	saved, err := config.Load(t.Context(), config.ExplicitConfigFile(configFile))
	require.NoError(t, err)

	ctx := saved.GetCurrentContext()
	require.NotNil(t, ctx)
	require.NotNil(t, ctx.Grafana)
	assert.Equal(t, "gat_test_token", ctx.Grafana.OAuthToken)
	assert.Equal(t, "gar_test_refresh", ctx.Grafana.OAuthRefreshToken)
	assert.NotEmpty(t, ctx.Grafana.OAuthTokenExpiresAt)
	assert.NotEmpty(t, ctx.Grafana.OAuthRefreshExpiresAt)
	assert.Contains(t, stdout.String(), "Authenticated as user@example.com")
}
