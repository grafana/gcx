package traces

import (
	"github.com/grafana/gcx/internal/agent"
	dstempo "github.com/grafana/gcx/internal/datasources/tempo"
	"github.com/grafana/gcx/internal/providers"
	adaptivetraces "github.com/grafana/gcx/internal/providers/traces/adaptive"
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

	// Grab the commands from the datasources package, and override the examples
	// and annotations to be suitable for the top-level commands.
	qCmd := dstempo.QueryCmd(loader)
	qCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx traces query -d abc123 '{ span.http.status_code >= 500 }' -o json`,
	}
	qCmd.Example = `
  # Run a TraceQL query
  gcx traces query -d UID '{ span.http.status_code >= 500 }'

  # Print a Grafana Explore share link for the query
  gcx traces query '{ span.http.status_code >= 500 }' --share-link

  # Output as JSON
  gcx traces query -d UID '{ span.http.status_code >= 500 }' -o json`
	cmd.AddCommand(qCmd)

	gCmd := dstempo.GetCmd(loader)
	gCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   "gcx traces get -d abc123 <trace-id> -o json",
	}
	gCmd.Example = `
  # Fetch a trace by ID
  gcx traces get -d UID <trace-id>

  # Print a Grafana Explore share link for the trace
  gcx traces get -d UID <trace-id> --share-link

  # Output as JSON
  gcx traces get -d UID <trace-id> -o json`
	cmd.AddCommand(gCmd)

	lCmd := dstempo.LabelsCmd(loader)
	lCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx traces labels -d abc123 -o json",
	}
	lCmd.Example = `
  # List all labels
  gcx traces labels -d UID

  # Output as JSON
  gcx traces labels -d UID -o json`
	cmd.AddCommand(lCmd)

	mCmd := dstempo.MetricsCmd(loader)
	mCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx traces metrics -d abc123 '{ } | rate()' --since 1h -o json`,
	}
	mCmd.Example = `
  # Run a TraceQL metrics query
  gcx traces metrics -d UID '{ } | rate()' --since 1h

  # Print a Grafana Explore share link for the query
  gcx traces metrics '{ } | rate()' --share-link

  # Output as JSON
  gcx traces metrics -d UID '{ } | rate()' --since 1h -o json`
	cmd.AddCommand(mCmd)

	// Adaptive Traces subcommands — rename Use from "traces" to "adaptive".
	adaptiveCmd := adaptivetraces.Commands(loader)
	adaptiveCmd.Use = "adaptive"
	adaptiveCmd.Short = "Manage Adaptive Traces resources"
	cmd.AddCommand(adaptiveCmd)

	return []*cobra.Command{cmd}
}

// queryCmd and metricsCmd are thin wrappers used by expr_test.go.
func queryCmd(loader *providers.ConfigLoader) *cobra.Command   { return dstempo.QueryCmd(loader) }
func metricsCmd(loader *providers.ConfigLoader) *cobra.Command { return dstempo.MetricsCmd(loader) }

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
