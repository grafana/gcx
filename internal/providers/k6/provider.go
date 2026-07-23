package k6

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
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
	)

	return []*cobra.Command{k6Cmd}
}

// Validate checks that the given provider configuration is valid.
func (p *K6Provider) Validate(cfg map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
// The cached-* keys are populated lazily in DirectClient mode (SA-token auth)
// to skip the /v3/account/grafana-app/start round-trip on subsequent invocations.
// They are not used in OAuth/plugin-proxy mode.
func (p *K6Provider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "api-domain"},
		// Cached auth from /v3/account/grafana-app/start (DirectClient / SA-token mode only).
		// Populated lazily on first run, reused on subsequent invocations to skip the
		// round-trip. Invalidated on 401 from any k6 API call. Bound to a specific
		// stack and API domain; cache is dropped if either changes.
		// Not used in OAuth/plugin-proxy mode.
		{Name: keyCachedToken, Secret: true},
		{Name: keyCachedOrgID},
		{Name: keyCachedStackID},
		{Name: keyCachedDomain},
		{Name: keyCachedBinding, Secret: true},
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
