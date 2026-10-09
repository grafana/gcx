package irm

import (
	"errors"
	"io"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The IRM API has no single update method for an incident. Each mutable field
// is its own operation. The command delegates to IncidentClient.Update, the
// method the resources push path also reaches, so both paths share the read,
// the skip of an unchanged field, and the not-found signal.

type incidentUpdateOpts struct {
	IO       cmdio.Options
	Severity string
	Title    string

	// changed carries the field names to the text codec. The structured
	// mutation result reports whether the command changed the incident.
	changed []string
}

func (o *incidentUpdateOpts) setup(flags *pflag.FlagSet) {
	// The result is a SingleMutation document. The human default is one line,
	// and machine formats include the changed signal for idempotent updates.
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{
		render: func(w io.Writer, m cmdio.SingleMutation) {
			if m.Changed != nil && !*m.Changed {
				cmdio.Info(w, "Incident %s already carries the requested values", m.Target.ID)
				return
			}
			cmdio.Success(w, "Updated incident %s (%s)", m.Target.ID, strings.Join(o.changed, ", "))
		},
	})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Severity, "severity", "",
		"New severity label (run 'gcx irm incidents severities list' for the valid values)")
	flags.StringVar(&o.Title, "title", "", "New title")
}

// Validate rejects an empty value on a flag the caller set. The Incident
// schema marks the title as required, and an empty severity label matches no
// entry in the severity list. Only flags.Changed separates an explicit empty
// value from an omitted flag.
func (o *incidentUpdateOpts) Validate(flags *pflag.FlagSet) error {
	if !flags.Changed("severity") && !flags.Changed("title") {
		return errors.New("give at least one of --severity or --title")
	}
	if flags.Changed("severity") && o.Severity == "" {
		return errors.New("--severity must not be empty: run 'gcx irm incidents severities list' for the valid values")
	}
	if flags.Changed("title") && o.Title == "" {
		return errors.New("--title must not be empty: the incident title is required")
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

gcx reads the incident first, so a value that already matches causes no write.
The command prints one line that names the fields it changed. Use -o json or
-o yaml for a structured update result.`,
		Example: `  # Raise the severity of an incident:
  gcx irm incidents update 4 --severity Critical

  # Correct the title:
  gcx irm incidents update 4 --title "Checkout latency above the objective"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := opts.Validate(cmd.Flags()); err != nil {
				return err
			}

			ctx := cmd.Context()
			id := args[0]

			crud, _, err := NewTypedCRUD(ctx, loader, IncidentQuery{})
			if err != nil {
				return err
			}

			// Update skips an empty field and a field that already matches, so
			// one call covers both flags. Validate rejects an explicit empty
			// value, so an empty field here is an omitted flag.
			updated, err := crud.Update(ctx, id, &adapter.TypedObject[Incident]{
				Spec: Incident{Title: opts.Title, Severity: opts.Severity},
			})
			if err != nil {
				return err
			}
			inc := &updated.Spec
			changed := inc.updatedFields
			opts.changed = changed

			result := cmdio.NewSingleMutation("updated", cmdio.MutationTarget{
				Kind: "Incident",
				ID:   id,
				Name: inc.Title,
			})
			changedValue := len(changed) > 0
			result.Changed = &changedValue
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
