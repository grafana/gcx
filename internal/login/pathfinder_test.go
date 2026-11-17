package login_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/login"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestDetectPathfinder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{name: "enabled", status: http.StatusOK, body: `{"id":"grafana-pathfinder-app","enabled":true}`, want: true},
		{name: "installed_but_disabled", status: http.StatusOK, body: `{"id":"grafana-pathfinder-app","enabled":false}`, want: false},
		{name: "not_installed", status: http.StatusNotFound, body: `{"message":"Plugin not found"}`, want: false},
		{name: "forbidden", status: http.StatusForbidden, body: `{}`, want: false},
		{name: "server_error", status: http.StatusInternalServerError, body: ``, want: false},
		{name: "undecodable_body", status: http.StatusOK, body: `not json`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotPath, gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotAuth = r.Header.Get("Authorization")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)

			restCfg := config.NamespacedRESTConfig{
				Config: rest.Config{Host: srv.URL, BearerToken: "glsa_test"},
			}

			assert.Equal(t, tt.want, login.DetectPathfinder(context.Background(), restCfg))
			assert.Equal(t, "/api/plugins/grafana-pathfinder-app/settings", gotPath)
			assert.Equal(t, "Bearer glsa_test", gotAuth, "probe must reuse the login credentials")
		})
	}
}

func TestDetectPathfinder_UnreachableServerReportsFalse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.NotFoundHandler())
	host := srv.URL
	srv.Close()

	restCfg := config.NamespacedRESTConfig{Config: rest.Config{Host: host}}
	assert.False(t, login.DetectPathfinder(context.Background(), restCfg))
}

func TestPathfinderURL(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "https://mystack.grafana.net/a/grafana-pathfinder-app", login.PathfinderURL("https://mystack.grafana.net"))
	assert.Equal(t, "https://mystack.grafana.net/a/grafana-pathfinder-app", login.PathfinderURL("https://mystack.grafana.net/"))
}

func TestRun_DetectPathfinder(t *testing.T) {
	usePlaintextCredentialStorage(t)

	tests := []struct {
		name          string
		target        login.Target
		detect        bool
		validateErr   error
		probeResult   bool
		wantProbed    bool
		wantInstalled bool
	}{
		{name: "cloud_installed", target: login.TargetCloud, detect: true, probeResult: true, wantProbed: true, wantInstalled: true},
		{name: "cloud_not_installed", target: login.TargetCloud, detect: true, probeResult: false, wantProbed: true, wantInstalled: false},
		{name: "cloud_not_requested", target: login.TargetCloud, detect: false, probeResult: true, wantProbed: false, wantInstalled: false},
		{name: "onprem_installed", target: login.TargetOnPrem, detect: true, probeResult: true, wantProbed: true, wantInstalled: true},
		{name: "onprem_not_requested", target: login.TargetOnPrem, detect: false, probeResult: true, wantProbed: false, wantInstalled: false},
		{
			// A rejected CAP token still completes the login, but validation
			// did not fully succeed, so the advisory probe is skipped.
			name: "cloud_cap_rejected_skips_probe", target: login.TargetCloud, detect: true,
			validateErr: &login.GCOMStackError{Slug: "mystack", Status: http.StatusUnauthorized, Cause: errors.New("unauthorized")}, probeResult: true,
			wantProbed: false, wantInstalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			probed := false
			opts := login.Options{
				Inputs: login.Inputs{
					Server:          "https://mystack.grafana.net",
					Target:          tt.target,
					GrafanaToken:    "glsa_test",
					CloudToken:      "cap-token",
					ProbePathfinder: tt.detect,
				},
				Hooks: login.Hooks{
					ConfigSource: configSource(dir),
					ValidateFn: func(context.Context, login.Options, config.NamespacedRESTConfig) (string, error) {
						return "12.3.0", tt.validateErr
					},
					PathfinderFn: func(context.Context, config.NamespacedRESTConfig) bool {
						probed = true
						return tt.probeResult
					},
				},
			}
			if tt.target == login.TargetOnPrem {
				opts.CloudToken = ""
			}

			result, err := login.Run(context.Background(), &opts)
			require.NoError(t, err)
			assert.Equal(t, tt.wantProbed, probed, "probe invocation")
			assert.Equal(t, tt.wantInstalled, result.PathfinderInstalled)

			// A positive detection is cached onto the persisted context so
			// later logins can skip the probe and the one-time hint.
			persisted, loadErr := config.Load(context.Background(), configSource(dir))
			require.NoError(t, loadErr)
			cached := false
			for _, c := range persisted.Contexts {
				if c.Grafana != nil && c.Grafana.PathfinderInstalled {
					cached = true
				}
			}
			assert.Equal(t, tt.wantInstalled, cached, "cached PathfinderInstalled")
		})
	}
}
