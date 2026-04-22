package instrumentation

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

// commandsBuilder is set by the wire package's init() to break the import cycle:
// subpackages import this package for types, so this package cannot import them.
//
//nolint:gochecknoglobals
var commandsBuilder func() []*cobra.Command

// SetCommandsBuilder is called by the wire bootstrap package to inject the
// command-tree constructor. Must be called before Commands() is invoked.
func SetCommandsBuilder(f func() []*cobra.Command) { commandsBuilder = f }

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&InstrumentationProvider{})

	// Natural-key registration is separate from providers.Register; both are called here to keep all self-registration in one init().
	adapter.RegisterNaturalKey(ClusterGVK, ClusterNaturalKey)
	adapter.RegisterNaturalKey(AppGVK, AppNaturalKey)
}

var _ providers.Provider = &InstrumentationProvider{}

// InstrumentationProvider manages Grafana Cloud instrumentation resources.
type InstrumentationProvider struct{}

// Name returns the unique identifier for this provider.
func (p *InstrumentationProvider) Name() string { return "instrumentation" }

// ShortDesc returns a one-line description of the provider.
func (p *InstrumentationProvider) ShortDesc() string {
	return "Manage Grafana Cloud instrumentation (clusters and apps)"
}

// ConfigKeys returns the configuration keys used by this provider.
func (p *InstrumentationProvider) ConfigKeys() []providers.ConfigKey { return nil }

// Validate checks that the given provider configuration is valid.
func (p *InstrumentationProvider) Validate(_ map[string]string) error { return nil }

// Commands returns the Cobra commands contributed by this provider.
// The command tree is assembled by the wire bootstrap package to avoid an
// import cycle (subpackages depend on this package for types).
func (p *InstrumentationProvider) Commands() []*cobra.Command {
	if commandsBuilder == nil {
		panic("instrumentation: SetCommandsBuilder not called before Commands()")
	}
	return commandsBuilder()
}

// TypedRegistrations returns adapter registrations for instrumentation resource types.
func (p *InstrumentationProvider) TypedRegistrations() []adapter.Registration {
	return Registrations()
}
