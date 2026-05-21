package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/loki"
	"github.com/grafana/gcx/internal/signals"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(signals.DatasourceProvider(
		"loki",
		"Query Loki datasources",
		loki.QueryCmd,
		loki.MetricsCmd,
		loki.LabelsCmd,
		loki.SeriesCmd,
	))
}
