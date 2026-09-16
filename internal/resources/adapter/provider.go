package adapter

import "github.com/spf13/cobra"

// ConfigKey describes a single configuration key for a provider.
//
// providers.ConfigKey (internal/providers/provider.go) is a type alias for
// this type: adapter cannot import internal/providers (providers already
// imports adapter for Registration/TypedRegistrations, so the reverse import
// would cycle), so Provider's ConfigKeys() method is defined here in terms
// of adapter.ConfigKey. Aliasing on the providers side keeps the method
// signature identical to the providers.Provider interface's, letting
// *adapter.Provider structurally satisfy it without adapter importing
// providers.
type ConfigKey struct {
	// Name is the key name as it appears in the provider's config map.
	Name string
	// Secret indicates whether the value should be redacted in output.
	Secret bool
}

// Provider is the concrete provider type built by NewProvider. It
// structurally satisfies providers.Provider (see ConfigKey's doc comment for
// why that works without an import cycle) — callers pass the returned value
// directly to providers.Register().
type Provider struct {
	name      string
	shortDesc string
	commands  func() []*cobra.Command
	regs      []Registration
}

// NewProvider builds a declarative provider from one or more Resource[T]
// declarations. loadDeps is invoked lazily — once per resource's Factory
// call — to resolve that resource's ClientDeps; every Resource declared here
// shares the same loader, mirroring how existing multi-type providers (e.g.
// OnCall) share one client loader across all their registrations.
//
// NewProvider does not auto-generate CRUD command verbs: attach a factory for
// a hand-written command tree via WithCommands.
// Construction registers declared natural keys in the global registry;
// client dependency loading remains deferred until an adapter is requested.
func NewProvider(name, shortDesc string, loadDeps DepsLoader, decls ...Declaration) *Provider {
	regs := make([]Registration, 0, len(decls))
	for _, r := range decls {
		regs = append(regs, r.registration(loadDeps))
	}
	return &Provider{name: name, shortDesc: shortDesc, regs: regs}
}

// WithCommands attaches a factory for a hand-written command tree to p and
// returns p, so it can be chained off NewProvider. The factory must return a
// fresh tree each time because Cobra commands retain their parent when added
// to a root command.
func (p *Provider) WithCommands(commands func() []*cobra.Command) *Provider {
	p.commands = commands
	return p
}

// Name returns the unique identifier for this provider.
func (p *Provider) Name() string { return p.name }

// ShortDesc returns a one-line description of the provider.
func (p *Provider) ShortDesc() string { return p.shortDesc }

// Commands returns a fresh command tree from the factory attached via
// WithCommands (nil if none).
func (p *Provider) Commands() []*cobra.Command {
	if p.commands == nil {
		return nil
	}
	return p.commands()
}

// Validate is a no-op: declarative providers built via NewProvider have no
// provider-specific config keys unless the caller adds its own validation.
func (p *Provider) Validate(map[string]string) error { return nil }

// ConfigKeys returns nil by default: declarative providers built via
// NewProvider declare no provider-specific configuration keys.
func (p *Provider) ConfigKeys() []ConfigKey { return nil }

// TypedRegistrations returns the adapter registrations built from the
// Resource[T] declarations passed to NewProvider.
func (p *Provider) TypedRegistrations() []Registration { return p.regs }
