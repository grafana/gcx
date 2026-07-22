package instrumentation

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&InstrumentationProvider{})
}

// InstrumentationProvider manages Grafana Instrumentation Hub resources via
// action-verb commands. It does NOT register any GVK with the resource adapter
// pipeline — gcx resources push/pull/schemas will not list any
// instrumentation kinds.
type InstrumentationProvider struct{}

// Name returns the unique identifier for this provider.
func (p *InstrumentationProvider) Name() string { return "instrumentation" }

// ShortDesc returns a one-line description of the provider.
func (p *InstrumentationProvider) ShortDesc() string {
	return "Manage Grafana Instrumentation Hub (clusters, apps, services)"
}

// Commands returns nil because the instrumentation command tree is wired
// directly from cmd/gcx/root via a named import. The tree is NOT
// surfaced through the provider's Commands() method to avoid a circular
// dependency between cmd/ and internal/.
func (p *InstrumentationProvider) Commands() []*cobra.Command { return nil }

// Validate checks that the given provider configuration is valid.
// The instrumentation provider derives all connection details from the
// shared fleet client configuration, so no extra provider-specific keys are required.
func (p *InstrumentationProvider) Validate(_ map[string]string) error { return nil }

// ConfigKeys returns nil — the instrumentation provider uses the shared
// fleet client configuration and does not introduce provider-specific config keys.
func (p *InstrumentationProvider) ConfigKeys() []providers.ConfigKey { return nil }

// TypedRegistrations returns nil — no GVK is registered for instrumentation
// kinds. gcx resources list-types/push/pull/delete will not include
// any instrumentation resource types.
func (p *InstrumentationProvider) TypedRegistrations() []adapter.Registration { return nil }
