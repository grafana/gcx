//nolint:testpackage // white-box: loginCmd and newGCOMOAuthFlow are unexported
package cloud

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1148: `gcx cloud login` had no callback-port option at all, so a user who
// could only reach one forwarded port between a remote host and their browser
// could not complete this command.

// captureGCOMOptions swaps the flow factory and records the options it is
// handed, without ever starting a browser flow.
func captureGCOMOptions(t *testing.T) (*auth.GCOMOptions, *bool) {
	t.Helper()
	var (
		captured auth.GCOMOptions
		started  bool
	)
	previous := newGCOMOAuthFlow
	newGCOMOAuthFlow = func(opts auth.GCOMOptions) gcomOAuthFlow {
		captured = opts
		started = true
		return gcomOAuthFlowFunc(func(context.Context) (*auth.GCOMResult, error) {
			return &auth.GCOMResult{AccessToken: "glc_test", Scope: "stacks:read"}, nil
		})
	}
	t.Cleanup(func() { newGCOMOAuthFlow = previous })
	return &captured, &started
}

func TestCloudLoginPassesCallbackPortToTheOAuthFlow(t *testing.T) {
	isolateCloudConfigEnv(t)
	captured, started := captureGCOMOptions(t)

	cmd := loginCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--oauth-callback-port", "8250"})
	require.NoError(t, cmd.ExecuteContext(t.Context()))

	require.True(t, *started, "the OAuth flow must run")
	assert.Equal(t, 8250, captured.Port, "the requested callback port must reach the OAuth flow")
}

// Auto-pick stays the default: an invocation with no flag is unchanged.
func TestCloudLoginDefaultsToAutomaticPortSelection(t *testing.T) {
	isolateCloudConfigEnv(t)
	captured, started := captureGCOMOptions(t)

	cmd := loginCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	require.NoError(t, cmd.ExecuteContext(t.Context()))

	require.True(t, *started)
	assert.Zero(t, captured.Port, "without the flag the flow must still auto-pick")
}

// Every rejection has to happen before the browser opens or a listener binds.
func TestCloudLoginRejectsUnusablePortCombinationsBeforeStartingOAuth(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "port below range",
			args:    []string{"--oauth-callback-port", "-1"},
			wantErr: "invalid --oauth-callback-port",
		},
		{
			name:    "port above range",
			args:    []string{"--oauth-callback-port", "65536"},
			wantErr: "invalid --oauth-callback-port",
		},
		{
			// A Cloud Access Policy token skips OAuth entirely, so this
			// command has no callback server for the port to apply to.
			name:    "port with a cloud token",
			args:    []string{"--cloud-token", "glc_abc", "--oauth-callback-port", "8250"},
			wantErr: "conflicting OAuth callback options",
		},
		{
			name:    "port with manual mode",
			args:    []string{"--oauth-manual", "--oauth-callback-port", "8250"},
			wantErr: "there is no port to fix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateCloudConfigEnv(t)
			_, started := captureGCOMOptions(t)

			cmd := loginCmd()
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetArgs(tt.args)

			err := cmd.ExecuteContext(t.Context())
			require.ErrorContains(t, err, tt.wantErr)
			assert.False(t, *started, "validation must fail before any browser or network side effect")
		})
	}
}
