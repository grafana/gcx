package experiments

import (
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&Provider{})
}

// Provider contributes the experimental Odin command group.
type Provider struct{}

var _ providers.Provider = &Provider{}

func (*Provider) Name() string                               { return "experiments" }
func (*Provider) ShortDesc() string                          { return "Work with Odin experiments." }
func (*Provider) Validate(map[string]string) error           { return nil }
func (*Provider) ConfigKeys() []providers.ConfigKey          { return nil }
func (*Provider) TypedRegistrations() []adapter.Registration { return nil }

func (*Provider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}
	cmd := &cobra.Command{
		Use:   "experiments",
		Short: "[experimental] Work with Odin experiments.",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

List, get, and create experiments through the Odin app plugin on the selected Grafana instance.`,
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
	}
	loader.BindFlags(cmd.PersistentFlags())
	cmd.AddCommand(newListCommand(loader))
	cmd.AddCommand(newGetCommand(loader))
	cmd.AddCommand(newCreateCommand(loader))
	return []*cobra.Command{cmd}
}
