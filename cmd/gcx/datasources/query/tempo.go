package query

import (
	"errors"

	"github.com/spf13/cobra"
)

// TempoCmd returns the `query` subcommand for a Tempo datasource parent.
func TempoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "query",
		Short: "Execute a Tempo query",
		Long:  "Execute a query against a Tempo datasource. Note: this subcommand is not yet implemented and will return an error.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("tempo queries are not yet implemented")
		},
	}
}
