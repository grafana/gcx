package root

import (
	"github.com/grafana/gcx/cmd/gcx/dev"
	_ "github.com/grafana/gcx/internal/datasources/providers" // DatasourceProvider registrations — blank imports trigger init() self-registration.
	"github.com/grafana/gcx/internal/providers"
	_ "github.com/grafana/gcx/internal/providers/aio11y"   // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/alert"    // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/appo11y"  // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/faro"     // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/fleet"    // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/irm"      // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/k6"       // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/kg"       // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/logs"     // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/metrics"  // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/profiles" // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/slo"      // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/synth"    // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/traces"   // Provider registrations — blank imports trigger init() self-registration.
	"github.com/grafana/gcx/internal/rootcmd"
	"github.com/spf13/cobra"
)

// IsJSONFlagActive reports whether the --json flag was actively set by the user
// on the command that was actually executed. Safe for concurrent use.
func IsJSONFlagActive() bool {
	return rootcmd.IsJSONFlagActive()
}

// Command builds the root cobra command for the given version using the
// compile-time registered provider list.
func Command(version string) *cobra.Command {
	return newCommand(version, providers.All())
}

// newCommand builds the root cobra command with an explicit provider list.
// Callers that need to inject providers (e.g. tests) should use this directly.
// Nil entries in pp are silently skipped.
func newCommand(version string, pp []providers.Provider) *cobra.Command {
	return rootcmd.New(version, &rootcmd.Options{
		Providers:     pp,
		ExtraCommands: []*cobra.Command{dev.Command()},
	})
}
