package incidents

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &IncidentsProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&IncidentsProvider{})
}

// IncidentsProvider manages Grafana IRM Incident resources.
type IncidentsProvider struct{}

// Name returns the unique identifier for this provider.
func (p *IncidentsProvider) Name() string { return "incidents" }

// ShortDesc returns a one-line description of the provider.
func (p *IncidentsProvider) ShortDesc() string {
	return "Manage Grafana Incident Response and Management (IRM) incidents"
}

// Commands returns the Cobra commands contributed by this provider.
func (p *IncidentsProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	incCmd := &cobra.Command{
		Use:     "incidents",
		Short:   p.ShortDesc(),
		Aliases: []string{"incident", "inc"},
	}

	loader.BindFlags(incCmd.PersistentFlags())

	incCmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newCloseCommand(loader),
		newActivityCommand(loader),
		newSeveritiesCommand(loader),
		newOpenCommand(loader),
	)

	return []*cobra.Command{incCmd}
}

// Validate checks that the given provider configuration is valid.
// The incidents provider uses Grafana's built-in authentication and does not
// require additional provider-specific keys.
func (p *IncidentsProvider) Validate(cfg map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
// The incidents provider uses Grafana's built-in authentication and does not
// require additional provider-specific keys.
func (p *IncidentsProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// TypedRegistrations returns adapter registrations for Incident resource types.
// Registrations are added globally by providers.Register() which calls this method.
func (p *IncidentsProvider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    NewAdapterFactory(loader),
			Descriptor: staticDescriptor,
			GVK:        staticDescriptor.GroupVersionKind(),
			Schema:     incidentSchema(),
			Example:    incidentExample(),
		},
	}
}
