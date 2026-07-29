package azure

import (
	"context"
	"strings"

	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/onboard"
)

// azureMonitorSpec provisions the Azure Monitor datasource. It uses the flat
// credential schema (tenantId/clientId/subscriptionId + secureJsonData.clientSecret).
type azureMonitorSpec struct{}

func (azureMonitorSpec) Token() string { return TokenAzureMonitor }
func (azureMonitorSpec) Kind() string  { return KindAzureMonitor }

func (azureMonitorSpec) RoleOptions() []RoleOption {
	// Reader (*/read) already covers metrics, Resource Graph, and Log Analytics
	// queries: in the default workspace access-control mode ("Use resource or
	// workspace permissions") a subscription-scoped read grant authorizes all
	// log reads, so a separate Log Analytics Reader is redundant here. It would
	// only matter for workspaces set to "Require workspace permissions", which
	// ignore subscription-scoped grants and need a workspace-scoped assignment
	// instead — out of scope for this subscription-level onboarding.
	return []RoleOption{
		{Label: "Default — Reader (metrics, logs, Resource Graph)", Roles: []string{"Reader"}},
		{Label: "Metrics only — Monitoring Reader", Roles: []string{"Monitoring Reader"}},
	}
}

func (s azureMonitorSpec) AcquireAndBuild(ctx context.Context, in SpecInput) (Provisioned, error) {
	cred, err := acquireCredential(ctx, in)
	if err != nil {
		return Provisioned{}, err
	}
	return Provisioned{Request: s.payload(in.Name, in.Account, cred), AppID: cred.AppID}, nil
}

func (azureMonitorSpec) payload(name string, acct Account, cred SPCredential) datasources.Datasource {
	return datasources.Datasource{
		Name:   name,
		Type:   KindAzureMonitor,
		Access: "proxy",
		JSONData: map[string]any{
			"azureAuthType":  "clientsecret",
			"cloudName":      monitorCloud(acct.CloudName),
			"tenantId":       cred.Tenant,
			"clientId":       cred.AppID,
			"subscriptionId": acct.SubID,
		},
		SecureJSONData: map[string]string{
			"clientSecret": cred.Password,
		},
	}
}

// acquireCredential mints a new gcx-owned app registration (service principal +
// secret + least-privilege role assignment) for the datasource.
func acquireCredential(ctx context.Context, in SpecInput) (SPCredential, error) {
	onboard.Progressf(in.Progress, "  minting Azure app registration (this can take ~10-20s)...")
	cred, err := in.CLI.CreateOwnedAppRegistration(ctx, AppRegistrationRequest{
		Name:        in.Name,
		Roles:       in.Roles,
		Scopes:      in.Scopes,
		CallerOID:   in.CallerOID,
		Tenant:      in.Account.TenantID,
		Description: in.attribution().roleDescription(in.Name),
		ExpiryDays:  in.ExpiryDays,
		AddUndo:     in.Rollback.Add,
	})
	if err != nil {
		return SPCredential{}, err
	}
	onboard.Progressf(in.Progress, "  app registration ready (clientId %s, roles: %s)", cred.AppID, strings.Join(in.Roles, ", "))
	return cred, nil
}
