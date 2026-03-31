// Package adaptive provides the gcx adaptive telemetry provider, covering
// Adaptive Metrics, Adaptive Logs, and Adaptive Traces.
package adaptive

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/adaptive/logs"
	"github.com/grafana/gcx/internal/providers/adaptive/metrics"
	"github.com/grafana/gcx/internal/providers/adaptive/traces"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&Provider{})
}

var _ providers.Provider = &Provider{}

// Provider manages Grafana Cloud Adaptive Telemetry resources.
type Provider struct{}

func (p *Provider) Name() string { return "adaptive" }

func (p *Provider) ShortDesc() string {
	return "Manage Grafana Cloud Adaptive Telemetry."
}

func (p *Provider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	adaptiveCmd := &cobra.Command{
		Use:   "adaptive",
		Short: p.ShortDesc(),
	}

	loader.BindFlags(adaptiveCmd.PersistentFlags())

	adaptiveCmd.AddCommand(
		metrics.Commands(loader),
		logs.Commands(loader),
		traces.Commands(loader),
	)

	return []*cobra.Command{adaptiveCmd}
}

func (p *Provider) Validate(_ map[string]string) error {
	return nil
}

func (p *Provider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "metrics-tenant-id", Secret: false},
		{Name: "metrics-tenant-url", Secret: false},
		{Name: "logs-tenant-id", Secret: false},
		{Name: "logs-tenant-url", Secret: false},
		{Name: "traces-tenant-id", Secret: false},
		{Name: "traces-tenant-url", Secret: false},
	}
}

func (p *Provider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    logs.NewExemptionAdapterFactory(loader),
			Descriptor: logs.ExemptionDescriptor(),
			GVK:        logs.ExemptionDescriptor().GroupVersionKind(),
			Schema:     logs.ExemptionSchema(),
			Example:    logs.ExemptionExample(),
		},
		{
			Factory:    traces.NewPolicyAdapterFactory(loader),
			Descriptor: traces.PolicyDescriptor(),
			GVK:        traces.PolicyDescriptor().GroupVersionKind(),
			Schema:     traces.PolicySchema(),
			Example:    traces.PolicyExample(),
		},
	}
}
