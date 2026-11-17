//go:build !wasip1

package root

import (
	"github.com/grafana/gcx/cmd/gcx/dev"
	"github.com/spf13/cobra"
)

func addDevCommand(rootCmd *cobra.Command) {
	rootCmd.AddCommand(dev.Command())
}
