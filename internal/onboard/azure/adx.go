package azure

import (
	"context"
	"strings"

	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/onboard"
)

// adxSpec provisions the Azure Data Explorer datasource. It uses the unified
// azureCredentials schema and additionally grants the minted principal
// AllDatabasesViewer on the target cluster.
type adxSpec struct{}

func (adxSpec) Token() string { return TokenADX }
func (adxSpec) Kind() string  { return KindADX }

func (adxSpec) RoleOptions() []RoleOption {
	return []RoleOption{
		{Label: "Reader on subscription + AllDatabasesViewer on cluster", Roles: []string{"Reader"}},
	}
}

func (s adxSpec) AcquireAndBuild(ctx context.Context, in SpecInput) (Provisioned, error) {
	cred, err := acquireCredential(ctx, in)
	if err != nil {
		return Provisioned{}, err
	}

	// Grant the minted principal viewer access to the cluster's databases.
	assignment := onboardAssignmentName(cred.AppID)
	rg, cluster := in.Extra["rg"], in.Extra["cluster"]
	onboard.Progressf(in.Progress, "  granting AllDatabasesViewer on cluster %q...", cluster)
	if err := in.CLI.GrantADXClusterViewer(ctx,
		rg, cluster, assignment, cred.AppID, in.Account.TenantID); err != nil {
		return Provisioned{}, err
	}
	if in.Rollback != nil {
		in.Rollback.Add("delete ADX cluster assignment "+assignment, func(ctx context.Context) error {
			return in.CLI.DeleteADXClusterAssignment(ctx, rg, cluster, assignment)
		})
	}

	return Provisioned{Request: s.payload(in.Name, in.Account, cred, in.Extra["clusterUrl"]), AppID: cred.AppID}, nil
}

func (adxSpec) payload(name string, acct Account, cred SPCredential, clusterURL string) datasources.Datasource {
	return datasources.Datasource{
		Name:   name,
		Type:   KindADX,
		Access: "proxy",
		JSONData: map[string]any{
			"clusterUrl": clusterURL,
			"azureCredentials": map[string]any{
				"authType":   "clientsecret",
				"azureCloud": armCloud(acct.CloudName),
				"tenantId":   cred.Tenant,
				"clientId":   cred.AppID,
			},
		},
		SecureJSONData: map[string]string{
			"azureClientSecret": cred.Password,
		},
	}
}

// onboardAssignmentName derives a stable, unique-per-app principal assignment
// name for the cluster grant.
func onboardAssignmentName(appID string) string {
	short := strings.ReplaceAll(appID, "-", "")
	if len(short) > 12 {
		short = short[:12]
	}
	return "gcx-" + short
}
