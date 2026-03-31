package linter

import (
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Lint Grafana resources",
		Long:  "Lint Grafana resources.",
	}

	cmd.AddCommand(lintCmd())
	cmd.AddCommand(newCmd())
	cmd.AddCommand(rulesCmd())
	cmd.AddCommand(testCmd())

	return cmd
}
