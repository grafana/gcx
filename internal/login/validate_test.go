package login //nolint:testpackage // White-box test: needs access to unexported validator struct for stub injection.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubGrafanaClient implements grafanaClient for testing.
type stubGrafanaClient struct {
	version *semver.Version
	raw     string
	err     error
}

func (s *stubGrafanaClient) GetVersion(_ context.Context) (*semver.Version, string, error) {
	raw := s.raw
	if raw == "" && s.version != nil {
		raw = s.version.String()
	}
	return s.version, raw, s.err
}

// stubGCOMClient implements gcomClient for testing.
type stubGCOMClient struct {
	called bool
	err    error
}

func (s *stubGCOMClient) GetStack(_ context.Context, _ string) (cloud.StackInfo, error) {
	s.called = true
	return cloud.StackInfo{}, s.err
}

func mustVersion(v string) *semver.Version {
	ver, err := semver.NewVersion(v)
	if err != nil {
		panic(fmt.Sprintf("invalid semver %q: %v", v, err))
	}
	return ver
}

func TestValidate(t *testing.T) {
	okDiscovery := func(_ context.Context, _ config.NamespacedRESTConfig) error { return nil }
	failDiscovery := func(_ context.Context, _ config.NamespacedRESTConfig) error {
		return errors.New("connection refused")
	}

	v12 := mustVersion("12.0.0")
	v11 := mustVersion("11.5.0")

	tests := []struct {
		name        string
		opts        Options
		grafana     grafanaClient
		discovery   func(context.Context, config.NamespacedRESTConfig) error
		gcom        gcomClient
		wantErr     bool
		wantErrSub  string
		wantErrAs   any // pointer-to-pointer for errors.As; nil to skip
		wantGCOMHit bool
	}{
		{
			name:       "health check failure",
			opts:       Options{Inputs: Inputs{Target: TargetOnPrem}},
			grafana:    &stubGrafanaClient{err: errors.New("connection refused")},
			discovery:  okDiscovery,
			wantErr:    true,
			wantErrSub: "health check failed",
			wantErrAs:  new(*HealthCheckError),
		},
		{
			name:       "K8s discovery failure",
			opts:       Options{Inputs: Inputs{Target: TargetOnPrem}},
			grafana:    &stubGrafanaClient{version: v12},
			discovery:  failDiscovery,
			wantErr:    true,
			wantErrSub: "kubernetes API unavailable",
			wantErrAs:  new(*K8sDiscoveryError),
		},
		{
			name:       "version below 12 returns named error",
			opts:       Options{Inputs: Inputs{Target: TargetOnPrem}},
			grafana:    &stubGrafanaClient{version: v11},
			discovery:  okDiscovery,
			wantErr:    true,
			wantErrSub: "version check failed",
			wantErrAs:  new(*VersionCheckError),
		},
		{
			name:        "GCOM check failure",
			opts:        Options{Inputs: Inputs{Target: TargetCloud, CloudToken: "cap-token", Server: "https://mystack.grafana.net"}},
			grafana:     &stubGrafanaClient{version: v12},
			discovery:   okDiscovery,
			gcom:        &stubGCOMClient{err: &cloud.GCOMHTTPError{Status: 403, Body: "denied"}},
			wantErr:     true,
			wantErrSub:  "GCOM check failed",
			wantErrAs:   new(*GCOMStackError),
			wantGCOMHit: true,
		},
		{
			name:        "Cloud + CAP token: GCOM GetStack is called",
			opts:        Options{Inputs: Inputs{Target: TargetCloud, CloudToken: "cap-token", Server: "https://mystack.grafana.net"}},
			grafana:     &stubGrafanaClient{version: v12},
			discovery:   okDiscovery,
			gcom:        &stubGCOMClient{},
			wantErr:     false,
			wantGCOMHit: true,
		},
		{
			name:        "Cloud + OAuth token: GCOM skipped",
			opts:        Options{Inputs: Inputs{Target: TargetCloud, CloudToken: "oauth-token", CloudCredentialKind: CloudCredentialOAuth, CloudTokenTrusted: true, Server: "https://mystack.grafana.net"}},
			grafana:     &stubGrafanaClient{version: v12},
			discovery:   okDiscovery,
			gcom:        &stubGCOMClient{},
			wantErr:     false,
			wantGCOMHit: false,
		},
		{
			name:        "Cloud without CAP token: GCOM skipped",
			opts:        Options{Inputs: Inputs{Target: TargetCloud, CloudToken: ""}},
			grafana:     &stubGrafanaClient{version: v12},
			discovery:   okDiscovery,
			gcom:        &stubGCOMClient{},
			wantErr:     false,
			wantGCOMHit: false,
		},
		{
			name:        "OnPrem: GCOM skipped entirely",
			opts:        Options{Inputs: Inputs{Target: TargetOnPrem}},
			grafana:     &stubGrafanaClient{version: v12},
			discovery:   okDiscovery,
			gcom:        &stubGCOMClient{},
			wantErr:     false,
			wantGCOMHit: false,
		},
		{
			name:        "Cloud + custom domain: GCOM skipped (no slug)",
			opts:        Options{Inputs: Inputs{Target: TargetCloud, CloudToken: "cap-token", Server: "https://grafana.example.com"}},
			grafana:     &stubGrafanaClient{version: v12},
			discovery:   okDiscovery,
			gcom:        &stubGCOMClient{},
			wantErr:     false,
			wantGCOMHit: false,
		},
		{
			name:      "all checks pass on-prem",
			opts:      Options{Inputs: Inputs{Target: TargetOnPrem}},
			grafana:   &stubGrafanaClient{version: v12},
			discovery: okDiscovery,
			wantErr:   false,
		},
		{
			name:      "empty version passes (Cloud anonymous health)",
			opts:      Options{Inputs: Inputs{Target: TargetOnPrem}},
			grafana:   &stubGrafanaClient{}, // nil version, empty raw, nil err
			discovery: okDiscovery,
			wantErr:   false,
		},
		{
			name:      "unparseable version passes (quirky dev build string)",
			opts:      Options{Inputs: Inputs{Target: TargetOnPrem}},
			grafana:   &stubGrafanaClient{raw: "main-abc1234"},
			discovery: okDiscovery,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gcomStub *stubGCOMClient
			if tt.gcom != nil {
				stub, ok := tt.gcom.(*stubGCOMClient)
				if !ok {
					t.Fatal("gcom is not *stubGCOMClient")
				}
				gcomStub = stub
			}

			v := &validator{
				grafana:   tt.grafana,
				discovery: tt.discovery,
				gcom:      tt.gcom,
			}

			_, err := v.validate(context.Background(), tt.opts, config.NamespacedRESTConfig{})

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrSub)
				}
				if tt.wantErrSub != "" && !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("expected error containing %q, got: %v", tt.wantErrSub, err)
				}
				if tt.wantErrAs != nil && !errors.As(err, tt.wantErrAs) {
					t.Fatalf("expected errors.As to %T to succeed; got %T: %v", tt.wantErrAs, err, err)
				}
			} else if err != nil {
				t.Fatalf("expected nil error, got: %v", err)
			}

			if gcomStub != nil && tt.wantGCOMHit != gcomStub.called {
				t.Fatalf("GCOM GetStack called=%v, want %v", gcomStub.called, tt.wantGCOMHit)
			}
		})
	}
}

