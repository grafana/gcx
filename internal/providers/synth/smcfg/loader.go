// Package smcfg defines the shared config loader interface for the synth provider.
package smcfg

import (
	"context"

	"github.com/grafana/gcx/internal/config"
)

// Loader resolves the Grafana REST config and SM datasource UID needed to
// route SM API calls through the datasource proxy. The returned config carries
// the caller's Grafana credentials; auth to the SM API is performed by the
// proxy using the datasource's stored credentials.
type Loader interface {
	LoadSMConfig(ctx context.Context) (cfg config.NamespacedRESTConfig, datasourceUID, namespace string, err error)
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
