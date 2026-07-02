package datasources

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&Provider{})
}

// Provider bridges Grafana datasources into the unified resources pipeline. It
// contributes no commands of its own — the human-facing `gcx datasources`
// command tree is mounted separately — but registers a resource adapter so
// datasources can be managed declaratively via `gcx resources`.
type Provider struct{}

// Name returns the unique identifier for this provider.
func (p *Provider) Name() string { return "datasources" }

// ShortDesc returns a one-line description of the provider.
func (p *Provider) ShortDesc() string { return "Manage Grafana datasources as resources" }

// Commands returns the Cobra commands contributed by this provider. The
// datasource resource type is exposed through `gcx resources`, so this provider
// adds no commands of its own.
func (p *Provider) Commands() []*cobra.Command { return nil }

// Validate is a no-op: datasources use Grafana's built-in authentication.
func (p *Provider) Validate(_ map[string]string) error { return nil }

// ConfigKeys returns nil: no provider-specific config keys are required.
func (p *Provider) ConfigKeys() []providers.ConfigKey { return nil }

// TypedRegistrations returns the adapter registration for the datasource type.
func (p *Provider) TypedRegistrations() []adapter.Registration {
	desc := StaticDescriptor()
	return []adapter.Registration{
		{
			Factory:     NewLazyFactory(),
			Descriptor:  desc,
			GVK:         desc.GroupVersionKind(),
			Schema:      DatasourceSchema(),
			Example:     DatasourceExample(),
			URLTemplate: "/connections/datasources/edit/{name}",
		},
	}
}
