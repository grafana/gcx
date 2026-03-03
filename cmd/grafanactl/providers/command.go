package providers

import (
	"fmt"
	"text/tabwriter"

	coreproviders "github.com/grafana/grafanactl/internal/providers"
	"github.com/spf13/cobra"
)

// Command returns the "providers" command that lists all registered providers.
func Command(pp []coreproviders.Provider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "providers",
		Short: "List registered providers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(pp) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No providers registered.\n")
				return nil
			}

			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', tabwriter.TabIndent|tabwriter.DiscardEmptyColumns)

			fmt.Fprintf(tab, "NAME\tDESCRIPTION\n")
			for _, p := range pp {
				if p == nil {
					continue
				}
				fmt.Fprintf(tab, "%s\t%s\n", p.Name(), p.ShortDesc())
			}

			return tab.Flush()
		},
	}

	return cmd
}
