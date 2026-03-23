package k6

import (
	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &K6Provider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&K6Provider{})
}

// K6Provider manages K6 Cloud resources (projects, load tests, environment variables).
type K6Provider struct{}

// Name returns the unique identifier for this provider.
func (p *K6Provider) Name() string { return "k6" }

// ShortDesc returns a one-line description of the provider.
func (p *K6Provider) ShortDesc() string {
	return "Manage K6 Cloud resources (projects, tests, env vars)."
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
		newTokenCommand(loader),
	)

	return []*cobra.Command{k6Cmd}
}

// Validate checks that the given provider configuration is valid.
func (p *K6Provider) Validate(cfg map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
func (p *K6Provider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "api-domain"},
	}
}

// ResourceAdapters returns adapter factories for K6 resource types.
// Factories are registered globally via adapter.Register() in resource_adapter.go init().
func (p *K6Provider) ResourceAdapters() []adapter.Factory {
	return nil
}
