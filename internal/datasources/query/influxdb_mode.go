package query

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
)

// InfluxDBConfig holds the query-language configuration from a datasource's jsonData.
type InfluxDBConfig struct {
	Mode          string // "InfluxQL" (default), "Flux", or "SQL"
	DefaultBucket string // Flux defaultBucket from jsonData, may be empty
}

// GetInfluxDBConfig fetches the datasource by UID and reads jsonData to determine
// the query language mode and default bucket.
// Mode defaults to "InfluxQL" if not set or unrecognized.
func GetInfluxDBConfig(ctx context.Context, cfg config.NamespacedRESTConfig, uid string) (InfluxDBConfig, error) {
	dsClient, err := datasources.NewClient(cfg)
	if err != nil {
		return InfluxDBConfig{Mode: "InfluxQL"}, fmt.Errorf("failed to create datasource client: %w", err)
	}

	ds, err := dsClient.GetByUID(ctx, uid)
	if err != nil {
		return InfluxDBConfig{Mode: "InfluxQL"}, fmt.Errorf("failed to get datasource %q: %w", uid, err)
	}

	jsonData, ok := ds.JSONData.(map[string]any)
	if !ok || jsonData == nil {
		return InfluxDBConfig{Mode: "InfluxQL"}, nil
	}

	version, _ := jsonData["version"].(string)
	defaultBucket, _ := jsonData["defaultBucket"].(string)

	mode := "InfluxQL"
	switch version {
	case "Flux", "SQL":
		mode = version
	}

	return InfluxDBConfig{Mode: mode, DefaultBucket: defaultBucket}, nil
}

// GetInfluxDBMode fetches the datasource by UID and reads jsonData.version
// to determine the InfluxDB query language mode.
// Returns "InfluxQL" as the default if version is not set or unrecognized.
func GetInfluxDBMode(ctx context.Context, cfg config.NamespacedRESTConfig, uid string) (string, error) {
	influxCfg, err := GetInfluxDBConfig(ctx, cfg, uid)
	return influxCfg.Mode, err
}
