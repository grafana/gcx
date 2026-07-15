package profiles

import (
	"fmt"

	dspyroscope "github.com/grafana/gcx/internal/datasources/pyroscope"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/signals"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&Provider{})
}

// Provider manages Pyroscope datasource queries and continuous profiling.
type Provider struct{}

func (p *Provider) descriptor() signals.Descriptor {
	return signals.Descriptor{
		Name:  "profiles",
		Short: p.ShortDesc(),
		Commands: []signals.CommandSpec{
			{
				Build:     dspyroscope.QueryCmd,
				TokenCost: "medium",
				LLMHint:   `gcx profiles query -d abc123 '{service_name="frontend"}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h -o json`,
				Example: `
  # Profile query with explicit datasource UID
  gcx profiles query -d abc123 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Using configured default datasource
  gcx profiles query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Output as JSON
  gcx profiles query -d abc123 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds -o json

  # Drill into one or more specific profiles found via 'gcx profiles exemplars'
  # (--profile-id is repeatable; pass it once per UUID)
  gcx profiles query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h \
    --profile-id 550e8400-e29b-41d4-a716-446655440000 \
    --profile-id 7c9e6679-7425-40de-944b-e07fc1f90ae7

  # Restrict the query to one or more trace spans
  gcx profiles query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h \
    --span-id 00f067aa0ba902b7

  # Restrict the query to samples from one or more traces
  gcx profiles query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h \
    --trace-id 4bf92f3577b34da6a3ce929d0e0e4736

  # Restrict the flamegraph to stacks rooted at a specific call site
  # (--stacktrace-selector is repeatable; pass it once per frame, root first)
  gcx profiles query '{service_name="my-go-service"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h \
    --stacktrace-selector 'github.com/prometheus/client_golang/prometheus.(*Registry).Gather.func1'`,
			},
			{
				Build:     dspyroscope.LabelsCmd,
				TokenCost: "small",
				LLMHint:   "gcx profiles labels -d abc123 -o json",
				Example: `
  # List all labels (use datasource UID, not name)
  gcx profiles labels -d UID

  # Get values for a specific label
  gcx profiles labels -d UID --label service_name

  # Output as JSON
  gcx profiles labels -d UID -o json`,
			},
			{
				Build:     dspyroscope.ProfileTypesCmd,
				TokenCost: "small",
				LLMHint:   "gcx profiles profile-types -d abc123 -o json",
				Example: `
  # List profile types (use datasource UID, not name)
  gcx profiles profile-types -d UID

  # Output as JSON
  gcx profiles profile-types -d UID -o json`,
			},
			{
				Build:     dspyroscope.MetricsCmd,
				TokenCost: "small",
				LLMHint:   "gcx profiles metrics '{}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --top -o json",
				Example: `
  # Top services by CPU usage (ranked leaderboard)
  gcx profiles metrics '{}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --top

  # CPU usage over the last hour with 1-minute resolution
  gcx profiles metrics -d pyro-001 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --step 1m

  # Output as JSON
  gcx profiles metrics -d abc123 '{}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --top -o json`,
			},
			{
				Build:     dspyroscope.ExemplarsCmd,
				TokenCost: "small",
				LLMHint:   "gcx profiles exemplars profile '{}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h -o json",
				Example: `
  # Top individual profile exemplars (Profile ID + Span ID if span-aware)
  gcx profiles exemplars profile '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Top span exemplars (profiles linked to trace spans; needs otelpyroscope)
  gcx profiles exemplars span '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Output as JSON for scripting
  gcx profiles exemplars profile '{}' --since 30m -o json`,
			},
		},
		ExtraCommands: []signals.CommandBuilder{func(*providers.ConfigLoader) *cobra.Command {
			return adaptiveStubCmd()
		}},
		ConfigKeys: []providers.ConfigKey{
			{Name: "profiles-tenant-id", Secret: false},
			{Name: "profiles-tenant-url", Secret: false},
		},
	}
}

func (p *Provider) Name() string { return "profiles" }

func (p *Provider) ShortDesc() string {
	return "Query Pyroscope datasources and manage continuous profiling"
}

func (p *Provider) Commands() []*cobra.Command {
	return []*cobra.Command{signals.Command(p.descriptor())}
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

func (p *Provider) ConfigKeys() []providers.ConfigKey {
	return p.descriptor().ConfigKeys
}

func (p *Provider) TypedRegistrations() []adapter.Registration { return nil }
