package irm

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// A team's business hours are a sub-resource that the Team object itself
// cannot carry: they live behind teams/<id>/working_hours/. They therefore
// appear as verbs on the teams noun, per the sub-resource rule in
// CONSTITUTION.md.

func newTeamGetWorkingHoursCmd(loader OnCallConfigLoader) *cobra.Command {
	opts := &workingHoursGetOpts{}
	cmd := &cobra.Command{
		Use:   "get-working-hours <id>",
		Short: "Get the business hours of a team.",
		Long: `Get the business hours of a team.

A team's business hours are a list of named intervals. Each interval holds up
to three time ranges per weekday. A team is in business hours when the current
time falls inside any of its intervals; the "pause escalation until team's
working hours" escalation step waits for that moment.

By default the command prints a weekly grid with one row per interval. Use
-o yaml or -o json to emit the document {"irm_working_hours": [...]} (the list
is null when the team has no business hours). Edit that document, then pass it
back through update-working-hours.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			hours, err := client.GetTeamWorkingHours(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			return encodeWorkingHours(&opts.IO, cmd.OutOrStdout(), hours)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newTeamUpdateWorkingHoursCmd(loader OnCallConfigLoader) *cobra.Command {
	opts := &workingHoursUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update-working-hours <id>",
		Short: "Replace the business hours of a team.",
		Long: `Replace the business hours of a team.

The file holds the document that get-working-hours emits. The intervals in it
become the team's entire set: an interval that the file omits is deleted. Pass
{"irm_working_hours": null} to clear every business hour of the team.

Times are wall-clock times (HH:MM:SS) in the team's timezone. A weekday with no
ranges is a day off, but every interval needs a time range on at least one
weekday.

Example file:

  irm_working_hours:
    - name: Office hours
      irm_working_hours:
        monday:    [{start: "09:00:00", end: "17:00:00"}]
        tuesday:   [{start: "09:00:00", end: "17:00:00"}]
        wednesday: [{start: "09:00:00", end: "17:00:00"}]
        thursday:  [{start: "09:00:00", end: "17:00:00"}]
        friday:    [{start: "09:00:00", end: "17:00:00"}]
        saturday:  []
        sunday:    []

The command prints the stored hours as a weekly grid. Use -o yaml or -o json
to emit the stored document instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			var hours map[string]any
			if err := providers.ReadFileOrStdin(opts.File, cmd.InOrStdin(), &hours); err != nil {
				return err
			}

			if err := validateTeamWorkingHours(hours); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			stored, err := client.UpdateTeamWorkingHours(cmd.Context(), args[0], hours)
			if err != nil {
				return err
			}

			return encodeWorkingHours(&opts.IO, cmd.OutOrStdout(), stored)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
