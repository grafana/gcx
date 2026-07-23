package alert

import "github.com/spf13/cobra"

// Exported command constructors for external test packages only. Commands
// bind their output flags (and resolve the agent-mode default format) at
// construction time, so tests must set agent mode BEFORE calling these.

// NewGroupsStatusCommandForTest wraps newGroupsStatusCommand.
func NewGroupsStatusCommandForTest(loader GrafanaConfigLoader) *cobra.Command {
	return newGroupsStatusCommand(loader)
}

// NewInstancesListCommandForTest wraps newInstancesListCommand.
func NewInstancesListCommandForTest(loader GrafanaConfigLoader) *cobra.Command {
	return newInstancesListCommand(loader)
}

// NewContactPointsDeleteCommandForTest wraps newContactPointsDeleteCommand.
func NewContactPointsDeleteCommandForTest(loader GrafanaConfigLoader) *cobra.Command {
	return newContactPointsDeleteCommand(loader)
}

// NewMuteTimingsDeleteCommandForTest wraps newMuteTimingsDeleteCommand.
func NewMuteTimingsDeleteCommandForTest(loader GrafanaConfigLoader) *cobra.Command {
	return newMuteTimingsDeleteCommand(loader)
}

// NewTemplatesDeleteCommandForTest wraps newTemplatesDeleteCommand.
func NewTemplatesDeleteCommandForTest(loader GrafanaConfigLoader) *cobra.Command {
	return newTemplatesDeleteCommand(loader)
}

// NewNotificationPoliciesSetCommandForTest wraps newNotificationPoliciesSetCommand.
func NewNotificationPoliciesSetCommandForTest(loader GrafanaConfigLoader) *cobra.Command {
	return newNotificationPoliciesSetCommand(loader)
}

// NewNotificationPoliciesResetCommandForTest wraps newNotificationPoliciesResetCommand.
func NewNotificationPoliciesResetCommandForTest(loader GrafanaConfigLoader) *cobra.Command {
	return newNotificationPoliciesResetCommand(loader)
}
