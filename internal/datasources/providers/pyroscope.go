package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/pyroscope"
	"github.com/grafana/gcx/internal/signals"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(signals.DatasourceProvider(
		"pyroscope",
		"Query Pyroscope datasources",
		pyroscope.QueryCmd,
		pyroscope.LabelsCmd,
		pyroscope.ListProfileTypesCmd,
		pyroscope.MetricsCmd,
		pyroscope.ExemplarsCmd,
	))
}
