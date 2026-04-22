package framework_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/setup/framework"
)

func TestConfigKeysStatus(t *testing.T) {
	cases := []struct {
		name       string
		configKeys []providers.ConfigKey
		cfg        map[string]string
		wantState  framework.ProductState
	}{
		{
			name:       "no config keys → active",
			configKeys: nil,
			cfg:        map[string]string{},
			wantState:  framework.StateActive,
		},
		{
			name: "all non-secret keys present → configured",
			configKeys: []providers.ConfigKey{
				{Name: "tenant-id", Secret: false},
				{Name: "tenant-url", Secret: false},
			},
			cfg: map[string]string{
				"tenant-id":  "123",
				"tenant-url": "https://example.com",
			},
			wantState: framework.StateConfigured,
		},
		{
			name: "one non-secret key missing → not configured",
			configKeys: []providers.ConfigKey{
				{Name: "tenant-id", Secret: false},
				{Name: "tenant-url", Secret: false},
			},
			cfg: map[string]string{
				"tenant-id": "123",
			},
			wantState: framework.StateNotConfigured,
		},
		{
			name: "all non-secret keys missing → not configured",
			configKeys: []providers.ConfigKey{
				{Name: "tenant-id", Secret: false},
				{Name: "tenant-url", Secret: false},
			},
			cfg:       map[string]string{},
			wantState: framework.StateNotConfigured,
		},
		{
			name: "only secret keys, all absent → active (no non-secret required)",
			configKeys: []providers.ConfigKey{
				{Name: "api-token", Secret: true},
			},
			cfg:       map[string]string{},
			wantState: framework.StateActive,
		},
		{
			name: "mix of secret and non-secret; non-secret present → configured",
			configKeys: []providers.ConfigKey{
				{Name: "tenant-id", Secret: false},
				{Name: "api-token", Secret: true},
			},
			cfg: map[string]string{
				"tenant-id": "123",
			},
			wantState: framework.StateConfigured,
		},
		{
			name: "mix of secret and non-secret; non-secret absent → not configured",
			configKeys: []providers.ConfigKey{
				{Name: "tenant-id", Secret: false},
				{Name: "api-token", Secret: true},
			},
			cfg: map[string]string{
				"api-token": "tok",
			},
			wantState: framework.StateNotConfigured,
		},
		{
			name:       "nil cfg with required keys → not configured",
			configKeys: []providers.ConfigKey{{Name: "tenant-id", Secret: false}},
			cfg:        nil,
			wantState:  framework.StateNotConfigured,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &stubProvider{name: "test"}
			// We need a provider whose ConfigKeys() returns tc.configKeys.
			sp := &configKeyProvider{stubProvider: *p, keys: tc.configKeys}
			got := framework.ConfigKeysStatus(sp, tc.cfg)

			if got.State != tc.wantState {
				t.Errorf("State = %q, want %q (details: %q)", got.State, tc.wantState, got.Details)
			}
			if got.Product != "test" {
				t.Errorf("Product = %q, want %q", got.Product, "test")
			}
		})
	}
}

// configKeyProvider overrides ConfigKeys() on stubProvider.
type configKeyProvider struct {
	stubProvider

	keys []providers.ConfigKey
}

func (c *configKeyProvider) ConfigKeys() []providers.ConfigKey { return c.keys }
