package query

import (
	"errors"

	"github.com/spf13/cobra"
)

// TempoCmd returns the `query` subcommand for the Tempo command group.
func TempoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "query",
		Short: "Execute a Tempo query (not yet available)",
		Long:  "Tempo query support is not yet implemented. This subcommand is a placeholder for future use.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("tempo queries are not yet implemented")
		},
	}
}
