package server_test

import (
	"net/http"
	"testing"

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
