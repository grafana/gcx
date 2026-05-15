package vulnobs

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

// Note: package init() is only in provider.go (calls providers.Register).
// Resource adapter registrations are returned by TypedRegistrations().

var _ providers.Provider = &VulnobsProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&VulnobsProvider{})
}

// VulnobsProvider exposes Grafana Vulnerability Observability data through
// gcx. It is read-only by design (see ADR-003) — Source is registered as a
// typed adapter; Issue is a sub-resource accessed via
// `gcx vulnobs projects list-issues`.
type VulnobsProvider struct{}

// Name returns the unique identifier for this provider.
func (p *VulnobsProvider) Name() string { return "vulnobs" }

// ShortDesc returns a one-line description of the provider.
func (p *VulnobsProvider) ShortDesc() string {
	return "Inspect Grafana Vulnerability Observability data (read-only)"
}

// Commands returns the Cobra commands contributed by this provider.
func (p *VulnobsProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	cmd := newVulnobsCommand(loader)
	loader.BindFlags(cmd.PersistentFlags())
	return []*cobra.Command{cmd}
}

// Validate checks that the given provider configuration is valid.
// The provider has no config keys (ADR-001), so any config map is valid.
func (p *VulnobsProvider) Validate(_ map[string]string) error { return nil }

// ConfigKeys returns the configuration keys used by this provider.
// Auth is inherited from the active Grafana context; no provider-local keys.
func (p *VulnobsProvider) ConfigKeys() []providers.ConfigKey { return nil }

// TypedRegistrations returns adapter registrations for vulnobs resource
// types. `Source` is registered as a read-only typed resource per ADR-003;
// `Issue` is intentionally a sub-resource and not typed-registered (per
// CONSTITUTION line 130–135).
func (p *VulnobsProvider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    NewSourceAdapterFactory(loader),
			Descriptor: sourceDescriptor,
			GVK:        sourceDescriptor.GroupVersionKind(),
			Schema:     SourceSchema(),
		},
	}
}
