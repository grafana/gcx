package irm

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
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

	// id and changed carry the result of the update to the text codec: the
	// success line names the incident and the fields that gcx changed.
	id      string
	changed []string
}

func (o *incidentUpdateOpts) setup(flags *pflag.FlagSet) {
	// update is a mutation command, so the human default is one success line
	// (docs/design/output.md § 11), the way `incidents close` prints one. The
	// encoded document stays the incident manifest, so -o json and -o yaml
	// emit what `gcx resources get` emits for the same incident.
	o.IO.RegisterCustomCodec("text", &incidentUpdateTextCodec{render: o.report})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Severity, "severity", "",
		"New severity label (run 'gcx irm incidents severities list' for the valid values)")
	flags.StringVar(&o.Title, "title", "", "New title")
}

// report writes the success line. gcx reads the incident first and skips a
// field that already matches, so a run can change nothing at all.
func (o *incidentUpdateOpts) report(w io.Writer) {
	if len(o.changed) == 0 {
		cmdio.Info(w, "Incident %s already carries the requested values", o.id)
		return
	}
	cmdio.Success(w, "Updated incident %s (%s)", o.id, strings.Join(o.changed, ", "))
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

// incidentUpdateTextCodec renders the human result of `incidents update`: one
// line that names the incident and the fields that gcx changed. The value
// that Encode receives is the incident manifest, which the json and the yaml
// codec emit unchanged.
type incidentUpdateTextCodec struct {
	render func(w io.Writer)
}

func (c *incidentUpdateTextCodec) Format() format.Format { return "text" }

func (c *incidentUpdateTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *incidentUpdateTextCodec) Encode(w io.Writer, _ any) error {
	c.render(w)
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
-o yaml for the incident manifest.`,
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
			opts.id = args[0]

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewIncidentClient(restCfg)
			if err != nil {
				return err
			}

			// Update skips an empty field and a field that already matches, so
			// one call covers both flags. Validate rejects an explicit empty
			// value, so an empty field here is an omitted flag.
			inc, changed, err := client.Update(ctx, opts.id, &Incident{Title: opts.Title, Severity: opts.Severity})
			if err != nil {
				return err
			}
			opts.changed = changed

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
