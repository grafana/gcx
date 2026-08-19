package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// azCLI wraps the local `az` CLI. The extension owns this dependency entirely:
// gcx knows nothing about Azure.
type azCLI struct{}

func (azCLI) ensure() error {
	if _, err := exec.LookPath("az"); err != nil {
		return errors.New("the Azure CLI (az) is required but was not found on PATH; install it from https://learn.microsoft.com/cli/azure/install-azure-cli and run `az login`")
	}
	return nil
}

func (azCLI) run(ctx context.Context, out any, args ...string) error {
	full := append(args, "--only-show-errors", "-o", "json")
	cmd := exec.CommandContext(ctx, "az", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("az %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	if out == nil {
		return nil
	}
	data := bytes.TrimSpace(stdout.Bytes())
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// account is one Azure subscription as reported by `az account`.
type account struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TenantID  string `json:"tenantId"`
	IsDefault bool   `json:"isDefault"`
	State     string `json:"state"`
	CloudName string `json:"cloudName"`
}

func (a azCLI) listAccounts(ctx context.Context) ([]account, error) {
	var accounts []account
	if err := a.run(ctx, &accounts, "account", "list"); err != nil {
		return nil, err
	}
	enabled := accounts[:0]
	for _, acct := range accounts {
		if strings.EqualFold(acct.State, "Enabled") {
			enabled = append(enabled, acct)
		}
	}
	if len(enabled) == 0 {
		return nil, errors.New("no enabled Azure subscriptions found; run `az login`")
	}
	return enabled, nil
}

// kustoCluster is a discovered Azure Data Explorer cluster.
type kustoCluster struct {
	Name string `json:"name"`
	URI  string `json:"uri"`
	// State must be "Running" before a datasource can query it.
	State string `json:"state"`
	// PublicNetworkAccess of "Disabled" means Grafana Cloud can only reach the
	// cluster through Private Data source Connect.
	PublicNetworkAccess string `json:"publicNetworkAccess"`
}

// listKustoClusters discovers ADX clusters. It goes through the generic
// `az resource` commands rather than `az kusto`, which needs an az extension
// and whose list verb requires a resource group in current az releases.
func (a azCLI) listKustoClusters(ctx context.Context, subscription string) ([]kustoCluster, error) {
	var refs []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := a.run(ctx, &refs, "resource", "list",
		"--resource-type", "Microsoft.Kusto/clusters", "--subscription", subscription); err != nil {
		return nil, err
	}

	clusters := make([]kustoCluster, 0, len(refs))
	for _, ref := range refs {
		var detail struct {
			Properties struct {
				URI                 string `json:"uri"`
				State               string `json:"state"`
				PublicNetworkAccess string `json:"publicNetworkAccess"`
			} `json:"properties"`
		}
		if err := a.run(ctx, &detail, "resource", "show", "--ids", ref.ID); err != nil {
			continue
		}
		clusters = append(clusters, kustoCluster{
			Name:                ref.Name,
			URI:                 detail.Properties.URI,
			State:               detail.Properties.State,
			PublicNetworkAccess: detail.Properties.PublicNetworkAccess,
		})
	}
	return clusters, nil
}

// servicePrincipal is the credential minted for one datasource.
type servicePrincipal struct {
	AppID       string
	ObjectID    string
	DisplayName string
	TenantID    string
	Password    string
}

// createServicePrincipal mints an app registration, its service principal, and
// a client secret, then binds the requested roles at the given scope.
func (a azCLI) createServicePrincipal(ctx context.Context, displayName string, roles []string, scope string, expiryYears int) (*servicePrincipal, error) {
	var app struct {
		AppID string `json:"appId"`
		ID    string `json:"id"`
	}
	if err := a.run(ctx, &app, "ad", "app", "create", "--display-name", displayName, "--sign-in-audience", "AzureADMyOrg"); err != nil {
		return nil, fmt.Errorf("creating app registration %q: %w", displayName, err)
	}

	if err := a.run(ctx, nil, "ad", "sp", "create", "--id", app.AppID); err != nil {
		return nil, fmt.Errorf("creating service principal for %s: %w", app.AppID, err)
	}

	var cred struct {
		Password string `json:"password"`
		Tenant   string `json:"tenant"`
	}
	if err := a.run(ctx, &cred, "ad", "app", "credential", "reset", "--id", app.AppID,
		"--years", fmt.Sprint(expiryYears), "--display-name", "gcx-ext-azure"); err != nil {
		return nil, fmt.Errorf("minting client secret for %s: %w", app.AppID, err)
	}

	for _, role := range roles {
		if err := a.assignRole(ctx, app.AppID, role, scope); err != nil {
			return nil, err
		}
	}

	return &servicePrincipal{
		AppID:       app.AppID,
		ObjectID:    app.ID,
		DisplayName: displayName,
		TenantID:    cred.Tenant,
		Password:    cred.Password,
	}, nil
}

// assignRole binds a role, retrying while Entra ID replicates the new principal.
func (a azCLI) assignRole(ctx context.Context, appID, role, scope string) error {
	var lastErr error
	for attempt := 0; attempt < roleAssignRetries; attempt++ {
		err := a.run(ctx, nil, "role", "assignment", "create",
			"--assignee", appID, "--role", role, "--scope", scope,
			"--description", "Created by the gcx azure-datasources extension")
		if err == nil {
			return nil
		}
		lastErr = err
		if !sleepCtx(ctx, roleAssignBackoff) {
			break
		}
	}
	return fmt.Errorf("assigning role %q at %s: %w", role, scope, lastErr)
}

// listOwnedApps returns app registrations this extension created, matched by
// the name prefix it stamps on everything it mints.
func (a azCLI) listOwnedApps(ctx context.Context) ([]servicePrincipal, error) {
	var apps []struct {
		AppID       string `json:"appId"`
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	}
	if err := a.run(ctx, &apps, "ad", "app", "list", "--filter",
		fmt.Sprintf("startswith(displayName,'%s')", artifactPrefix)); err != nil {
		return nil, err
	}
	out := make([]servicePrincipal, 0, len(apps))
	for _, app := range apps {
		out = append(out, servicePrincipal{AppID: app.AppID, ObjectID: app.ID, DisplayName: app.DisplayName})
	}
	return out, nil
}

func (a azCLI) deleteApp(ctx context.Context, appID string) error {
	return a.run(ctx, nil, "ad", "app", "delete", "--id", appID)
}
