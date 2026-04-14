package incidents

import (
	"github.com/spf13/cobra"
)

// NewTestListCommand creates a list command for testing label validation.
// Labels are injected after flag setup so they are validated in RunE.
func NewTestListCommand(labels []string) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:          "list",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			opts.Labels = labels
			return opts.Validate()
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
