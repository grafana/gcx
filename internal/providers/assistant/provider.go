// Package assistant registers Grafana Assistant as a first-class gcx
// provider, mounting the existing command tree through the provider
// registry loop instead of a hand-mount (CONSTITUTION §28-32).
package assistant

import (
	"github.com/grafana/gcx/internal/assistant/mcpserver"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &AssistantProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&AssistantProvider{})
}

// AssistantProvider manages Grafana Assistant resources and commands.
type AssistantProvider struct{}

// Name returns the unique identifier for this provider.
func (p *AssistantProvider) Name() string { return "assistant" }

// ShortDesc returns a one-line description of the provider.
func (p *AssistantProvider) ShortDesc() string { return "Interact with Grafana Assistant" }

// Commands returns the Cobra commands contributed by this provider. This is
// a verbatim lift-and-shift of the existing assistant command tree (prompt,
// dashboard, conversation, investigations, mcp-servers), preserving the
// requireGrafanaCloud guard and per-subcommand config wiring.
func (p *AssistantProvider) Commands() []*cobra.Command {
	return []*cobra.Command{Command()}
}

// Validate checks that the given provider configuration is valid.
func (p *AssistantProvider) Validate(_ map[string]string) error { return nil }

// ConfigKeys returns the configuration keys used by this provider. The
// api-mode key caches the detected Assistant investigations API surface
// (v1/v2); it is not a secret, so config view must not redact it.
func (p *AssistantProvider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "api-mode", Secret: false},
	}
}

// TypedRegistrations returns adapter registrations for assistant resource
// types. MCPServer is the only registered type — investigations,
// conversation, and the A2A prompt/dashboard path stay command-only.
func (p *AssistantProvider) TypedRegistrations() []adapter.Registration {
	desc := mcpserver.MCPServerDescriptor()
	return []adapter.Registration{
		{
			Factory:    mcpserver.NewLazyFactory(),
			Descriptor: desc,
			GVK:        desc.GroupVersionKind(),
			Schema:     mcpserver.MCPServerSchema(),
			Example:    mcpserver.MCPServerExample(),
		},
	}
}
