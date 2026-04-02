package traces

import (
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	adaptivetraces "github.com/grafana/gcx/internal/providers/adaptive/traces"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&Provider{})
}

// Provider manages Tempo datasource queries and Adaptive Traces.
type Provider struct{}

func (p *Provider) Name() string { return "traces" }

func (p *Provider) ShortDesc() string {
	return "Query Tempo datasources and manage Adaptive Traces"
}

func (p *Provider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	cmd := &cobra.Command{
		Use:   "traces",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	loader.BindFlags(cmd.PersistentFlags())

	// Datasource-origin subcommand.
	qCmd := queryCmd()
	qCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   "gcx traces query",
	}
	cmd.AddCommand(qCmd)

	// Adaptive Traces subcommands — rename Use from "traces" to "adaptive".
	adaptiveCmd := adaptivetraces.Commands(loader)
	adaptiveCmd.Use = "adaptive"
	adaptiveCmd.Short = "Manage Adaptive Traces resources"
	cmd.AddCommand(adaptiveCmd)

	return []*cobra.Command{cmd}
}

func (p *Provider) Validate(_ map[string]string) error { return nil }

func (p *Provider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "traces-tenant-id", Secret: false},
		{Name: "traces-tenant-url", Secret: false},
	}
}

func (p *Provider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    adaptivetraces.NewPolicyAdapterFactory(loader),
			Descriptor: adaptivetraces.PolicyDescriptor(),
			GVK:        adaptivetraces.PolicyDescriptor().GroupVersionKind(),
			Schema:     adaptivetraces.PolicySchema(),
			Example:    adaptivetraces.PolicyExample(),
		},
	}
}
