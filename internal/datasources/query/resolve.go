package query

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
)

// ResolveDatasourceFlag resolves a datasource UID from the -d flag value or config fallback.
// If flagValue is non-empty it is returned directly. Otherwise the UID is looked up
// from cfgCtx (the current context's datasources.<kind> config key). cfgCtx may be nil
// when config loading failed. If neither flag nor config provides a UID, an error is
// returned mentioning both the -d flag and the config key.
func ResolveDatasourceFlag(flagValue string, cfgCtx *config.Context, kind string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}

	if cfgCtx != nil {
		if uid := config.DefaultDatasourceUID(*cfgCtx, kind); uid != "" {
			return uid, nil
		}
	}

	return "", fmt.Errorf("datasource UID is required: use -d flag or set datasources.%s in config", kind)
}

// ResolveTypedArgs parses positional args for typed subcommands.
// Typed subcommands accept: [DATASOURCE_UID] EXPR
// If only one arg is provided, it is EXPR and DATASOURCE_UID is resolved from defaultUID.
// If two args are provided, arg[0] is DATASOURCE_UID and arg[1] is EXPR.
func ResolveTypedArgs(args []string, defaultUID string, kind string) (string, string, error) {
	switch len(args) {
	case 0:
		return "", "", errors.New("EXPR is required")
	case 1:
		// No UID provided -- use the pre-resolved default.
		if defaultUID == "" {
			return "", "", fmt.Errorf("DATASOURCE_UID is required: provide it as the first positional argument or configure datasources.%s in your context", kind)
		}
		return defaultUID, args[0], nil
	case 2:
		return args[0], args[1], nil
	default:
		return "", "", errors.New("too many arguments: expected [DATASOURCE_UID] EXPR")
	}
}

// NormalizeKind converts a Grafana datasource plugin ID to its short kind name.
// Some plugins use the short name directly (e.g., "prometheus"), while others
// use a longer ID (e.g., "grafana-pyroscope-datasource").
// If the plugin ID is not recognized, it is returned as-is.
func NormalizeKind(pluginID string) string {
	switch pluginID {
	case "prometheus", "loki", "tempo":
		return pluginID
	case "grafana-pyroscope-datasource":
		return "pyroscope"
	default:
		return pluginID
	}
}

// ValidateDatasourceType checks that the datasource's actual type matches the expected kind.
func ValidateDatasourceType(actualType, expectedKind string) error {
	if NormalizeKind(actualType) != expectedKind {
		return fmt.Errorf("datasource is type %s, not %s", actualType, expectedKind)
	}
	return nil
}

// GetDatasourceType fetches datasource type from the API.
func GetDatasourceType(ctx context.Context, cfg config.NamespacedRESTConfig, uid string) (string, error) {
	dsClient, err := datasources.NewClient(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to create datasource client: %w", err)
	}

	ds, err := dsClient.GetByUID(ctx, uid)
	if err != nil {
		return "", fmt.Errorf("failed to get datasource %q: %w", uid, err)
	}

	return ds.Type, nil
}
