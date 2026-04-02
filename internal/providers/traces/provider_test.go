package traces_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/traces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderRegistration(t *testing.T) {
	p := &traces.Provider{}

	t.Run("Name", func(t *testing.T) {
		assert.Equal(t, "traces", p.Name())
	})

	t.Run("ShortDesc", func(t *testing.T) {
		assert.NotEmpty(t, p.ShortDesc())
	})

	t.Run("Commands", func(t *testing.T) {
		cmds := p.Commands()
		require.Len(t, cmds, 1)

		root := cmds[0]
		assert.Equal(t, "traces", root.Use)

		subNames := make([]string, 0, len(root.Commands()))
		for _, sub := range root.Commands() {
			subNames = append(subNames, sub.Name())
		}

		for _, expected := range []string{"query", "adaptive"} {
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

		tid, ok := keyMap["traces-tenant-id"]
		require.True(t, ok, "missing config key traces-tenant-id")
		assert.False(t, tid.Secret)

		turl, ok := keyMap["traces-tenant-url"]
		require.True(t, ok, "missing config key traces-tenant-url")
		assert.False(t, turl.Secret)
	})

	t.Run("Validate", func(t *testing.T) {
		assert.NoError(t, p.Validate(nil))
	})

	t.Run("IsRegistered", func(t *testing.T) {
		var found bool
		for _, rp := range providers.All() {
			if rp.Name() == "traces" {
				found = true
				break
			}
		}
		assert.True(t, found, "traces provider not found in providers.All()")
	})
}
