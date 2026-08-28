package azuremonitor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/spf13/cobra"
)

// rejectExplicitEmptyFlag errors if flagName was explicitly passed as an
// empty or whitespace-only value, as opposed to simply being omitted (which
// falls through to the flag's documented default behavior). An unset shell
// variable interpolated into a flag (e.g. --subscription "$AZ_SUB") produces
// exactly this shape, and without this check it silently falls back to a
// different default rather than failing loudly.
func rejectExplicitEmptyFlag(cmd *cobra.Command, flagName, value string) error {
	if cmd.Flags().Changed(flagName) && strings.TrimSpace(value) == "" {
		return fmt.Errorf("--%s must not be empty", flagName)
	}
	return nil
}

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
