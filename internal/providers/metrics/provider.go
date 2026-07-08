package metrics

import (
	dsprometheus "github.com/grafana/gcx/internal/datasources/prometheus"
	"github.com/grafana/gcx/internal/providers"
	adaptivemetrics "github.com/grafana/gcx/internal/providers/metrics/adaptive"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/signals"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&Provider{})
}

// Provider manages Prometheus datasource queries and Adaptive Metrics.
type Provider struct{}

func (p *Provider) descriptor() signals.Descriptor {
	return signals.Descriptor{
		Name:  "metrics",
		Short: p.ShortDesc(),
		Commands: []signals.CommandSpec{
			{
				Build:     dsprometheus.QueryCmd,
				TokenCost: "medium",
				LLMHint:   `gcx metrics query -d abc123 'up{job="grafana"}' -o json`,
				Example: `
  # Instant query using configured default datasource
  gcx metrics query 'up{job="grafana"}'

  # Instant query at a specific time
  gcx metrics query 'rate(http_requests_total[5m])' --time 2026-01-15T10:30:00Z

  # Range query with explicit datasource UID
  gcx metrics query -d abc123 'rate(http_requests_total[5m])' --from now-1h --to now --step 1m

  # Query the last hour
  gcx metrics query 'up' --since 1h

  # Print a Grafana Explore share link for the executed query
  gcx metrics query 'up' --share-link

  # Output as JSON
  gcx metrics query -d abc123 'up' -o json`,
			},
			{
				Build:     dsprometheus.LabelsCmd,
				TokenCost: "small",
				LLMHint:   "gcx metrics labels -d abc123 -o json",
				Example: `
  # List all labels (use datasource UID, not name)
  gcx metrics labels -d UID

  # Get values for a specific label
  gcx metrics labels -d UID --label job

  # Output as JSON
  gcx metrics labels -d UID -o json`,
			},
			{
				Build:     dsprometheus.MetadataCmd,
				TokenCost: "small",
				LLMHint:   "gcx metrics metadata -d abc123 -o json",
				Example: `
  # Get all metric metadata (use datasource UID, not name)
  gcx metrics metadata -d UID

  # Get metadata for a specific metric
  gcx metrics metadata -d UID --metric http_requests_total

  # Output as JSON
  gcx metrics metadata -d UID -o json`,
			},
			{
				Build:     seriesCmd,
				TokenCost: "medium",
				LLMHint:   `gcx metrics series -d abc123 '{__name__="up"}' --since 1h -o json`,
			},
		},
		ExtraCommands: []signals.CommandBuilder{BillingCommands, CardinalityCommands},
		Adaptive: &signals.AdaptiveSpec{
			Build: adaptivemetrics.Commands,
			Use:   "adaptive",
			Short: "Manage Adaptive Metrics resources",
		},
		ConfigKeys: []providers.ConfigKey{
			{Name: "metrics-tenant-id", Secret: false},
			{Name: "metrics-tenant-url", Secret: false},
		},
		Registrations: func(loader *providers.ConfigLoader) []adapter.Registration {
			return []adapter.Registration{
				{
					Factory:    adaptivemetrics.NewRuleAdapterFactory(loader),
					Descriptor: adaptivemetrics.RuleDescriptor(),
					GVK:        adaptivemetrics.RuleDescriptor().GroupVersionKind(),
					Schema:     adaptivemetrics.RuleSchema(),
					Example:    adaptivemetrics.RuleExample(),
				},
				{
					Factory:    adaptivemetrics.NewSegmentAdapterFactory(loader),
					Descriptor: adaptivemetrics.SegmentDescriptor(),
					GVK:        adaptivemetrics.SegmentDescriptor().GroupVersionKind(),
					Schema:     adaptivemetrics.SegmentSchema(),
					Example:    adaptivemetrics.SegmentExample(),
				},
				{
					Factory:    adaptivemetrics.NewExemptionAdapterFactory(loader),
					Descriptor: adaptivemetrics.ExemptionDescriptor(),
					GVK:        adaptivemetrics.ExemptionDescriptor().GroupVersionKind(),
					Schema:     adaptivemetrics.ExemptionSchema(),
					Example:    adaptivemetrics.ExemptionExample(),
				},
			}
		},
	}
}

func (p *Provider) Name() string { return "metrics" }

func (p *Provider) ShortDesc() string {
	return "Query Prometheus datasources and manage Adaptive Metrics"
}

func (p *Provider) Commands() []*cobra.Command {
	return []*cobra.Command{signals.Command(p.descriptor())}
}

// queryCmd is a thin wrapper used by expr_test.go.
func queryCmd(loader *providers.ConfigLoader) *cobra.Command { return dsprometheus.QueryCmd(loader) }

func (p *Provider) Validate(_ map[string]string) error { return nil }

func (p *Provider) ConfigKeys() []providers.ConfigKey {
	return p.descriptor().ConfigKeys
}

func (p *Provider) TypedRegistrations() []adapter.Registration {
	return p.descriptor().TypedRegistrations()
}
