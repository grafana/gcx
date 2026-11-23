//go:build !wasip1

package root

import (
	"github.com/grafana/gcx/cmd/gcx/ext"
	"github.com/spf13/cobra"
)

func addExtCommand(rootCmd *cobra.Command) {
	rootCmd.AddCommand(ext.Command())
}
