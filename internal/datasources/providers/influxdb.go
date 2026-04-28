package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/influxdb"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&influxdbDSProvider{})
}

type influxdbDSProvider struct{}

func (p *influxdbDSProvider) Kind() string      { return "influxdb" }
func (p *influxdbDSProvider) ShortDesc() string { return "Query InfluxDB datasources" }

func (p *influxdbDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return influxdb.QueryCmd(loader)
}

func (p *influxdbDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		influxdb.MeasurementsCmd(loader),
		influxdb.FieldKeysCmd(loader),
	}
}
