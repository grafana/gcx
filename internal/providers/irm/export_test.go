package irm

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// NewTestListCommand creates a list command for testing validation.
// Labels and date strings are injected after flag setup so they go through Validate().
func NewTestListCommand(labels []string, dateFrom, dateTo string) *cobra.Command {
	opts := &incidentListOpts{}
	cmd := &cobra.Command{
		Use:          "list",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			opts.Labels = labels
			opts.DateFrom = dateFrom
			opts.DateTo = dateTo
			return opts.Validate()
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ValidateAlertGroupActionDefaultIO sets up alertGroupActionOpts with default
// flags and returns the result of IO.Validate(). Used to guard against the
// DefaultFormat being a value the codec registry doesn't recognise.
func ValidateAlertGroupActionDefaultIO() error {
	o := &alertGroupActionOpts{}
	o.setup(pflag.NewFlagSet("test", pflag.ContinueOnError))
	return o.IO.Validate()
}

// ValidateEscalateDefaultIO is the escalateOpts counterpart of
// ValidateAlertGroupActionDefaultIO.
func ValidateEscalateDefaultIO() error {
	o := &escalateOpts{}
	o.setup(pflag.NewFlagSet("test", pflag.ContinueOnError))
	return o.IO.Validate()
}
