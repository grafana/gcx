package profiles_test

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/profiles"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time assertion: Provider satisfies StatusDetectable.
var _ framework.StatusDetectable = (*profiles.Provider)(nil)

func TestProviderRegistration(t *testing.T) {
	p := &profiles.Provider{}

	t.Run("Name", func(t *testing.T) {
		assert.Equal(t, "profiles", p.Name())
	})

	t.Run("ShortDesc", func(t *testing.T) {
		assert.NotEmpty(t, p.ShortDesc())
	})

	t.Run("Commands", func(t *testing.T) {
		cmds := p.Commands()
		require.Len(t, cmds, 1)

		root := cmds[0]
		assert.Equal(t, "profiles", root.Use)

		subNames := make([]string, 0, len(root.Commands()))
		for _, sub := range root.Commands() {
			subNames = append(subNames, sub.Name())
		}

		for _, expected := range []string{"query", "labels", "profile-types", "metrics", "adaptive"} {
			assert.Contains(t, subNames, expected, "missing subcommand %q", expected)
		}
	})

	t.Run("ConfigKeys", func(t *testing.T) {
		keys := p.ConfigKeys()
		require.Len(t, keys, 2)

		keyMap := make(map[string]providers.ConfigKey)
		for _, k := range keys {
			keyMap[k.Name] = k
		}

		tid, ok := keyMap["profiles-tenant-id"]
		require.True(t, ok, "missing config key profiles-tenant-id")
		assert.False(t, tid.Secret)

		turl, ok := keyMap["profiles-tenant-url"]
		require.True(t, ok, "missing config key profiles-tenant-url")
		assert.False(t, turl.Secret)
	})

	t.Run("Validate", func(t *testing.T) {
		assert.NoError(t, p.Validate(nil))
	})

	t.Run("IsRegistered", func(t *testing.T) {
		var found bool
		for _, rp := range providers.All() {
			if rp.Name() == "profiles" {
				found = true
				break
			}
		}
		assert.True(t, found, "profiles provider not found in providers.All()")
	})
}

func TestProvider_StatusDetectable(t *testing.T) {
	p := &profiles.Provider{}

	t.Run("NotSetupable", func(t *testing.T) {
		_, ok := any(p).(framework.Setupable)
		assert.False(t, ok, "profiles provider must not implement Setupable")
	})

	t.Run("ProductName", func(t *testing.T) {
		assert.Equal(t, p.Name(), p.ProductName())
	})

	t.Run("Status", func(t *testing.T) {
		status, err := p.Status(context.Background())
		require.NoError(t, err)
		require.NotNil(t, status)
		validStates := map[framework.ProductState]bool{
			framework.StateNotConfigured: true,
			framework.StateConfigured:    true,
			framework.StateActive:        true,
			framework.StateError:         true,
		}
		assert.True(t, validStates[status.State], "unexpected state: %q", status.State)
	})
}
