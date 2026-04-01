package profiles

import (
	"fmt"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/datasources"
	"github.com/grafana/gcx/cmd/gcx/datasources/query"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&Provider{})
}

// Provider manages Pyroscope datasource queries and continuous profiling.
type Provider struct{}

func (p *Provider) Name() string { return "profiles" }

func (p *Provider) ShortDesc() string {
	return "Query Pyroscope datasources and manage continuous profiling"
}

func (p *Provider) Commands() []*cobra.Command {
	configOpts := &cmdconfig.Options{}

	cmd := &cobra.Command{
		Use:   "profiles",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	configOpts.BindFlags(cmd.PersistentFlags())

	// Datasource-origin subcommands.
	qCmd := query.PyroscopeCmd(configOpts)
	qCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   "gcx profiles query abc123 'process_cpu:cpu:nanoseconds:cpu:nanoseconds{}' -o json",
	}
	qCmd.Example = `
  # Query profiles using configured default datasource
  gcx profiles query <datasource-uid> 'process_cpu:cpu:nanoseconds:cpu:nanoseconds{}'

  # Output as JSON
  gcx profiles query <datasource-uid> '<expr>' -o json`
	cmd.AddCommand(qCmd)

	labelsCmd := datasources.PyroscopeLabelsCmd(configOpts)
	labelsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx profiles labels -d abc123 -o json",
	}
	labelsCmd.Example = `
  # List all labels (use datasource UID, not name)
  gcx profiles labels -d <datasource-uid>

  # Get values for a specific label
  gcx profiles labels -d <datasource-uid> --label service_name

  # Output as JSON
  gcx profiles labels -d <datasource-uid> -o json`
	cmd.AddCommand(labelsCmd)

	profileTypesCmd := datasources.ProfileTypesCmd(configOpts)
	profileTypesCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx profiles profile-types -d abc123 -o json",
	}
	profileTypesCmd.Example = `
  # List profile types (use datasource UID, not name)
  gcx profiles profile-types -d <datasource-uid>

  # Output as JSON
  gcx profiles profile-types -d <datasource-uid> -o json`
	cmd.AddCommand(profileTypesCmd)

	seriesCmd := query.PyroscopeSeriesCmd(configOpts)
	seriesCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx profiles series -d abc123 --match '{service_name="myservice"}' -o json`,
	}
	seriesCmd.Example = `
  # List series matching a selector (use datasource UID, not name)
  gcx profiles series -d <datasource-uid> --match '{service_name="myservice"}'

  # Output as JSON
  gcx profiles series -d <datasource-uid> --match '{service_name="myservice"}' -o json`
	cmd.AddCommand(seriesCmd)

	// Adaptive Profiles stub.
	cmd.AddCommand(adaptiveStubCmd())

	return []*cobra.Command{cmd}
}

func adaptiveStubCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "adaptive",
		Short: "Manage Adaptive Profiles (not yet available)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.ErrOrStderr(), "Adaptive Profiles is not yet available.")
			return nil
		},
	}
}

func (p *Provider) Validate(_ map[string]string) error { return nil }

func (p *Provider) ConfigKeys() []providers.ConfigKey { return nil }

func (p *Provider) TypedRegistrations() []adapter.Registration { return nil }
