package traces

import (
	dstempo "github.com/grafana/gcx/internal/datasources/tempo"
	"github.com/grafana/gcx/internal/providers"
	adaptivetraces "github.com/grafana/gcx/internal/providers/traces/adaptive"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/signals"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&Provider{})
}

// Provider manages Tempo datasource queries and Adaptive Traces.
type Provider struct{}

func (p *Provider) descriptor() signals.Descriptor {
	return signals.Descriptor{
		Name:  "traces",
		Short: p.ShortDesc(),
		Commands: []signals.CommandSpec{
			{
				Build:     dstempo.QueryCmd,
				TokenCost: "medium",
				LLMHint:   `gcx traces query -d abc123 '{ span.http.status_code >= 500 }' -o json`,
				Example: `
  # Run a TraceQL query
  gcx traces query -d UID '{ span.http.status_code >= 500 }'

  # Print a Grafana Explore share link for the query
  gcx traces query '{ span.http.status_code >= 500 }' --share-link

  # Output as JSON
  gcx traces query -d UID '{ span.http.status_code >= 500 }' -o json`,
			},
			{
				Build:     dstempo.GetCmd,
				TokenCost: "medium",
				LLMHint:   "gcx traces get -d abc123 <trace-id> --llm -o json; for a large trace, narrow with --q '{ status = error }' --keep-hierarchy or shrink fan-outs with --span-pruning",
				Example: `
  # Fetch a trace by ID for agent analysis
  gcx traces get -d UID <trace-id> --llm -o json

  # Print a Grafana Explore share link for the trace
  gcx traces get -d UID <trace-id> --share-link

  # Output raw OTLP-shaped JSON when explicitly needed
  gcx traces get -d UID <trace-id> -o json

  # Narrow a large trace to error spans and their ancestor path
  gcx traces get -d UID <trace-id> --q '{ status = error }' --keep-hierarchy

  # Collapse repeated sibling spans to shrink a huge trace before analysis
  gcx traces get -d UID <trace-id> --span-pruning --llm -o json`,
			},
			{
				Build:     dstempo.LabelsCmd,
				TokenCost: "small",
				LLMHint:   "gcx traces labels -d abc123 -o json",
				Example: `
  # List all labels
  gcx traces labels -d UID

  # Get LLM-friendly values for a label
  gcx traces tags -d UID -l resource.service.name --llm -o json

  # Output as JSON
  gcx traces labels -d UID -o json`,
			},
			{
				Build:     dstempo.MetricsCmd,
				TokenCost: "medium",
				LLMHint:   `gcx traces metrics -d abc123 '{ } | rate()' --since 1h -o json`,
				Example: `
  # Run a TraceQL metrics query
  gcx traces metrics -d UID '{ } | rate()' --since 1h

  # Print a Grafana Explore share link for the query
  gcx traces metrics '{ } | rate()' --share-link

  # Output as JSON
  gcx traces metrics -d UID '{ } | rate()' --since 1h -o json`,
			},
			{
				Build:     dstempo.DiffCmd,
				TokenCost: "medium",
				LLMHint:   "gcx traces diff -d abc123 <trace-a> <trace-b> -o json",
				Example: `
  # Compare two traces (B - A semantics); experimental, Grafana Cloud-only
  gcx traces diff <trace-a> <trace-b>

  # With an explicit datasource UID, JSON output
  gcx traces diff -d UID <trace-a> <trace-b> -o json`,
			},
			{
				Build:     dstempo.BaselineCmd,
				TokenCost: "medium",
				LLMHint:   "gcx traces baseline -d abc123 <trace-id> -o json",
				Example: `
  # Start unfiltered, then diff a candidate as the baseline (B - A semantics)
  gcx traces baseline <trace-id>
  gcx traces diff <candidate> <trace-id>

  # Only if unfiltered candidates are not valid comparisons, refine by tenant
  gcx traces baseline <trace-id> --filter '{ span.tenantID = "tenant-a" }'

  # Widen the window to 6h before and after the seed, output JSON
  gcx traces baseline <trace-id> --window 6h -o json`,
			},
		},
		Adaptive: &signals.AdaptiveSpec{
			Build: adaptivetraces.Commands,
			Use:   "adaptive",
			Short: "Manage Adaptive Traces resources",
		},
		ConfigKeys: []providers.ConfigKey{
			{Name: "traces-tenant-id", Secret: false},
			{Name: "traces-tenant-url", Secret: false},
		},
		Registrations: func(loader *providers.ConfigLoader) []adapter.Registration {
			return []adapter.Registration{
				{
					Factory:    adaptivetraces.NewPolicyAdapterFactory(loader),
					Descriptor: adaptivetraces.PolicyDescriptor(),
					GVK:        adaptivetraces.PolicyDescriptor().GroupVersionKind(),
					Schema:     adaptivetraces.PolicySchema(),
					Example:    adaptivetraces.PolicyExample(),
				},
			}
		},
	}
}

func (p *Provider) Name() string { return "traces" }

func (p *Provider) ShortDesc() string {
	return "Query Tempo datasources and manage Adaptive Traces"
}

func (p *Provider) Commands() []*cobra.Command {
	return []*cobra.Command{signals.Command(p.descriptor())}
}

// queryCmd and metricsCmd are thin wrappers used by expr_test.go.
func queryCmd(loader *providers.ConfigLoader) *cobra.Command   { return dstempo.QueryCmd(loader) }
func metricsCmd(loader *providers.ConfigLoader) *cobra.Command { return dstempo.MetricsCmd(loader) }

func (p *Provider) Validate(_ map[string]string) error { return nil }

func (p *Provider) ConfigKeys() []providers.ConfigKey {
	return p.descriptor().ConfigKeys
}

func (p *Provider) TypedRegistrations() []adapter.Registration {
	return p.descriptor().TypedRegistrations()
}
