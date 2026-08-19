package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// secretEnvVar carries the minted client secret to gcx out of band. The
// manifest piped to `gcx datasources create` references it with
// {fromEnv: ...}, so the secret never lands in argv, in a file, or in the
// manifest text itself.
const secretEnvVar = "GCX_EXT_AZURE_CLIENT_SECRET"

// datasourceResult is one row of the run's outcome.
type datasourceResult struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	UID      string   `json:"uid,omitempty"`
	Status   string   `json:"status"`
	Health   string   `json:"health,omitempty"`
	ClientID string   `json:"clientId,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	Scope    string   `json:"scope,omitempty"`
	Hint     string   `json:"hint,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// runResult is what the extension prints on stdout.
type runResult struct {
	Subscription string             `json:"subscription"`
	Context      string             `json:"context,omitempty"`
	DryRun       bool               `json:"dryRun,omitempty"`
	Datasources  []datasourceResult `json:"datasources"`
	Removed      []removedArtifact  `json:"removed,omitempty"`
}

func provision(ctx context.Context, o *options, g *gcxClient, az azCLI, progress io.Writer) (*runResult, error) {
	if err := az.ensure(); err != nil {
		return nil, err
	}

	acct, err := resolveAccount(ctx, az, o.subscription)
	if err != nil {
		return nil, err
	}

	gcxContext := g.currentContext(ctx)
	label := gcxContext
	if label == "" {
		label = "grafana"
	}

	progressf(progress, "Planning against subscription %s (%s)...", acct.Name, acct.ID)
	plan := buildPlan(ctx, az, acct, label, o.types)
	if len(plan) == 0 {
		return nil, fmt.Errorf("nothing to onboard in subscription %s", acct.ID)
	}

	result := &runResult{Subscription: acct.ID, Context: gcxContext, DryRun: o.dryRun}

	existing, err := g.listDatasources(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, ds := range existing {
		known[ds.UID] = true
	}

	for _, s := range plan {
		switch {
		case known[s.UID]:
			result.Datasources = append(result.Datasources, datasourceResult{
				Name: s.Name, Type: s.Kind, UID: s.UID, Status: "existing", Hint: s.Hint,
			})
			continue
		case o.dryRun:
			result.Datasources = append(result.Datasources, datasourceResult{
				Name: s.Name, Type: s.Kind, UID: s.UID, Status: "planned",
				Roles: s.Roles, Scope: s.Scope, Hint: s.Hint,
			})
			continue
		}

		row, err := provisionOne(ctx, g, az, acct, s, progress)
		if err != nil {
			row = datasourceResult{Name: s.Name, Type: s.Kind, UID: s.UID, Status: "failed", Error: err.Error()}
		}
		row.Hint = s.Hint
		result.Datasources = append(result.Datasources, row)
	}

	return result, nil
}

func provisionOne(ctx context.Context, g *gcxClient, az azCLI, acct account, s suggestion, progress io.Writer) (datasourceResult, error) {
	progressf(progress, "Minting Azure app registration %s (this takes ~10-20s)...", s.Name)
	sp, err := az.createServicePrincipal(ctx, s.Name, s.Roles, s.Scope, secretExpiryYears)
	if err != nil {
		return datasourceResult{}, err
	}

	manifest, err := buildManifest(acct, s, sp)
	if err != nil {
		return datasourceResult{}, err
	}

	progressf(progress, "Creating datasource %s in Grafana...", s.Name)
	if err := g.stdinJSON(ctx, manifest, []string{secretEnvVar + "=" + sp.Password}, nil,
		"datasources", "create", "-f", "-"); err != nil {
		return datasourceResult{}, err
	}

	row := datasourceResult{
		Name: s.Name, Type: s.Kind, UID: s.UID, Status: "created",
		ClientID: sp.AppID, Roles: s.Roles, Scope: s.Scope,
	}
	row.Health = checkHealth(ctx, g, s.UID, progress)
	return row, nil
}

// buildManifest renders the datasource manifest gcx will apply. The secret is
// referenced by env var, never inlined.
func buildManifest(acct account, s suggestion, sp *servicePrincipal) ([]byte, error) {
	jsonData := map[string]any{
		"azureAuthType":  "clientsecret",
		"cloudName":      monitorCloud(acct.CloudName),
		"tenantId":       sp.TenantID,
		"clientId":       sp.AppID,
		"subscriptionId": acct.ID,
	}
	if s.Token == tokenADX {
		jsonData = map[string]any{
			"azureCloud":        adxCloud(acct.CloudName),
			"clusterUrl":        s.ClusterURI,
			"tenantId":          sp.TenantID,
			"clientId":          sp.AppID,
			"dataConsistency":   "strongconsistency",
			"defaultEditorMode": "visual",
		}
	}

	secretKey := "clientSecret"
	manifest := map[string]any{
		"apiVersion": s.Kind + ".datasource.grafana.app/v0alpha1",
		"kind":       "DataSource",
		"metadata":   map[string]any{"name": s.UID},
		"secure": map[string]any{
			secretKey: map[string]any{"fromEnv": secretEnvVar},
		},
		"spec": map[string]any{
			"type":     s.Kind,
			"title":    s.Name,
			"access":   "proxy",
			"jsonData": jsonData,
		},
	}
	return json.Marshal(manifest)
}

// checkHealth asks gcx to verify the datasource works. A failed check is
// reported, not fatal: the credential can take a moment to propagate.
func checkHealth(ctx context.Context, g *gcxClient, uid string, progress io.Writer) string {
	progressf(progress, "Checking datasource health...")
	var payload struct {
		Datasources []struct {
			UID    string `json:"uid"`
			Status string `json:"status"`
		} `json:"datasources"`
	}
	for attempt := 0; attempt < 3; attempt++ {
		err := g.json(ctx, &payload, "datasources", "health", uid)
		for _, d := range payload.Datasources {
			if d.UID == uid && d.Status != "" {
				if d.Status == "OK" || err == nil {
					return d.Status
				}
			}
		}
		if !sleepCtx(ctx, 5*time.Second) {
			break
		}
	}
	return "UNKNOWN"
}

func resolveAccount(ctx context.Context, az azCLI, subscription string) (account, error) {
	accounts, err := az.listAccounts(ctx)
	if err != nil {
		return account{}, err
	}
	if subscription != "" {
		for _, a := range accounts {
			if a.ID == subscription || a.Name == subscription {
				return a, nil
			}
		}
		return account{}, fmt.Errorf("subscription %q not found in `az account list`", subscription)
	}
	for _, a := range accounts {
		if a.IsDefault {
			return a, nil
		}
	}
	return accounts[0], nil
}

// monitorCloud maps az cloud names onto the Azure Monitor plugin's values.
func monitorCloud(cloudName string) string {
	switch cloudName {
	case "AzureUSGovernment":
		return "usgovazuremonitor"
	case "AzureChinaCloud":
		return "chinaazuremonitor"
	default:
		return "azuremonitor"
	}
}

// adxCloud maps az cloud names onto the ADX plugin's values.
func adxCloud(cloudName string) string {
	switch cloudName {
	case "AzureUSGovernment":
		return "usgovazure"
	case "AzureChinaCloud":
		return "chinaazure"
	default:
		return "azuremonitor"
	}
}
