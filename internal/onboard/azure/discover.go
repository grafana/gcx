package azure

import (
	"context"
	"strings"
)

// KustoCluster is a discovered Azure Data Explorer cluster.
type KustoCluster struct {
	Name string `json:"name"`
	URI  string `json:"uri"`
	RG   string `json:"resourceGroup"`
	// State is the cluster runtime state (e.g. "Running", "Stopped",
	// "Starting"). Provisioning requires a running cluster.
	State string `json:"state"`
	// PublicNetworkAccess is the cluster's public network access setting
	// ("Enabled"/"Disabled"). "Disabled" means Grafana Cloud can only reach the
	// cluster via Private Data source Connect (PDC).
	PublicNetworkAccess string `json:"publicNetworkAccess"`
}

// IsRunning reports whether the cluster is in a state that supports
// provisioning (role/principal assignment and live queries).
func (c KustoCluster) IsRunning() bool {
	return strings.EqualFold(c.State, "Running")
}

// isPrivate reports whether an Azure resource's publicNetworkAccess value
// indicates the resource is not publicly reachable. An empty value is treated
// as public (the common default) so we never emit a spurious PDC hint.
func isPrivate(publicNetworkAccess string) bool {
	return strings.EqualFold(publicNetworkAccess, "Disabled")
}

// IsPrivate reports whether the cluster has public network access disabled.
func (c KustoCluster) IsPrivate() bool { return isPrivate(c.PublicNetworkAccess) }

// ListKustoClusters discovers ADX clusters via `az kusto cluster list`. The
// command requires the `kusto` az extension (auto-installed by az on first use)
// and Reader access; callers should treat an error as "no ADX clusters".
func (c *CLI) ListKustoClusters(ctx context.Context) ([]KustoCluster, error) {
	var clusters []KustoCluster
	if err := c.tool.RunJSON(ctx, &clusters, "kusto", "cluster", "list", "--only-show-errors", "-o", "json"); err != nil {
		return nil, err
	}
	return clusters, nil
}

// CosmosAccount is a discovered Azure Cosmos DB account.
type CosmosAccount struct {
	Name             string `json:"name"`
	RG               string `json:"resourceGroup"`
	DocumentEndpoint string `json:"documentEndpoint"`
	// PublicNetworkAccess is the account's public network access setting
	// ("Enabled"/"Disabled"). "Disabled" means Grafana Cloud can only reach the
	// account via Private Data source Connect (PDC).
	PublicNetworkAccess string `json:"publicNetworkAccess"`
}

// IsPrivate reports whether the account has public network access disabled.
func (a CosmosAccount) IsPrivate() bool { return isPrivate(a.PublicNetworkAccess) }

// ListCosmosAccounts discovers Cosmos DB accounts via `az cosmosdb list`.
func (c *CLI) ListCosmosAccounts(ctx context.Context) ([]CosmosAccount, error) {
	var accounts []CosmosAccount
	if err := c.tool.RunJSON(ctx, &accounts, "cosmosdb", "list", "--only-show-errors", "-o", "json"); err != nil {
		return nil, err
	}
	return accounts, nil
}

// CosmosPrimaryKey fetches the primary master key for a Cosmos account.
func (c *CLI) CosmosPrimaryKey(ctx context.Context, rg, account string) (string, error) {
	var keys struct {
		PrimaryMasterKey string `json:"primaryMasterKey"`
	}
	if err := c.tool.RunJSON(ctx, &keys, "cosmosdb", "keys", "list",
		"--name", account, "--resource-group", rg, "--type", "keys",
		"--only-show-errors", "-o", "json"); err != nil {
		return "", classifyAuthErr(err)
	}
	return keys.PrimaryMasterKey, nil
}
