package logs

import (
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	adaptivelogs "github.com/grafana/gcx/internal/providers/logs/adaptive"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&Provider{})
}

// Provider manages Loki datasource queries and Adaptive Logs.
type Provider struct{}

func (p *Provider) Name() string { return "logs" }

func (p *Provider) ShortDesc() string {
	return "Query Loki datasources and manage Adaptive Logs"
}

func (p *Provider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	cmd := &cobra.Command{
		Use:   "logs",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	loader.BindFlags(cmd.PersistentFlags())

	// Datasource-origin subcommands.
	qCmd := queryCmd(loader)
	qCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx logs query -d abc123 '{job="grafana"}' -o json`,
	}
	qCmd.Example = `
  # Query logs using configured default datasource
  gcx logs query '{job="varlogs"}'

  # Query with explicit datasource UID
  gcx logs query -d abc123 '{job="varlogs"} |= "error"'

  # Output as JSON
  gcx logs query -d abc123 '{job="varlogs"}' -o json`
	cmd.AddCommand(qCmd)

	mqCmd := metricsCmd(loader)
	mqCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx logs metrics -d abc123 'rate({job="grafana"}[5m])' --since 1h -o json`,
	}
	cmd.AddCommand(mqCmd)

	lCmd := labelsCmd(loader)
	lCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx logs labels -d abc123 -o json",
	}
	lCmd.Example = `
  # List all labels (use datasource UID, not name)
  gcx logs labels -d <datasource-uid>

  # Get values for a specific label
  gcx logs labels -d <datasource-uid> --label job

  # Output as JSON
  gcx logs labels -d <datasource-uid> -o json`
	cmd.AddCommand(lCmd)

	sCmd := seriesCmd(loader)
	sCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx logs series -d abc123 --match '{job="varlogs"}' -o json`,
	}
	sCmd.Example = `
  # List series matching a selector (use datasource UID, not name)
  gcx logs series -d <datasource-uid> --match '{job="varlogs"}'

  # Multiple matchers (OR logic)
  gcx logs series -d <datasource-uid> --match '{job="varlogs"}' --match '{namespace="default"}'

  # Output as JSON
  gcx logs series -d <datasource-uid> --match '{job="varlogs"}' -o json`
	cmd.AddCommand(sCmd)

	// Adaptive Logs subcommands — rename Use from "logs" to "adaptive".
	adaptiveCmd := adaptivelogs.Commands(loader)
	adaptiveCmd.Use = "adaptive"
	adaptiveCmd.Short = "Manage Adaptive Logs resources"
	cmd.AddCommand(adaptiveCmd)

	return []*cobra.Command{cmd}
}

func (p *Provider) Validate(_ map[string]string) error { return nil }

func (p *Provider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "logs-tenant-id", Secret: false},
		{Name: "logs-tenant-url", Secret: false},
	}
}

func (p *Provider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    adaptivelogs.NewExemptionAdapterFactory(loader),
			Descriptor: adaptivelogs.ExemptionDescriptor(),
			GVK:        adaptivelogs.ExemptionDescriptor().GroupVersionKind(),
			Schema:     adaptivelogs.ExemptionSchema(),
			Example:    adaptivelogs.ExemptionExample(),
		},
		{
			Factory:    adaptivelogs.NewSegmentAdapterFactory(loader),
			Descriptor: adaptivelogs.SegmentDescriptor(),
			GVK:        adaptivelogs.SegmentDescriptor().GroupVersionKind(),
			Schema:     adaptivelogs.SegmentSchema(),
			Example:    adaptivelogs.SegmentExample(),
		},
		{
			Factory:    adaptivelogs.NewDropRuleAdapterFactory(loader),
			Descriptor: adaptivelogs.DropRuleDescriptor(),
			GVK:        adaptivelogs.DropRuleDescriptor().GroupVersionKind(),
			Schema:     adaptivelogs.DropRuleSchema(),
			Example:    adaptivelogs.DropRuleExample(),
		},
	}
}
