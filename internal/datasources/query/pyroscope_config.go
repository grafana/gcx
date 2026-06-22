package query

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/grafana-app-sdk/logging"
)

const defaultPyroscopeMinStep = 15 * time.Second

// GetPyroscopeMinStep fetches the datasource by UID and reads jsonData.minStep.
// It defaults to 15s when unset or invalid, matching Grafana's Pyroscope backend.
func GetPyroscopeMinStep(ctx context.Context, cfg config.NamespacedRESTConfig, uid string) (time.Duration, error) {
	dsClient, err := datasources.NewClient(cfg)
	if err != nil {
		return 0, fmt.Errorf("failed to create datasource client: %w", err)
	}

	ds, err := dsClient.GetByUID(ctx, uid)
	if err != nil {
		return 0, fmt.Errorf("failed to get datasource %q: %w", uid, err)
	}

	jsonData, ok := ds.JSONData.(map[string]any)
	if !ok || jsonData == nil {
		return defaultPyroscopeMinStep, nil
	}

	minStep, _ := jsonData["minStep"].(string)
	if minStep == "" {
		return defaultPyroscopeMinStep, nil
	}

	parsed, _ := ParseDuration(minStep)
	if parsed <= 0 {
		logging.FromContext(ctx).Warn(
			"invalid Pyroscope datasource minStep; using default",
			slog.String("datasource_uid", uid),
			slog.String("min_step", minStep),
			slog.Duration("default_min_step", defaultPyroscopeMinStep),
		)
		return defaultPyroscopeMinStep, nil
	}

	return parsed, nil
}
