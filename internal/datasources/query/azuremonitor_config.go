package query

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
)

// GetAzureMonitorDefaultSubscription fetches the datasource by UID and reads
// the default subscription configured in its jsonData. Returns an empty
// string when the datasource has no default subscription set.
func GetAzureMonitorDefaultSubscription(ctx context.Context, cfg config.NamespacedRESTConfig, uid string) (string, error) {
	dsClient, err := datasources.NewClient(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to create datasource client: %w", err)
	}

	ds, err := dsClient.GetByUID(ctx, uid)
	if err != nil {
		return "", fmt.Errorf("failed to get datasource %q: %w", uid, err)
	}

	if ds.JSONData == nil {
		return "", nil
	}

	subscription, _ := ds.JSONData["subscriptionId"].(string)
	return subscription, nil
}
