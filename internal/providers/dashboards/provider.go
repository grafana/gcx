package dashboards

// DashboardsProvider manages Grafana dashboard resources via the K8s dynamic API tier.
// It is a commands-only provider: TypedRegistrations, Validate, and ConfigKeys all return nil
// because dashboards are already served by the existing K8s dynamic path (gcx resources).
//
// Self-registration follows the database/sql driver pattern: importing this package
// as a blank import (e.g. in cmd/gcx/root/command.go) is sufficient to register
// the provider via init().

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&DashboardsProvider{})
}

// DashboardsProvider contributes the `gcx dashboards` command subtree.
type DashboardsProvider struct{}

// Name returns the unique identifier for this provider.
func (p *DashboardsProvider) Name() string { return "dashboards" }

// ShortDesc returns a one-line description of the provider.
func (p *DashboardsProvider) ShortDesc() string {
	return "Manage Grafana dashboards (CRUD, search, snapshots)"
}

// Commands returns the Cobra commands contributed by this provider.
func (p *DashboardsProvider) Commands() []*cobra.Command {
	return commands()
}

// Validate is a no-op: the dashboards provider uses Grafana's built-in authentication
// and does not require additional provider-specific configuration keys.
func (p *DashboardsProvider) Validate(_ map[string]string) error { return nil }

// ConfigKeys returns nil: no provider-specific config keys are required.
func (p *DashboardsProvider) ConfigKeys() []providers.ConfigKey { return nil }

// TypedRegistrations returns nil: dashboards are served via the K8s dynamic tier,
// not through provider adapter factories.
func (p *DashboardsProvider) TypedRegistrations() []adapter.Registration { return nil }
