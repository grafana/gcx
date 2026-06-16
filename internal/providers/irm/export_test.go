package irm

import (
	"github.com/spf13/cobra"
)

// NewTestListCommand creates a list command for testing validation. Flag
// values are injected after flag setup so they go through Validate().
func NewTestListCommand(labels, statuses []string, severity, query, dateFrom, dateTo string) *cobra.Command {
	opts := &incidentListOpts{}
	cmd := &cobra.Command{
		Use:          "list",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			opts.Labels = labels
			opts.Statuses = statuses
			opts.Severity = severity
			opts.Query = query
			opts.DateFrom = dateFrom
			opts.DateTo = dateTo
			return opts.Validate()
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
