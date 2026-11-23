package root

import "github.com/spf13/cobra"

// addExtCommand leaves out gcx ext in wasip1 builds: extensions are local
// executables, and the sandbox cannot start processes.
func addExtCommand(*cobra.Command) {}
