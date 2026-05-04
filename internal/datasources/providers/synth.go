package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/synth"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&synthDSProvider{})
}

type synthDSProvider struct{}

func (p *synthDSProvider) Kind() string      { return "synth" }
func (p *synthDSProvider) ShortDesc() string { return "Query Synthetic Monitoring datasources" }

func (p *synthDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return synth.QueryCmd(loader)
}

func (p *synthDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		synth.ProbesCmd(loader),
		synth.ChecksCmd(loader),
	}
}
