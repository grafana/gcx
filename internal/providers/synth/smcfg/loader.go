// Package smcfg defines the shared config loader interface for the synth provider.
package smcfg

import (
	"context"

	"github.com/grafana/gcx/internal/config"
)

// Loader can load SM credentials and the current namespace from config.
//
// LoadSMConfig resolves the direct SM-API base URL + token (the fallback
// transport). LoadSMProxyConfig resolves the Grafana REST config + SM
// datasource UID for the datasource-proxy transport (the primary path); it
// returns an empty UID — not an error — when no SM datasource can be resolved,
// so the typed clients degrade to the direct API.
type Loader interface {
	LoadSMConfig(ctx context.Context) (baseURL, token, namespace string, err error)
	LoadSMProxyConfig(ctx context.Context) (restCfg config.NamespacedRESTConfig, datasourceUID, namespace string, err error)
}

// GrafanaConfigLoader can load a Grafana REST config for Prometheus queries.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// ConfigLoader can load the full config for datasource discovery.
type ConfigLoader interface {
	LoadConfig(ctx context.Context) (*config.Config, error)
}

// DatasourceUIDSaver can persist a discovered Prometheus datasource UID to the SM provider config.
type DatasourceUIDSaver interface {
	SaveMetricsDatasourceUID(ctx context.Context, uid string) error
}

// StatusLoader combines SM config loading with Grafana REST config and full config loading.
// Used by status/timeline commands that need SM API + Prometheus + datasource discovery.
type StatusLoader interface {
	Loader
	GrafanaConfigLoader
	ConfigLoader
	DatasourceUIDSaver
}
