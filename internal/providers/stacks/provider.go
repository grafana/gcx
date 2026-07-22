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

// Commands returns nil because the stacks command tree is wired via
// cmd/gcx/cloud (as "gcx cloud stacks"). The provider is still registered
// so it appears in "gcx providers list".
func (p *StacksProvider) Commands() []*cobra.Command { return nil }

// NewCommand returns the "stacks" cobra command with all subcommands registered.
// Called by cmd/gcx/cloud to mount stacks under the cloud command group.
func NewCommand() *cobra.Command {
	loader := &providers.ConfigLoader{}

	stacksCmd := &cobra.Command{
		Use:   "stacks",
		Short: "Manage Grafana Cloud stacks (list, create, update, delete)",
	}

	loader.BindFlags(stacksCmd.PersistentFlags())

	stacksCmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newDeleteCommand(loader),
		newListRegionsCommand(loader),
	)

	return stacksCmd
}

func (p *StacksProvider) Validate(_ map[string]string) error { return nil }

func (p *StacksProvider) ConfigKeys() []providers.ConfigKey { return nil }

func (p *StacksProvider) TypedRegistrations() []adapter.Registration { return nil }
