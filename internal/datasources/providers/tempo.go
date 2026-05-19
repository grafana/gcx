package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/tempo"
	"github.com/grafana/gcx/internal/signals"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(signals.DatasourceProvider(
		"tempo",
		"Query Tempo datasources",
		tempo.QueryCmd,
		tempo.GetCmd,
		tempo.LabelsCmd,
		tempo.MetricsCmd,
	))
}
