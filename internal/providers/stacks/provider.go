package stacks

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &StacksProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&StacksProvider{})
}

// StacksProvider manages Grafana Cloud stack lifecycle via the GCOM API.
type StacksProvider struct{}

func (p *StacksProvider) Name() string { return "stacks" }

func (p *StacksProvider) ShortDesc() string {
	return "Manage Grafana Cloud stacks (list, create, update, delete)"
}

func (p *StacksProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	stacksCmd := &cobra.Command{
		Use:   "stacks",
		Short: p.ShortDesc(),
	}

	loader.BindFlags(stacksCmd.PersistentFlags())

	stacksCmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newDeleteCommand(loader),
		newRegionsCommand(loader),
	)

	return []*cobra.Command{stacksCmd}
}

func (p *StacksProvider) Validate(_ map[string]string) error { return nil }

func (p *StacksProvider) ConfigKeys() []providers.ConfigKey { return nil }

func (p *StacksProvider) TypedRegistrations() []adapter.Registration { return nil }
