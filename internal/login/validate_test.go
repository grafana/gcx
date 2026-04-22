package login //nolint:testpackage // White-box test: needs access to unexported validator struct for stub injection.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
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
		wantGCOMHit bool
	}{
		{
			name:       "health check failure",
			opts:       Options{Inputs: Inputs{Target: TargetOnPrem}},
			grafana:    &stubGrafanaClient{err: errors.New("connection refused")},
			discovery:  okDiscovery,
			wantErr:    true,
			wantErrSub: "health check failed",
		},
		{
			name:       "K8s discovery failure",
			opts:       Options{Inputs: Inputs{Target: TargetOnPrem}},
			grafana:    &stubGrafanaClient{version: v12},
			discovery:  failDiscovery,
			wantErr:    true,
			wantErrSub: "kubernetes API unavailable",
		},
		{
			name:       "version below 12 returns named error",
			opts:       Options{Inputs: Inputs{Target: TargetOnPrem}},
			grafana:    &stubGrafanaClient{version: v11},
			discovery:  okDiscovery,
			wantErr:    true,
			wantErrSub: "version check failed",
		},
		{
			name:        "GCOM check failure",
			opts:        Options{Inputs: Inputs{Target: TargetCloud, CloudToken: "cap-token", Server: "https://mystack.grafana.net"}},
			grafana:     &stubGrafanaClient{version: v12},
			discovery:   okDiscovery,
			gcom:        &stubGCOMClient{err: errors.New("unauthorized")},
			wantErr:     true,
			wantErrSub:  "GCOM check failed",
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
			} else if err != nil {
				t.Fatalf("expected nil error, got: %v", err)
			}

			if gcomStub != nil && tt.wantGCOMHit != gcomStub.called {
				t.Fatalf("GCOM GetStack called=%v, want %v", gcomStub.called, tt.wantGCOMHit)
			}
		})
	}
}
