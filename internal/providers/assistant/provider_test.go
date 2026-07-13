package assistant_test

import (
	"testing"

	"github.com/grafana/gcx/internal/assistant/mcpserver"
	"github.com/grafana/gcx/internal/providers"
	assistantprovider "github.com/grafana/gcx/internal/providers/assistant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderRegistration(t *testing.T) {
	p := &assistantprovider.AssistantProvider{}

	t.Run("Name", func(t *testing.T) {
		assert.Equal(t, "assistant", p.Name())
	})

	t.Run("ShortDesc", func(t *testing.T) {
		assert.NotEmpty(t, p.ShortDesc())
	})

	t.Run("Commands", func(t *testing.T) {
		cmds := p.Commands()
		require.Len(t, cmds, 1)

		root := cmds[0]
		assert.Equal(t, "assistant", root.Use)

		subNames := make([]string, 0, len(root.Commands()))
		for _, sub := range root.Commands() {
			subNames = append(subNames, sub.Name())
		}

		for _, expected := range []string{"prompt", "dashboard", "conversation", "investigations", "mcp-servers"} {
			assert.Contains(t, subNames, expected, "missing subcommand %q", expected)
		}
	})

	t.Run("ConfigKeys", func(t *testing.T) {
		keys := p.ConfigKeys()
		require.Len(t, keys, 1)
		assert.Equal(t, "api-mode", keys[0].Name)
		assert.False(t, keys[0].Secret, "providers.assistant.* capability-cache keys must not be redacted")
	})

	t.Run("Validate", func(t *testing.T) {
		assert.NoError(t, p.Validate(nil))
	})

	t.Run("TypedRegistrations", func(t *testing.T) {
		regs := p.TypedRegistrations()
		require.Len(t, regs, 1, "exactly one adapter registration — MCPServer")

		reg := regs[0]
		assert.Equal(t, mcpserver.MCPServerKind, reg.GVK.Kind)
		assert.Equal(t, mcpserver.MCPServerAPIGroup, reg.GVK.Group)
		assert.Equal(t, mcpserver.MCPServerVersion, reg.GVK.Version)
		assert.NotNil(t, reg.Schema)
		assert.NotNil(t, reg.Example)
		assert.NotNil(t, reg.Factory)
	})

	t.Run("IsRegistered", func(t *testing.T) {
		var found bool
		for _, rp := range providers.All() {
			if rp.Name() == "assistant" {
				found = true
				break
			}
		}
		assert.True(t, found, "assistant provider not found in providers.All()")
	})
}
