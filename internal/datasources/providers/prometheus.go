package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/prometheus"
	"github.com/grafana/gcx/internal/signals"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(signals.DatasourceProvider(
		"prometheus",
		"Query Prometheus datasources",
		prometheus.QueryCmd,
		prometheus.LabelsCmd,
		prometheus.MetadataCmd,
	))
}