func TestBasicAuthProbeErrors(t *testing.T) {
	for _, status := range []int{401, 403, 404, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/user", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"password-secret-echo"}`))
			}))
			defer server.Close()
			err := validateBasicAuth(t.Context(), config.Context{Grafana: &config.GrafanaConfig{
				Server: server.URL, AuthMethod: "basic", User: "admin", Password: "password-secret-echo",
			}})
			var probeErr *BasicAuthCheckError
			require.ErrorAs(t, err, &probeErr)
			assert.Equal(t, status, probeErr.Status)
			assert.NotContains(t, err.Error(), "password-secret-echo")
			assert.NoError(t, errors.Unwrap(probeErr))
		})
	}
}

func TestBasicAuthProbeRetainsCause(t *testing.T) {
	for _, test := range []struct {
		name        string
		closeServer bool
	}{
		{"transport failure", true},
		{"decode failure", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`<html>password-secret-echo</html>`))
			}))
			defer server.Close()
			if test.closeServer {
				server.Close()
			}
			err := validateBasicAuth(t.Context(), config.Context{Grafana: &config.GrafanaConfig{
				Server: server.URL, AuthMethod: "basic", User: "admin", Password: "password-secret-echo",
			}})
			var probeErr *BasicAuthCheckError
			require.ErrorAs(t, err, &probeErr)
			assert.Zero(t, probeErr.Status)
			require.Error(t, probeErr.Cause)
			require.ErrorIs(t, err, probeErr.Cause)
			assert.NotContains(t, err.Error(), "password-secret-echo")
			if test.closeServer {
				var transportErr *url.Error
				assert.ErrorAs(t, err, &transportErr)
			} else {
				assert.Contains(t, err.Error(), "invalid character")
			}
		})
	}
}
