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
}

// IsRunning reports whether the cluster is in a state that supports
// provisioning (role/principal assignment and live queries).
func (c KustoCluster) IsRunning() bool {
	return strings.EqualFold(c.State, "Running")
}

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
}

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
