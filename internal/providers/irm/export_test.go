package irm

import (
	"github.com/spf13/cobra"
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

// NewTestOnCallNounCmds returns the OnCall noun command builders for
// structural conformance testing. Construction is safe with a nil loader
// because the loader is only invoked inside RunE.
func NewTestOnCallNounCmds() []*cobra.Command {
	return []*cobra.Command{
		newIntegrationsCmd(nil),
		newEscalationChainsCmd(nil),
		newEscalationPoliciesCmd(nil),
		newSchedulesCmd(nil),
		newShiftsCmd(nil),
		newRoutesCmd(nil),
		newWebhooksCmd(nil),
		newAlertGroupsCommand(nil),
		newUsersCommand(nil),
		newTeamsCmd(nil),
		newUserGroupsCmd(nil),
		newSlackChannelsCmd(nil),
		newAlertsCmd(nil),
		newOrganizationsCmd(nil),
		newResolutionNotesCmd(nil),
		newShiftSwapsCmd(nil),
	}
}
