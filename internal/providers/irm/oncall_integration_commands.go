package irm

import (
	"fmt"
	"io"
	"sort"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The alert templates and the maintenance state of an integration are two
// sub-resources that the Integration object itself cannot carry: the template
// document lives behind its own collection, and maintenance starts through an
// action endpoint. Both therefore appear as verbs on the integrations noun,
// per the sub-resource rule in CONSTITUTION.md.

// ---------------------------------------------------------------------------
// integrations get-templates / update-templates
// ---------------------------------------------------------------------------

func newIntegrationGetTemplatesCmd(loader OnCallConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get-templates <id>",
		Short: "Get the alert templates of an integration.",
		Long: `Get the alert templates of an integration.

The templates decide what a responder reads and hears: the alert title, the
message, the grouping identifier, the resolve condition, and a separate
rendering for each channel (web, phone call, Short Message Service, email,
Slack, and Microsoft Teams).

The command emits the whole template document. Edit that document, then pass
it back through update-templates.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			templates, err := client.GetIntegrationTemplates(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), templates)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newIntegrationUpdateTemplatesCmd(loader OnCallConfigLoader) *cobra.Command {
	opts := &mutateOpts{}
	cmd := &cobra.Command{
		Use:   "update-templates <id>",
		Short: "Replace the alert templates of an integration.",
		Long: `Replace the alert templates of an integration.

The file holds the template document as get-templates emits it. Every field
travels to the backend unchanged, so a field that this build does not know
about still survives a get, an edit, and an update.

The command emits the stored document.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			var templates map[string]any
			if err := providers.ReadFileOrStdin(opts.File, cmd.InOrStdin(), &templates); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			stored, err := client.UpdateIntegrationTemplates(cmd.Context(), args[0], templates)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), stored)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// integrations start-maintenance / stop-maintenance
// ---------------------------------------------------------------------------

type startMaintenanceOpts struct {
	IO       cmdio.Options
	Mode     string
	Duration int
}

func (o *startMaintenanceOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{
		render: func(w io.Writer, m cmdio.SingleMutation) {
			cmdio.Success(w, "Started maintenance on integration %s", m.Target.ID)
		},
	})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Mode, "mode", "maintenance",
		fmt.Sprintf("Maintenance mode (%s)", strings.Join(maintenanceModeFlagValues(), ", ")))
	flags.IntVar(&o.Duration, "duration", 3600, "Maintenance duration in seconds")
}

func (o *startMaintenanceOpts) Validate() error {
	if _, ok := MaintenanceModeNames[o.Mode]; !ok {
		return fmt.Errorf("unknown --mode %q, expected one of: %s",
			o.Mode, strings.Join(maintenanceModeFlagValues(), ", "))
	}
	if o.Duration <= 0 {
		return fmt.Errorf("--duration must be a positive number of seconds, got %d", o.Duration)
	}
	return nil
}

// maintenanceModeFlagValues returns the accepted --mode values in a stable
// order, for the flag help and the error message.
func maintenanceModeFlagValues() []string {
	names := make([]string, 0, len(MaintenanceModeNames))
	for name := range MaintenanceModeNames {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func newIntegrationStartMaintenanceCmd(loader OnCallConfigLoader) *cobra.Command {
	opts := &startMaintenanceOpts{}
	cmd := &cobra.Command{
		Use:   "start-maintenance <id>",
		Short: "Start maintenance on an integration.",
		Long: `Start maintenance on an integration.

Maintenance suppresses escalation during planned work. Mode "maintenance"
groups every alert of the integration into one alert group and pages nobody.
Mode "debug" routes each alert to its author only.

The backend accepts a limited set of durations. It rejects any other value.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			mode := MaintenanceModeNames[opts.Mode]
			if err := client.StartIntegrationMaintenance(cmd.Context(), args[0], int(mode), opts.Duration); err != nil {
				return err
			}

			return encodeIntegrationMutation(cmd, &opts.IO, "maintenance-started", args[0])
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type stopMaintenanceOpts struct {
	IO cmdio.Options
}

func (o *stopMaintenanceOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{
		render: func(w io.Writer, m cmdio.SingleMutation) {
			cmdio.Success(w, "Stopped maintenance on integration %s", m.Target.ID)
		},
	})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newIntegrationStopMaintenanceCmd(loader OnCallConfigLoader) *cobra.Command {
	opts := &stopMaintenanceOpts{}
	cmd := &cobra.Command{
		Use:   "stop-maintenance <id>",
		Short: "Stop maintenance on an integration.",
		Long: `Stop maintenance on an integration.

Use this to end maintenance before its scheduled end. Escalation resumes at
once.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			if err := client.StopIntegrationMaintenance(cmd.Context(), args[0]); err != nil {
				return err
			}

			return encodeIntegrationMutation(cmd, &opts.IO, "maintenance-stopped", args[0])
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// encodeIntegrationMutation emits the structured result of an integration
// action. The action endpoints report no prior state, so the command cannot
// tell an idempotent repeat from a real change and leaves Changed unset.
func encodeIntegrationMutation(cmd *cobra.Command, opts *cmdio.Options, action, id string) error {
	result := cmdio.NewSingleMutation(action, cmdio.MutationTarget{Kind: "Integration", ID: id})
	return opts.Encode(cmd.OutOrStdout(), result)
}
