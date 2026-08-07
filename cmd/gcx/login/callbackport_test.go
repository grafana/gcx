// White-box: runCloudOAuth and loginOpts are unexported.
//
//nolint:testpackage // see cmd/gcx/login/command_test.go
package login

import (
	"context"
	"testing"

	internalauth "github.com/grafana/gcx/internal/auth"
	internallogin "github.com/grafana/gcx/internal/login"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1148: --oauth-callback-port reached only the Grafana stack OAuth leg. The
// Cloud leg built its GCOMOptions without a port and fell back to auto-picking
// from 54321-54399, which is unreachable when only one port is forwarded
// between a remote host and the browser.
//
// runCloudOAuth is driven directly rather than through `gcx login --cloud`:
// the Cloud OAuth leg is optional and only runs when the interactive prompt
// selects it, and --yes skips it entirely.
func TestRunCloudOAuthPassesCallbackPortToTheCloudLeg(t *testing.T) {
	t.Parallel()

	var gotFlowOpts internalauth.GCOMOptions
	opts := internallogin.Options{
		Inputs: internallogin.Inputs{
			Server:            "https://my-stack.grafana.net",
			OAuthCallbackPort: 8250,
		},
		Hooks: internallogin.Hooks{
			NewCloudAuthFlow: func(flowOpts internalauth.GCOMOptions) internallogin.CloudAuthFlow {
				gotFlowOpts = flowOpts
				return &stubCloudAuthFlow{result: &internalauth.GCOMResult{AccessToken: "oauth-token"}}
			},
		},
	}

	require.NoError(t, runCloudOAuth(context.Background(), &opts))
	assert.Equal(t, 8250, gotFlowOpts.Port, "a port fixed for the login must apply to the Cloud leg too")
}

func TestRunCloudOAuthLeavesPortUnsetWhenNoneRequested(t *testing.T) {
	t.Parallel()

	var gotFlowOpts internalauth.GCOMOptions
	opts := internallogin.Options{
		Inputs: internallogin.Inputs{Server: "https://my-stack.grafana.net"},
		Hooks: internallogin.Hooks{
			NewCloudAuthFlow: func(flowOpts internalauth.GCOMOptions) internallogin.CloudAuthFlow {
				gotFlowOpts = flowOpts
				return &stubCloudAuthFlow{result: &internalauth.GCOMResult{AccessToken: "oauth-token"}}
			},
		},
	}

	require.NoError(t, runCloudOAuth(context.Background(), &opts))
	assert.Zero(t, gotFlowOpts.Port, "without the flag the Cloud leg must still auto-pick")
}

func TestLoginValidateCallbackPortCombinations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantErr  string
		wantPort int
	}{
		{
			name:     "valid fixed port",
			args:     []string{"--server", "https://my-stack.grafana.net", "--oauth-callback-port", "8250"},
			wantPort: 8250,
		},
		{
			name: "zero means auto-pick",
			args: []string{"--server", "https://my-stack.grafana.net"},
		},
		{
			name:    "port below range",
			args:    []string{"--server", "https://my-stack.grafana.net", "--oauth-callback-port", "-1"},
			wantErr: "invalid --oauth-callback-port",
		},
		{
			name:    "port above range",
			args:    []string{"--server", "https://my-stack.grafana.net", "--oauth-callback-port", "65536"},
			wantErr: "invalid --oauth-callback-port",
		},
		{
			name:    "manual mode has no callback server",
			args:    []string{"--server", "https://my-stack.grafana.net", "--oauth-manual", "--oauth-callback-port", "8250"},
			wantErr: "conflicting OAuth callback options",
		},
		{
			// Unlike `gcx cloud login`, unified login still runs the stack
			// OAuth leg here, so the port is meaningful. Rejecting this pair
			// would break the case the flag exists for.
			name:     "a Cloud token coexists with a port the stack leg needs",
			args:     []string{"--server", "https://my-stack.grafana.net", "--oauth", "--cloud-token", "glc_abc", "--oauth-callback-port", "8250"},
			wantPort: 8250,
		},
		{
			// And the mirror image: a stack service-account token with the
			// port reserved for the optional Cloud OAuth leg.
			name:     "a stack token coexists with a port the Cloud leg needs",
			args:     []string{"--server", "https://my-stack.grafana.net", "--token", "glsa_abc", "--oauth-callback-port", "8250"},
			wantPort: 8250,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := &loginOpts{}
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			opts.setup(flags)
			require.NoError(t, flags.Parse(tt.args))

			err := opts.Validate(nil)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantPort, opts.OAuthCallbackPort)
		})
	}
}
