package azure

import (
	"context"
	"io"

	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/onboard"
	"github.com/grafana/grafana-app-sdk/logging"
)

// Friendly datasource tokens accepted by --only and shown in the picker.
const (
	TokenAzureMonitor = "azure-monitor"
	TokenADX          = "adx"
	TokenCosmos       = "cosmos"
)

// Grafana plugin IDs.
const (
	KindAzureMonitor = "grafana-azure-monitor-datasource"
	KindADX          = "grafana-azure-data-explorer-datasource"
	KindCosmos       = "grafana-azurecosmosdb-datasource"
)

// RoleOption is a selectable set of Azure RBAC roles for a datasource, ordered
// from most-capable default to tighter alternatives.
type RoleOption struct {
	Label string
	Roles []string
}

// SpecInput carries everything a spec needs to acquire credentials and build a
// datasource create request.
type SpecInput struct {
	CLI         *CLI
	Account     Account
	Name        string            // resolved, collision-free datasource + app name
	Roles       []string          // selected role set (for app-registration specs)
	Scopes      []string          // role-assignment scopes
	CallerOID   string            // signed-in user object ID (owner)
	Extra       map[string]string // spec-specific (e.g. ADX clusterUrl/rg/cluster)
	Stack       string            // Grafana stack slug for attribution
	ExpiryDays  int               // optional minted-secret expiry (0 = default)
	Rollback    *onboard.Rollback
	Interactive bool
	Progress    io.Writer // human-readable narration for long-running steps (nil = suppressed)
	Log         logging.Logger
}

// attribution builds the Attribution stamp for artifacts this spec creates. The
// datasource UID is filled in later (after the datasource exists) by the
// orchestrator.
func (in SpecInput) attribution() Attribution {
	return Attribution{Stack: in.Stack, OwnerOID: in.CallerOID}
}

// Provisioned is the result of a spec acquiring credentials.
type Provisioned struct {
	Request datasources.Datasource
	AppID   string // empty when no app registration was minted (key-based specs)
}

// DatasourceSpec owns credential acquisition and payload construction for one
// datasource type.
type DatasourceSpec interface {
	// Token is the friendly identifier used by --only and the picker.
	Token() string
	// Kind is the Grafana plugin ID.
	Kind() string
	// RoleOptions are the selectable role sets (default first). May be empty for
	// key-based datasources.
	RoleOptions() []RoleOption
	// AcquireAndBuild mints/acquires credentials, performs any extra grants, and
	// returns the datasource create request.
	AcquireAndBuild(ctx context.Context, in SpecInput) (Provisioned, error)
}

// Suggestion is one datasource gcx proposes to create.
type Suggestion struct {
	Spec   DatasourceSpec
	Name   string            // gcx-<stack>-<token>[-<resource>] base name
	Label  string            // human label shown in the picker
	Scopes []string          // role-assignment scopes
	Extra  map[string]string // passed through to the spec
	// Disabled marks a suggestion that is shown but cannot be provisioned (e.g.
	// a stopped ADX cluster). DisabledReason explains why.
	Disabled       bool
	DisabledReason string
}

// monitorCloud maps an `az` environmentName to the Azure Monitor datasource's
// cloudName value.
func monitorCloud(env string) string {
	switch env {
	case "AzureUSGovernment":
		return "govazuremonitor"
	case "AzureChinaCloud":
		return "chinaazuremonitor"
	default:
		return "azuremonitor"
	}
}

// armCloud maps an `az` environmentName to the unified azureCredentials
// azureCloud value (used by ADX/SQL/newer plugins).
func armCloud(env string) string {
	switch env {
	case "AzureUSGovernment":
		return "AzureUSGovernment"
	case "AzureChinaCloud":
		return "AzureChinaCloud"
	default:
		return "AzureCloud"
	}
}
