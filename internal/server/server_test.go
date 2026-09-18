package server_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/server"
	"github.com/stretchr/testify/require"
)

func TestCheckOrigin_LoopbackOriginsAllowed(t *testing.T) {
	checker := server.MakeOriginChecker("")

	allowed := []string{
		"http://127.0.0.1:8080",
		"http://localhost:8080",
		"http://[::1]:8080",
	}
	for _, origin := range allowed {
		t.Run(origin, func(t *testing.T) {
			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/livereload", nil)
			req.Header.Set("Origin", origin)
			require.True(t, checker(req), "expected %q to be allowed", origin)
		})
	}
}

func TestCheckOrigin_ConfiguredAddressAllowed(t *testing.T) {
	checker := server.MakeOriginChecker("dev.local")

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/livereload", nil)
	req.Header.Set("Origin", "http://dev.local")
	require.True(t, checker(req))
}

func TestCheckOrigin_WildcardBindRejectsBindAddressOrigin(t *testing.T) {
	// When the server binds to a wildcard address, an Origin whose hostname
	// equals that wildcard (e.g. http://0.0.0.0, which some browsers route to
	// loopback) must NOT be allow-listed — otherwise the hijacking protection
	// is defeated for shared-network binds.
	cases := map[string]string{
		"0.0.0.0": "http://0.0.0.0:8080",
		"::":      "http://[::]:8080",
	}
	for listenAddr, origin := range cases {
		t.Run(listenAddr, func(t *testing.T) {
			checker := server.MakeOriginChecker(listenAddr)

			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/livereload", nil)
			req.Header.Set("Origin", origin)
			require.False(t, checker(req), "expected %q to be rejected for wildcard bind %q", origin, listenAddr)
		})
	}
}

func TestCheckOrigin_ExternalOriginsRejected(t *testing.T) {
	checker := server.MakeOriginChecker("")

	rejected := []string{
		"http://evil.example",
		"http://otherhost:8080",
		"https://attacker.com",
	}
	for _, origin := range rejected {
		t.Run(origin, func(t *testing.T) {
			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/livereload", nil)
			req.Header.Set("Origin", origin)
			require.False(t, checker(req), "expected %q to be rejected", origin)
		})
	}
}

func TestCheckOrigin_MissingOriginHeaderAllowed(t *testing.T) {
	checker := server.MakeOriginChecker("")

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/livereload", nil)
	require.True(t, checker(req), "same-origin requests without Origin header should be allowed")
}

// TestStartUsesPreparedRESTConfig covers every auth method the dev proxy
// accepts, including OAuth: Start no longer derives its own REST config, so it
// must not re-reject an auth method the caller already resolved.
func TestStartUsesPreparedRESTConfig(t *testing.T) {
	tests := []struct {
		name    string
		grafana config.GrafanaConfig
	}{
		{
			name: "token",
			grafana: config.GrafanaConfig{
				AuthMethod: "token",
				APIToken:   "selected-token",
			},
		},
		{
			name: "basic",
			grafana: config.GrafanaConfig{
				AuthMethod: "basic",
				User:       "selected-user",
				Password:   "selected-password",
			},
		},
		{
			name: "oauth",
			grafana: config.GrafanaConfig{
				AuthMethod:          "oauth",
				OAuthToken:          "oauth-access",
				OAuthRefreshToken:   "oauth-refresh",
				OAuthTokenExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			grafanaCfg := tc.grafana
			grafanaCfg.Server = "https://stack.example.invalid"
			grafanaCfg.ProxyEndpoint = "https://proxy.example.invalid"
			grafanaCfg.StackID = 12345
			cfgCtx := &config.Context{Name: "dev", Grafana: &grafanaCfg}

			restCfg, err := cfgCtx.ToRESTConfig(context.Background())
			require.NoError(t, err)

			srv := server.New(server.Config{ListenAddr: "127.0.0.1"}, restCfg, cfgCtx, resources.NewResources())

			// Cancelling up front shuts the listener down as soon as it starts,
			// so Start only exercises validation and proxy wiring.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			require.NoError(t, srv.Start(ctx))
		})
	}
}
