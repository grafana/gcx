package irm

import (
	"errors"
	"fmt"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The IRM API has no single update method for an incident. Each mutable
// field is its own operation, so this command applies one flag per call and
// echoes the incident once, after the last call.

type incidentUpdateOpts struct {
	IO       cmdio.Options
	Severity string
	Title    string
}

func (o *incidentUpdateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Severity, "severity", "",
		"New severity label (run `gcx irm incidents severities list` for the valid values)")
	flags.StringVar(&o.Title, "title", "", "New title")
}

func (o *incidentUpdateOpts) Validate() error {
	if o.Severity == "" && o.Title == "" {
		return errors.New("give at least one of --severity or --title")
	}
	return nil
}

func NewUpdateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &incidentUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update the severity or the title of an incident.",
		Long: `Update the severity or the title of an incident.

The severity is the display label, not the identifier. Run
` + "`gcx irm incidents severities list`" + ` for the labels of your organization.

The command emits the updated incident.`,
		Example: `  # Raise the severity of an incident:
  gcx irm incidents update 4 --severity Critical

  # Correct the title:
  gcx irm incidents update 4 --title "Checkout latency above the objective"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			id := args[0]

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewIncidentClient(restCfg)
			if err != nil {
				return err
			}

			// Validate guarantees at least one flag, so at least one call
			// runs and inc is never nil below.
			var inc *Incident
			if opts.Title != "" {
				if inc, err = client.UpdateTitle(ctx, id, opts.Title); err != nil {
					return err
				}
			}
			if opts.Severity != "" {
				if inc, err = client.UpdateSeverity(ctx, id, opts.Severity); err != nil {
					return err
				}
			}

			res, err := ToResource(*inc, restCfg.Namespace)
			if err != nil {
				return fmt.Errorf("failed to convert incident to resource: %w", err)
			}

			obj := res.ToUnstructured()
			return opts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
