package irm

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &IRMProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&IRMProvider{})
}

// IRMProvider manages Grafana IRM resources (OnCall + Incidents).
type IRMProvider struct{}

func (p *IRMProvider) Name() string      { return "irm" }
func (p *IRMProvider) ShortDesc() string { return "Manage Grafana IRM (OnCall + Incidents)" }

func (p *IRMProvider) Commands() []*cobra.Command {
	return nil
}

func (p *IRMProvider) Validate(_ map[string]string) error        { return nil }
func (p *IRMProvider) ConfigKeys() []providers.ConfigKey          { return nil }
func (p *IRMProvider) TypedRegistrations() []adapter.Registration { return nil }
