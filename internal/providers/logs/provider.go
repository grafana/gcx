package logs

import (
	dsloki "github.com/grafana/gcx/internal/datasources/loki"
	"github.com/grafana/gcx/internal/providers"
	adaptivelogs "github.com/grafana/gcx/internal/providers/logs/adaptive"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/signals"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&Provider{})
}

// Provider manages Loki datasource queries and Adaptive Logs.
type Provider struct{}

func (p *Provider) descriptor() signals.Descriptor {
	return signals.Descriptor{
		Name:  "logs",
		Short: p.ShortDesc(),
		Commands: []signals.CommandSpec{
			{
				Build:     dsloki.QueryCmd,
				TokenCost: "medium",
				LLMHint:   `gcx logs query -d abc123 '{job="grafana"}' -o json`,
				Example: `
  # Query logs using configured default datasource
  gcx logs query '{job="varlogs"}'

  # Query logs at a specific time
  gcx logs query '{job="varlogs"}' --time 2026-01-15T10:30:00Z

  # Query with explicit datasource UID
  gcx logs query -d abc123 '{job="varlogs"} |= "error"'

  # Print a Grafana Explore share link for the query
  gcx logs query '{job="varlogs"}' --share-link

  # Raw line bodies only
  gcx logs query -d abc123 '{job="varlogs"}' -o raw

  # Output as JSON
  gcx logs query -d abc123 '{job="varlogs"}' -o json`,
			},
			{
				Build:     dsloki.MetricsCmd,
				TokenCost: "medium",
				LLMHint:   `gcx logs metrics -d abc123 'rate({job="grafana"}[5m])' --since 1h -o json`,
				Example: `
  # Run a metric query over logs
  gcx logs metrics -d UID 'rate({job="grafana"}[5m])' --since 1h

  # Print a Grafana Explore share link for the query
  gcx logs metrics 'rate({job="grafana"}[5m])' --share-link

  # Output as JSON
  gcx logs metrics -d UID 'rate({job="grafana"}[5m])' --since 1h -o json`,
			},
			{
				Build:     dsloki.LabelsCmd,
				TokenCost: "small",
				LLMHint:   "gcx logs labels -d abc123 -o json",
				Example: `
  # List all labels (use datasource UID, not name)
  gcx logs labels -d UID

  # Get values for a specific label
  gcx logs labels -d UID --label job

  # Output as JSON
  gcx logs labels -d UID -o json`,
			},
			{
				Build:     dsloki.SeriesCmd,
				TokenCost: "small",
				LLMHint:   `gcx logs series -d abc123 --match '{job="varlogs"}' -o json`,
				Example: `
  # List series matching a selector (use datasource UID, not name)
  gcx logs series -d UID --match '{job="varlogs"}'

  # Multiple matchers (OR logic)
  gcx logs series -d UID --match '{job="varlogs"}' --match '{namespace="default"}'

  # Output as JSON
  gcx logs series -d UID --match '{job="varlogs"}' -o json`,
			},
		},
		Adaptive: &signals.AdaptiveSpec{
			Build: adaptivelogs.Commands,
			Use:   "adaptive",
			Short: "Manage Adaptive Logs resources",
		},
		ConfigKeys: []providers.ConfigKey{
			{Name: "logs-tenant-id", Secret: false},
			{Name: "logs-tenant-url", Secret: false},
		},
		Registrations: func(loader *providers.ConfigLoader) []adapter.Registration {
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
		},
	}
}

func (p *Provider) Name() string { return "logs" }

func (p *Provider) ShortDesc() string {
	return "Query Loki datasources and manage Adaptive Logs"
}

func (p *Provider) Commands() []*cobra.Command {
	return []*cobra.Command{signals.Command(p.descriptor())}
}

// queryCmd and metricsCmd are thin wrappers used by expr_test.go.
func queryCmd(loader *providers.ConfigLoader) *cobra.Command   { return dsloki.QueryCmd(loader) }
func metricsCmd(loader *providers.ConfigLoader) *cobra.Command { return dsloki.MetricsCmd(loader) }

func (p *Provider) Validate(_ map[string]string) error { return nil }

func (p *Provider) ConfigKeys() []providers.ConfigKey {
	return p.descriptor().ConfigKeys
}

func (p *Provider) TypedRegistrations() []adapter.Registration {
	return p.descriptor().TypedRegistrations()
}
