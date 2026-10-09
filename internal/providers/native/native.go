// Package native binds provider commands to Kubernetes-compatible resources
// that Grafana serves natively and gcx discovers from the server.
//
// A command binds a [Config] once (no I/O) and calls [Binding.Load] only
// after validation and any confirmation. Load resolves a fresh config
// snapshot, the resource descriptor, and a dynamic client every time; nothing
// is cached between calls.
//
// The package has no direct CLI imports (no cobra, output, terminal, or prompt
// packages; TestNoCLIImports enforces this), so other agent-facing surfaces
// can reuse it.
package native

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Client is the dynamic-client subset the router already uses for native
// resources; *dynamic.NamespacedClient satisfies it.
type Client = adapter.DynamicClient

// ConfigLoader resolves the active Grafana connection.
// providers.GrafanaConfigLoader satisfies it; it is redeclared here so the
// package does not import providers (which pulls in CLI packages).
type ConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// Config identifies the native resource a binding serves.
type Config struct {
	Group    string // e.g. "notifications.alerting.grafana.app"
	Resource string // e.g. "routingtrees"
}

// LoadOptions selects the API version for a single Load.
type LoadOptions struct {
	// APIVersion is "group/version" or "version". Empty selects the server's
	// preferred version.
	APIVersion string
}

// Access is everything a command needs to call the native API.
type Access struct {
	Client     Client
	Descriptor resources.Descriptor
	// Config is the same snapshot the client was built from, for helper
	// queries and deep links.
	Config config.NamespacedRESTConfig
}

// RegistryFunc builds a discovery registry for a config snapshot.
type RegistryFunc func(ctx context.Context, cfg config.NamespacedRESTConfig) (*discovery.Registry, error)

// BindOption customizes a binding.
type BindOption func(*bindOptions)

type bindOptions struct {
	registry RegistryFunc
}

// WithRegistry replaces the default disk-cached discovery registry, e.g. so a
// long-running multi-tenant process can supply its own cache.
func WithRegistry(f RegistryFunc) BindOption {
	return func(o *bindOptions) { o.registry = f }
}

// Binding resolves [Access] on demand.
type Binding struct {
	load func(ctx context.Context, o LoadOptions) (Access, error)
}

// Bind binds a resource to a config loader. It does no I/O.
func Bind(loader ConfigLoader, cfg Config, opts ...BindOption) Binding {
	bo := bindOptions{registry: discovery.NewDefaultRegistry}
	for _, opt := range opts {
		opt(&bo)
	}

	return Binding{load: func(ctx context.Context, o LoadOptions) (Access, error) {
		// Validate the version before any I/O.
		version, err := ParseAPIVersion(cfg.Group, o.APIVersion)
		if err != nil {
			return Access{}, err
		}

		restCfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return Access{}, err
		}

		reg, err := bo.registry(ctx, restCfg)
		if err != nil {
			return Access{}, fmt.Errorf("discovery failed: %w", err)
		}

		desc, err := resolveDescriptor(reg, cfg, version)
		if err != nil {
			return Access{}, err
		}

		client, err := dynamic.NewDefaultNamespacedClient(restCfg)
		if err != nil {
			return Access{}, err
		}

		return Access{Client: client, Descriptor: desc, Config: restCfg}, nil
	}}
}

// Fixed returns a binding whose Load returns the given Access without I/O. It is
// a test seam.
func Fixed(a Access) Binding {
	return Func(func(context.Context, LoadOptions) (Access, error) { return a, nil })
}

// Func returns a binding backed by f. It is a test seam for asserting the
// options a command passes to Load, or for injecting a Load error.
func Func(f func(ctx context.Context, o LoadOptions) (Access, error)) Binding {
	return Binding{load: f}
}

// Load resolves a fresh config snapshot, descriptor, and client.
func (b Binding) Load(ctx context.Context, o LoadOptions) (Access, error) {
	if b.load == nil {
		return Access{}, errors.New("native: binding not initialized")
	}
	return b.load(ctx, o)
}

// ParseAPIVersion validates apiVersion ("group/version" or "version") against
// group and returns the version. An empty apiVersion returns "" (server
// preferred). A mismatched group is an error.
func ParseAPIVersion(group, apiVersion string) (string, error) {
	if apiVersion == "" {
		return "", nil
	}

	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return "", fmt.Errorf("invalid API version %q: %w", apiVersion, err)
	}
	if gv.Group != "" && gv.Group != group {
		return "", fmt.Errorf("API version %q is not in group %q", apiVersion, group)
	}
	// ParseGroupVersion puts slash-less input wholly into Version, so a bare
	// group ("rules.alerting.grafana.app") would otherwise pass as a version.
	if gv.Version == "" {
		return "", fmt.Errorf("invalid API version %q: version is empty", apiVersion)
	}
	if errs := validation.IsDNS1035Label(gv.Version); len(errs) > 0 {
		return "", fmt.Errorf("invalid API version %q: %q is not a version (expected e.g. v1beta1 or %s/v1beta1)", apiVersion, gv.Version, group)
	}

	return gv.Version, nil
}

// resolveDescriptor builds the selector from parts rather than formatting and
// re-parsing a selector string, which misreads version-only input and
// multi-dot groups.
func resolveDescriptor(reg *discovery.Registry, cfg Config, version string) (resources.Descriptor, error) {
	sel := resources.Selector{
		Type: resources.FilterTypeAll,
		GroupVersionKind: resources.PartialGVK{
			Group:    cfg.Group,
			Version:  version,
			Resource: cfg.Resource,
		},
	}

	filters, err := reg.MakeFilters(discovery.MakeFiltersOptions{
		Selectors:            resources.Selectors{sel},
		PreferredVersionOnly: true,
	})
	// The registry's error is a selector error, which the CLI would render as
	// a selector-parsing failure; the caller passed no selector, so report the
	// real cause instead of wrapping it.
	if err != nil || len(filters) == 0 {
		target := cfg.Resource + "." + cfg.Group
		if version != "" {
			target = fmt.Sprintf("%s (version %s)", target, version)
		}
		return resources.Descriptor{}, fmt.Errorf("server does not serve %s", target)
	}

	return filters[0].Descriptor, nil
}
