package irm

import (
	"github.com/spf13/cobra"
)

// NewTestOnCallNounCmds builds every OnCall noun command tree for structural
// conformance tests. The loader is only consulted at RunE time, so a nil
// loader is safe for tree-shape assertions.
func NewTestOnCallNounCmds() []*cobra.Command {
	var loader OnCallConfigLoader
	return []*cobra.Command{
		newIntegrationsCmd(loader),
		newEscalationChainsCmd(loader),
		newEscalationPoliciesCmd(loader),
		newSchedulesCmd(loader),
		newShiftsCmd(loader),
		newRoutesCmd(loader),
		newWebhooksCmd(loader),
		newResolutionNotesCmd(loader),
		newShiftSwapsCmd(loader),
		newTeamsCmd(loader),
		newUserGroupsCmd(loader),
		newSlackChannelsCmd(loader),
		newOrganizationsCmd(loader),
		newUsersCommand(loader),
	}
}

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
