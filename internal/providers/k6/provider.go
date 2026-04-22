package k6

import (
	"context"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ providers.Provider = &K6Provider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&K6Provider{})
}

// K6Provider manages k6 Cloud resources (projects, load tests, environment variables).
type K6Provider struct{}

// Name returns the unique identifier for this provider.
func (p *K6Provider) Name() string { return "k6" }

// ShortDesc returns a one-line description of the provider.
func (p *K6Provider) ShortDesc() string {
	return "Manage Grafana k6 Cloud projects, load tests, and schedules"
}

// Commands returns the Cobra commands contributed by this provider.
func (p *K6Provider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	k6Cmd := &cobra.Command{
		Use:   "k6",
		Short: p.ShortDesc(),
	}

	loader.BindFlags(k6Cmd.PersistentFlags())

	k6Cmd.AddCommand(
		newProjectsCommand(loader),
		newTestsCommand(loader),
		newEnvVarsCommand(loader),
		newRunsCommand(loader),
		newSchedulesCommand(loader),
		newLoadZonesCommand(loader),
		newTestrunCommand(loader),
		newAuthCommand(loader),
		newSetupCommand(p),
	)

	return []*cobra.Command{k6Cmd}
}

// ProductName implements framework.StatusDetectable.
func (p *K6Provider) ProductName() string { return p.Name() }

// Status implements framework.StatusDetectable using a config-key heuristic.
func (p *K6Provider) Status(ctx context.Context) (*framework.ProductStatus, error) {
	var loader providers.ConfigLoader
	// TODO: add proper error handling once provider setup is implemented
	cfg, _, _ := loader.LoadProviderConfig(ctx, p.Name())
	status := framework.ConfigKeysStatus(p, cfg)
	return &status, nil
}

// InfraCategories implements framework.Setupable.
func (p *K6Provider) InfraCategories() []framework.InfraCategory { return nil }

// ResolveChoices implements framework.Setupable.
func (p *K6Provider) ResolveChoices(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// ValidateSetup implements framework.Setupable.
func (p *K6Provider) ValidateSetup(_ context.Context, _ map[string]string) error { return nil }

// Setup implements framework.Setupable.
func (p *K6Provider) Setup(_ context.Context, _ map[string]string) error {
	return framework.ErrSetupNotSupported
}

// Validate checks that the given provider configuration is valid.
func (p *K6Provider) Validate(_ map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
func (p *K6Provider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "api-domain"},
	}
}

// TypedRegistrations returns adapter registrations for k6 resource types.
func (p *K6Provider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	registrations := make([]adapter.Registration, 0, len(allResources()))

	for _, rd := range allResources() {
		desc := resources.Descriptor{
			GroupVersion: schema.GroupVersion{
				Group:   APIGroup,
				Version: APIVersionStr,
			},
			Kind:     rd.kind,
			Singular: rd.singular,
			Plural:   rd.plural,
		}
		registrations = append(registrations, adapter.Registration{
			Factory:     newSubResourceFactory(loader, rd),
			Descriptor:  desc,
			GVK:         desc.GroupVersionKind(),
			Schema:      rd.schema,
			Example:     rd.example,
			URLTemplate: rd.urlTemplate,
		})
	}

	return registrations
}
