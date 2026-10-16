package root

import "github.com/spf13/cobra"

// addDevCommand leaves out gcx dev in wasip1 builds: they run embedded, with
// no local project to scaffold, lint or serve, and dropping it removes the
// linter's policy engine and the dashboard SDK from the module.
func addDevCommand(*cobra.Command) {}
