package framework_test

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
)

// stubProvider is a minimal providers.Provider implementation for tests.
type stubProvider struct{ name string }

func (s *stubProvider) Name() string                               { return s.name }
func (s *stubProvider) ShortDesc() string                          { return "" }
func (s *stubProvider) Commands() []*cobra.Command                 { return nil }
func (s *stubProvider) Validate(_ map[string]string) error         { return nil }
func (s *stubProvider) ConfigKeys() []providers.ConfigKey          { return nil }
func (s *stubProvider) TypedRegistrations() []adapter.Registration { return nil }

// detectableProvider is a stubProvider that also implements StatusDetectable.
type detectableProvider struct {
	stubProvider

	state framework.ProductState
}

func (d *detectableProvider) ProductName() string { return d.name }
func (d *detectableProvider) Status(_ context.Context) (*framework.ProductStatus, error) {
	return &framework.ProductStatus{Product: d.name, State: d.state}, nil
}

// setupableProvider is a stubProvider that also implements Setupable.
type setupableProvider struct {
	stubProvider
}

func (s *setupableProvider) ProductName() string { return s.name }
func (s *setupableProvider) Status(_ context.Context) (*framework.ProductStatus, error) {
	return &framework.ProductStatus{Product: s.name, State: framework.StateActive}, nil
}
func (s *setupableProvider) InfraCategories() []framework.InfraCategory { return nil }
func (s *setupableProvider) ResolveChoices(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (s *setupableProvider) ValidateSetup(_ context.Context, _ map[string]string) error { return nil }
func (s *setupableProvider) Setup(_ context.Context, _ map[string]string) error         { return nil }

func TestDiscoverStatusDetectableFrom(t *testing.T) {
	cases := []struct {
		name      string
		providers []providers.Provider
		wantNames []string
	}{
		{
			name:      "empty slice returns empty",
			providers: []providers.Provider{},
			wantNames: nil,
		},
		{
			name: "non-detectable provider excluded",
			providers: []providers.Provider{
				&stubProvider{name: "plain"},
			},
			wantNames: nil,
		},
		{
			name: "detectable provider included",
			providers: []providers.Provider{
				&detectableProvider{stubProvider: stubProvider{name: "slo"}, state: framework.StateActive},
			},
			wantNames: []string{"slo"},
		},
		{
			name: "mixed: only detectable included",
			providers: []providers.Provider{
				&stubProvider{name: "plain"},
				&detectableProvider{stubProvider: stubProvider{name: "slo"}, state: framework.StateActive},
				&detectableProvider{stubProvider: stubProvider{name: "metrics"}, state: framework.StateConfigured},
			},
			wantNames: []string{"slo", "metrics"},
		},
		{
			name: "setupable satisfies detectable",
			providers: []providers.Provider{
				&setupableProvider{stubProvider: stubProvider{name: "synth"}},
			},
			wantNames: []string{"synth"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := framework.DiscoverStatusDetectableFrom(tc.providers)
			if len(got) != len(tc.wantNames) {
				t.Fatalf("got %d results, want %d", len(got), len(tc.wantNames))
			}
			for i, sd := range got {
				if sd.ProductName() != tc.wantNames[i] {
					t.Errorf("result[%d].ProductName() = %q, want %q", i, sd.ProductName(), tc.wantNames[i])
				}
			}
		})
	}
}

func TestDiscoverSetupableFrom(t *testing.T) {
	cases := []struct {
		name      string
		providers []providers.Provider
		wantNames []string
	}{
		{
			name:      "empty slice returns empty",
			providers: []providers.Provider{},
			wantNames: nil,
		},
		{
			name: "detectable-only excluded",
			providers: []providers.Provider{
				&detectableProvider{stubProvider: stubProvider{name: "slo"}},
			},
			wantNames: nil,
		},
		{
			name: "setupable included",
			providers: []providers.Provider{
				&setupableProvider{stubProvider: stubProvider{name: "synth"}},
			},
			wantNames: []string{"synth"},
		},
		{
			name: "mixed: only setupable included",
			providers: []providers.Provider{
				&stubProvider{name: "plain"},
				&detectableProvider{stubProvider: stubProvider{name: "slo"}},
				&setupableProvider{stubProvider: stubProvider{name: "synth"}},
				&setupableProvider{stubProvider: stubProvider{name: "k6"}},
			},
			wantNames: []string{"synth", "k6"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := framework.DiscoverSetupableFrom(tc.providers)
			if len(got) != len(tc.wantNames) {
				t.Fatalf("got %d results, want %d", len(got), len(tc.wantNames))
			}
			for i, s := range got {
				if s.ProductName() != tc.wantNames[i] {
					t.Errorf("result[%d].ProductName() = %q, want %q", i, s.ProductName(), tc.wantNames[i])
				}
			}
		})
	}
}
