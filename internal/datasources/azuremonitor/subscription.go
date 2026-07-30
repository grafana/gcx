package azuremonitor

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
)

// resolveSubscription returns the --subscription flag value, falling back to
// the datasource's configured default subscription when the flag is empty.
func resolveSubscription(ctx context.Context, cfg config.NamespacedRESTConfig, datasourceUID, flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	subscription, err := dsquery.GetAzureMonitorDefaultSubscription(ctx, cfg, datasourceUID)
	if err != nil {
		return "", fmt.Errorf("failed to read the datasource's default subscription: %w", err)
	}
	if subscription == "" {
		return "", errors.New("--subscription is required (the datasource has no default subscription configured)")
	}
	return subscription, nil
}
