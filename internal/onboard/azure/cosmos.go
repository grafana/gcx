package azure

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/datasources"
)

// cosmosSpec provisions the Azure Cosmos DB datasource (Enterprise plugin).
// Cosmos DB does not support service-principal auth, so gcx does not mint an
// app registration; it fetches the account's primary key instead. The Grafana
// instance must have the grafana-azurecosmosdb-datasource Enterprise plugin
// installed for the created datasource to function.
type cosmosSpec struct{}

func (cosmosSpec) Token() string             { return TokenCosmos }
func (cosmosSpec) Kind() string              { return KindCosmos }
func (cosmosSpec) RoleOptions() []RoleOption { return nil }

func (s cosmosSpec) AcquireAndBuild(ctx context.Context, in SpecInput) (Provisioned, error) {
	rg := in.Extra["rg"]
	account := in.Extra["account"]
	endpoint := in.Extra["endpoint"]
	if rg == "" || account == "" {
		return Provisioned{}, fmt.Errorf("cosmos: missing account/resource-group for %q", in.Name)
	}

	key, err := in.CLI.CosmosPrimaryKey(ctx, rg, account)
	if err != nil {
		return Provisioned{}, err
	}

	return Provisioned{Request: s.payload(in.Name, endpoint, key)}, nil
}

func (cosmosSpec) payload(name, endpoint, key string) datasources.Datasource {
	return datasources.Datasource{
		Name:   name,
		Type:   KindCosmos,
		Access: "proxy",
		JSONData: map[string]any{
			"accountEndpoint": endpoint,
		},
		SecureJSONData: map[string]string{
			"accountKey": key,
		},
	}
}
