// Package wire is a bootstrap package that breaks the import cycle between the
// instrumentation parent package and its subpackages.
//
// The instrumentation subpackages (apps, clusters, check, discover, setup,
// status) all import the parent package for shared types and constructors.
// This means the parent cannot import them directly. This package sits above
// both: it imports the parent to call SetCommandsBuilder and imports all
// subpackages to assemble the command tree.
//
// Root wiring: cmd/gcx/root/command.go blank-imports this package so its
// init() runs before any cobra command is executed.
package wire

import (
	"github.com/grafana/gcx/internal/providers"
	instr "github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/providers/instrumentation/apps"
	"github.com/grafana/gcx/internal/providers/instrumentation/check"
	"github.com/grafana/gcx/internal/providers/instrumentation/clusters"
	"github.com/grafana/gcx/internal/providers/instrumentation/discover"
	instrsetup "github.com/grafana/gcx/internal/providers/instrumentation/setup"
	"github.com/grafana/gcx/internal/providers/instrumentation/status"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Bootstrap wiring — sets the command builder on the instrumentation provider.
	instr.SetCommandsBuilder(func() []*cobra.Command {
		loader := &providers.ConfigLoader{}

		instrCmd := &cobra.Command{
			Use:   "instrumentation",
			Short: "Manage Grafana Cloud instrumentation (clusters and apps)",
			PersistentPreRun: func(cmd *cobra.Command, args []string) {
				if root := cmd.Root(); root.PersistentPreRun != nil {
					root.PersistentPreRun(cmd, args)
				}
			},
		}
		loader.BindFlags(instrCmd.PersistentFlags())

		clustersCmd := clusters.Commands(loader)
		clustersCmd.AddCommand(
			instrsetup.Command(loader),
			discover.Command(loader),
			check.Command(loader),
		)

		instrCmd.AddCommand(
			clustersCmd,
			apps.Commands(loader),
			status.Command(loader),
		)

		return []*cobra.Command{instrCmd}
	})
}
