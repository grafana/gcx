// Package embed provides the gcx cobra command tree for embedding, excluding
// development-only commands (dev/lint) that pull in heavy transitive deps.
package embed

import (
	_ "github.com/grafana/gcx/internal/datasources/providers" // DatasourceProvider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/aio11y"      // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/alert"       // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/appo11y"     // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/faro"        // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/fleet"       // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/irm"         // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/k6"          // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/kg"          // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/logs"        // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/metrics"     // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/profiles"    // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/slo"         // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/synth"       // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/traces"      // Provider registrations — blank imports trigger init() self-registration.
	"github.com/grafana/gcx/internal/rootcmd"
	"github.com/spf13/cobra"
)

// Command builds the gcx cobra command tree for embedding.
// It mirrors cmd/gcx/root.Command but excludes the dev subcommand
// (and its linter dependency) to avoid heavy transitive deps.
func Command(version string) *cobra.Command {
	// No ExtraCommands — dev.Command() intentionally omitted to avoid
	// linter → Loki dep chain.
	return rootcmd.New(version, nil)
}
