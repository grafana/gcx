package setup

import (
	"fmt"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/spf13/pflag"
)

// Exported aliases for unexported types and constructors, available to
// external test packages only.

// NewStatusOptsForTest constructs setupStatusOpts with flags bound, for
// output-contract tests. Call agent.SetFlag before this — the agent-mode
// default format is resolved at bind time.
func NewStatusOptsForTest(flags *pflag.FlagSet) *setupStatusOpts {
	opts := &setupStatusOpts{}
	opts.setup(flags)
	return opts
}

// StatusDocForTest builds the setupStatus document that the status RunE encodes
// when the collector app plugin is enabled and the login holds both actions.
func StatusDocForTest(enabled bool, clusters int) setupStatus {
	return newSetupStatus([]setupProductStatus{
		CollectorRowForTest(true, true, true, true, true),
		{
			Product: "instrumentation",
			Enabled: enabled,
			Health:  "healthy",
			Details: fmt.Sprintf("%d clusters", clusters),
		},
	})
}

// CollectorRowForTest builds the Fleet Management preflight row for the given
// plugin and permission state.
func CollectorRowForTest(installed, pluginEnabled, actionsKnown, canRead, canAdmin bool) setupProductStatus {
	return collectorAppRow(fleet.CollectorAppState{
		PluginKnown:  true,
		Installed:    installed,
		Enabled:      pluginEnabled,
		ActionsKnown: actionsKnown,
		CanRead:      canRead,
		CanAdmin:     canAdmin,
	})
}

// UnknownPluginRowForTest builds the Fleet Management row for the case where
// Grafana answers the plugin settings endpoint with the given status, so the
// plugin state stays unknown.
func UnknownPluginRowForTest(status int) setupProductStatus {
	return collectorAppRow(fleet.CollectorAppState{PluginStatus: status})
}

// StatusRowFieldsForTest exposes a product row's fields to external tests, in
// the order product, enabled, health, details.
func StatusRowFieldsForTest(row setupProductStatus) (string, bool, string, string) {
	return row.Product, row.Enabled, row.Health, row.Details
}
