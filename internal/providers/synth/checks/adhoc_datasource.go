package checks

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
)

// resolveLogsDataSourceUID resolves the Loki datasource UID used to poll
// ad-hoc check results, using the same four-tier resolution as
// resolveDataSourceUID (Prometheus): explicit flag, shared config resolver
// (datasources.loki), SM provider cache (sm-logs-datasource-uid), then
// auto-discovery via SM plugin settings, cached for next run.
func resolveLogsDataSourceUID(ctx context.Context, flagUID string, loader smcfg.AdHocLoader) (string, error) {
	return resolveTieredDatasourceUID(ctx, flagUID, loader, tieredDatasourceConfig{
		Kind:     "loki",
		FlagName: "--logs-datasource-uid",
		CacheKey: "sm-logs-datasource-uid",
		Discover: discoverLogsDatasource,
		Save:     loader.SaveLogsDatasourceUID,
	})
}

// discoverLogsDatasource queries the Grafana SM plugin settings to find the
// Loki datasource configured for Synthetic Monitoring logs.
func discoverLogsDatasource(ctx context.Context, restCfg config.NamespacedRESTConfig) (string, error) {
	dsName, err := smLogsDatasourceName(ctx, restCfg)
	if err != nil {
		return "", fmt.Errorf(
			"could not auto-discover SM logs datasource: %w; use --logs-datasource-uid or set contexts.<name>.datasources.loki in config",
			err)
	}

	dsClient, err := datasources.NewClient(restCfg)
	if err != nil {
		return "", errors.New(
			"logs datasource UID is required: use --logs-datasource-uid flag or set contexts.<name>.datasources.loki in config")
	}
	ds, err := dsClient.GetByName(ctx, dsName)
	if err != nil {
		return "", fmt.Errorf(
			"SM logs datasource %q not found in Grafana: %w; use --logs-datasource-uid or set contexts.<name>.datasources.loki in config",
			dsName, err)
	}

	return ds.UID, nil
}

// smLogsDatasourceName queries the grafana-synthetic-monitoring-app plugin settings
// and returns the configured logs datasource name (jsonData.logs.grafanaName).
func smLogsDatasourceName(ctx context.Context, restCfg config.NamespacedRESTConfig) (string, error) {
	return smPluginDatasourceName(ctx, restCfg, "logs")
}
