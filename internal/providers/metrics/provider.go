package metrics

import (
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	adaptivemetrics "github.com/grafana/gcx/internal/providers/metrics/adaptive"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&Provider{})
}

// Provider manages Prometheus datasource queries and Adaptive Metrics.
type Provider struct{}

func (p *Provider) Name() string { return "metrics" }

func (p *Provider) ShortDesc() string {
	return "Query Prometheus datasources and manage Adaptive Metrics"
}

func (p *Provider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	cmd := &cobra.Command{
		Use:   "metrics",
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
		agent.AnnotationLLMHint:   `gcx metrics query -d abc123 'up{job="grafana"}' -o json`,
	}
	qCmd.Example = `
  # Instant query using configured default datasource
  gcx metrics query 'up{job="grafana"}'

  # Range query with explicit datasource UID
  gcx metrics query -d abc123 'rate(http_requests_total[5m])' --from now-1h --to now --step 1m

  # Query the last hour
  gcx metrics query 'up' --since 1h

  # Output as JSON
  gcx metrics query -d abc123 'up' -o json`
	cmd.AddCommand(qCmd)

	lCmd := labelsCmd(loader)
	lCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx metrics labels -d abc123 -o json",
	}
	lCmd.Example = `
  # List all labels (use datasource UID, not name)
  gcx metrics labels -d <datasource-uid>

  # Get values for a specific label
  gcx metrics labels -d <datasource-uid> --label job

  # Output as JSON
  gcx metrics labels -d <datasource-uid> -o json`
	cmd.AddCommand(lCmd)

	mCmd := metadataCmd(loader)
	mCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx metrics metadata -d abc123 -o json",
	}
	mCmd.Example = `
  # Get all metric metadata (use datasource UID, not name)
  gcx metrics metadata -d <datasource-uid>

  # Get metadata for a specific metric
  gcx metrics metadata -d <datasource-uid> --metric http_requests_total

  # Output as JSON
  gcx metrics metadata -d <datasource-uid> -o json`
	cmd.AddCommand(mCmd)

	// Adaptive Metrics subcommands — rename Use from "metrics" to "adaptive".
	adaptiveCmd := adaptivemetrics.Commands(loader)
	adaptiveCmd.Use = "adaptive"
	adaptiveCmd.Short = "Manage Adaptive Metrics resources"
	cmd.AddCommand(adaptiveCmd)

	return []*cobra.Command{cmd}
}

func (p *Provider) Validate(_ map[string]string) error { return nil }

func (p *Provider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "metrics-tenant-id", Secret: false},
		{Name: "metrics-tenant-url", Secret: false},
	}
}

func (p *Provider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    adaptivemetrics.NewRuleAdapterFactory(loader),
			Descriptor: adaptivemetrics.RuleDescriptor(),
			GVK:        adaptivemetrics.RuleDescriptor().GroupVersionKind(),
			Schema:     adaptivemetrics.RuleSchema(),
			Example:    adaptivemetrics.RuleExample(),
		},
	}
}
