package profiles

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	dspyroscope "github.com/grafana/gcx/internal/datasources/pyroscope"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
)

var _ framework.StatusDetectable = &Provider{}

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

	// Grab the commands from the datasources package, and override the examples
	// and annotations to be suitable for the top-level commands.
	qCmd := dspyroscope.QueryCmd(loader)
	qCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx profiles query -d abc123 '{service_name="frontend"}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h -o json`,
	}
	qCmd.Example = `
  # Profile query with explicit datasource UID
  gcx profiles query -d abc123 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Using configured default datasource
  gcx profiles query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Output as JSON
  gcx profiles query -d abc123 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds -o json`
	cmd.AddCommand(qCmd)

	lCmd := dspyroscope.LabelsCmd(loader)
	lCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx profiles labels -d abc123 -o json",
	}
	lCmd.Example = `
  # List all labels (use datasource UID, not name)
  gcx profiles labels -d UID

  # Get values for a specific label
  gcx profiles labels -d UID --label service_name

  # Output as JSON
  gcx profiles labels -d UID -o json`
	cmd.AddCommand(lCmd)

	ptCmd := dspyroscope.ProfileTypesCmd(loader)
	ptCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx profiles profile-types -d abc123 -o json",
	}
	ptCmd.Example = `
  # List profile types (use datasource UID, not name)
  gcx profiles profile-types -d UID

  # Output as JSON
  gcx profiles profile-types -d UID -o json`
	cmd.AddCommand(ptCmd)

	mCmd := dspyroscope.MetricsCmd(loader)
	mCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx profiles metrics '{}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --top -o json",
	}
	mCmd.Example = `
  # Top services by CPU usage (ranked leaderboard)
  gcx profiles metrics '{}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --top

  # CPU usage over the last hour with 1-minute resolution
  gcx profiles metrics -d pyro-001 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --step 1m

  # Output as JSON
  gcx profiles metrics -d abc123 '{}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --top -o json`
	cmd.AddCommand(mCmd)

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

// queryCmd and metricsCmd are thin wrappers used by expr_test.go.
func queryCmd(loader *providers.ConfigLoader) *cobra.Command   { return dspyroscope.QueryCmd(loader) }
func metricsCmd(loader *providers.ConfigLoader) *cobra.Command { return dspyroscope.MetricsCmd(loader) }

func (p *Provider) Validate(_ map[string]string) error { return nil }

// ProductName returns the human-readable product name.
func (p *Provider) ProductName() string { return p.Name() }

// Status returns the current configuration state based on config key presence.
// This is a v1 stub: it never probes any API. Config is loaded from the active
// context; a missing or unreadable config is treated as StateNotConfigured.
func (p *Provider) Status(ctx context.Context) (*framework.ProductStatus, error) {
	loader := &providers.ConfigLoader{}
	cfg, _, _ := loader.LoadProviderConfig(ctx, p.Name())
	s := framework.ConfigKeysStatus(p, cfg)
	return &s, nil
}

func (p *Provider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "profiles-tenant-id", Secret: false},
		{Name: "profiles-tenant-url", Secret: false},
	}
}

func (p *Provider) TypedRegistrations() []adapter.Registration { return nil }
