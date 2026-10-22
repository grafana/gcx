package setup

import (
	"fmt"

	"github.com/grafana/gcx/internal/fleet"
)

// collectorAppRow renders the Fleet preflight as a setup status row.
func collectorAppRow(state fleet.CollectorAppState) setupProductStatus {
	switch {
	case !state.PluginKnown:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: false,
			Health:  "unknown",
			Details: fmt.Sprintf("HTTP %d from %s; the plugin state is unknown", state.PluginStatus, fleet.CollectorAppSettingsPath),
		}
	case !state.Installed:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: false,
			Health:  "unhealthy",
			Details: "the " + fleet.CollectorAppID + " plugin is not installed",
		}
	case !state.Enabled:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: false,
			Health:  "unhealthy",
			Details: "the " + fleet.CollectorAppID + " plugin is installed but not enabled",
		}
	case !state.ActionsKnown:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: true,
			Health:  "unknown",
			Details: "plugin enabled; route permissions unknown",
		}
	case state.CanRead && state.CanAdmin:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: true,
			Health:  "healthy",
			Details: "read and admin routes",
		}
	case state.CanRead:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: true,
			Health:  "degraded",
			Details: "limited access; some commands, including read-only commands, need the " + fleet.CollectorAppAdminAction + " action",
		}
	case state.CanAdmin:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: true,
			Health:  "degraded",
			Details: "limited access; named read routes need the " + fleet.CollectorAppReadAction + " action",
		}
	default:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: true,
			Health:  "unhealthy",
			Details: "your login has neither the " + fleet.CollectorAppReadAction + " nor the " + fleet.CollectorAppAdminAction + " action",
		}
	}
}
