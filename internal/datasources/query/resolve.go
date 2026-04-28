package query

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/grafana-app-sdk/logging"
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

// DatasourceResolution describes the outcome of datasource resolution.
type DatasourceResolution struct {
	UID     string
	Persist bool
}

// DatasourceSaver persists an inferred datasource UID into config.
type DatasourceSaver interface {
	SaveDatasourceUID(ctx context.Context, kind, uid string) error
}

// ResolveDatasource resolves a datasource UID from the -d flag, config fallback,
// or auto-discovery from Grafana when a single matching datasource is visible.
//
// Resolution order:
//  1. Explicit -d/--datasource flag.
//  2. Context config via datasources.<kind> (or legacy default-*-datasource keys).
//  3. Auto-discovery via /api/datasources when Grafana exposes a single matching
//     datasource kind, or a canonical Grafana Cloud datasource name for the
//     configured stack slug (from cloud.stack, GRAFANA_CLOUD_STACK, or
//     grafana.server-derived cloud context).
func ResolveDatasource(ctx context.Context, flagValue string, cfgCtx *config.Context, restCfg config.NamespacedRESTConfig, kind string) (DatasourceResolution, error) {
	if uid, err := ResolveDatasourceFlag(flagValue, cfgCtx, kind); err == nil {
		return DatasourceResolution{UID: uid}, nil
	}

	stackSlug := configuredCloudStack(cfgCtx)

	return discoverDatasourceUID(ctx, restCfg, kind, stackSlug)
}

// ResolveAndSaveDatasource resolves a datasource UID and best-effort persists it
// to config when the resolver inferred it from cloud.stack.
func ResolveAndSaveDatasource(ctx context.Context, saver DatasourceSaver, flagValue string, cfgCtx *config.Context, restCfg config.NamespacedRESTConfig, kind string) (string, error) {
	resolved, err := ResolveDatasource(ctx, flagValue, cfgCtx, restCfg, kind)
	if err != nil {
		return "", err
	}

	if resolved.Persist && saver != nil {
		if saveErr := saver.SaveDatasourceUID(ctx, kind, resolved.UID); saveErr != nil {
			logging.FromContext(ctx).Warn(
				"could not save discovered datasource UID to config",
				slog.String("datasource_kind", kind),
				slog.String("uid", resolved.UID),
				slog.String("error", saveErr.Error()),
			)
		}
	}

	return resolved.UID, nil
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
	case "yesoreyeram-infinity-datasource":
		return "infinity"
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

func discoverDatasourceUID(ctx context.Context, restCfg config.NamespacedRESTConfig, kind, stackSlug string) (DatasourceResolution, error) {
	dsClient, err := datasources.NewClient(restCfg)
	if err != nil {
		return DatasourceResolution{}, fmt.Errorf("could not auto-discover %s datasource: failed to create datasource client: %w; use -d flag or set datasources.%s in config", kind, err, kind)
	}

	allDatasources, err := dsClient.List(ctx)
	if err != nil {
		return DatasourceResolution{}, fmt.Errorf("could not auto-discover %s datasource: %w; use -d flag or set datasources.%s in config", kind, err, kind)
	}

	matches := matchingDatasources(allDatasources, kind)
	switch len(matches) {
	case 0:
		return DatasourceResolution{}, fmt.Errorf("no %s datasource found in Grafana: use -d flag or set datasources.%s in config", kind, kind)
	case 1:
		return DatasourceResolution{UID: matches[0].UID}, nil
	}

	if stackSlug == "" {
		return DatasourceResolution{}, fmt.Errorf("multiple %s datasources found (%s): use -d flag or set datasources.%s in config; set cloud.stack or grafana.server to enable auto-discovery", kind, formatDatasourceChoices(matches), kind)
	}

	if canonical := canonicalCloudDatasource(matches, kind, stackSlug); canonical != nil {
		return DatasourceResolution{UID: canonical.UID, Persist: true}, nil
	}

	return DatasourceResolution{}, fmt.Errorf("multiple %s datasources found (%s): use -d flag or set datasources.%s in config", kind, formatDatasourceChoices(matches), kind)
}

func configuredCloudStack(cfgCtx *config.Context) string {
	if cfgCtx != nil {
		if slug := strings.TrimSpace(cfgCtx.ResolveStackSlug()); slug != "" {
			return slug
		}
	}

	var fallback config.Context
	fallback.Cloud = &config.CloudConfig{}
	fallback.Grafana = &config.GrafanaConfig{}
	_ = env.Parse(&fallback)
	return strings.TrimSpace(fallback.ResolveStackSlug())
}

func canonicalCloudDatasource(matches []*datasources.Datasource, kind, stackSlug string) *datasources.Datasource {
	if stackSlug == "" {
		return nil
	}

	targetName := canonicalCloudDatasourceName(kind, stackSlug)
	if targetName == "" {
		return nil
	}

	for _, ds := range matches {
		if ds.Name == targetName {
			return ds
		}
	}

	return nil
}

func canonicalCloudDatasourceName(kind, stackSlug string) string {
	suffix := canonicalCloudDatasourceSuffix(kind)
	if suffix == "" {
		return ""
	}
	return fmt.Sprintf("grafanacloud-%s-%s", stackSlug, suffix)
}

func canonicalCloudDatasourceSuffix(kind string) string {
	switch kind {
	case "prometheus":
		return "prom"
	case "loki":
		return "logs"
	case "tempo":
		return "traces"
	case "pyroscope":
		return "profiles"
	default:
		return ""
	}
}

func matchingDatasources(all []*datasources.Datasource, kind string) []*datasources.Datasource {
	matches := make([]*datasources.Datasource, 0)
	for _, ds := range all {
		if NormalizeKind(ds.Type) == kind {
			matches = append(matches, ds)
		}
	}
	return matches
}

func formatDatasourceChoices(datasources []*datasources.Datasource) string {
	choices := make([]string, 0, len(datasources))
	for _, ds := range datasources {
		label := ds.Name
		if label == "" {
			label = ds.UID
		}
		if ds.UID != "" && ds.UID != label {
			label = fmt.Sprintf("%s (%s)", label, ds.UID)
		}
		choices = append(choices, label)
	}
	sort.Strings(choices)
	return strings.Join(choices, ", ")
}
