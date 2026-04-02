package profiles

import (
	"fmt"

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
	loader := &providers.ConfigLoader{}

	cmd := &cobra.Command{
		Use:   "profiles",
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
		agent.AnnotationLLMHint:   "gcx profiles query abc123 '{service_name=\"frontend\"}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h -o json",
	}
	qCmd.Example = `
  # Profile query with explicit datasource UID
  gcx profiles query abc123 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Using configured default datasource
  gcx profiles query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Output as JSON
  gcx profiles query abc123 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds -o json`
	cmd.AddCommand(qCmd)

	lCmd := labelsCmd(loader)
	lCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx profiles labels -d abc123 -o json",
	}
	lCmd.Example = `
  # List all labels (use datasource UID, not name)
  gcx profiles labels -d <datasource-uid>

  # Get values for a specific label
  gcx profiles labels -d <datasource-uid> --label service_name

  # Output as JSON
  gcx profiles labels -d <datasource-uid> -o json`
	cmd.AddCommand(lCmd)

	ptCmd := profileTypesCmd(loader)
	ptCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx profiles profile-types -d abc123 -o json",
	}
	ptCmd.Example = `
  # List profile types (use datasource UID, not name)
  gcx profiles profile-types -d <datasource-uid>

  # Output as JSON
  gcx profiles profile-types -d <datasource-uid> -o json`
	cmd.AddCommand(ptCmd)

	sCmd := seriesCmd(loader)
	sCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx profiles series '{}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --top -o json",
	}
	sCmd.Example = `
  # Top services by CPU usage (ranked leaderboard)
  gcx profiles series '{}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --top

  # CPU usage over the last hour with 1-minute resolution
  gcx profiles series '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --step 1m

  # Output as JSON
  gcx profiles series abc123 '{}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --top -o json`
	cmd.AddCommand(sCmd)

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
