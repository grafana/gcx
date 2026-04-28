package config

import "context"

// configContextKey is a private key type for storing the Grafana config context
// name in a Go context.Context. Using a named type prevents collisions with
// other packages that also use context.WithValue.
type configContextKey struct{}

// ContextWithName attaches the Grafana config context name to a Go context.
// Use this before invoking provider adapter factories so they can select the
// correct named context when loading credentials.
func ContextWithName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, configContextKey{}, name)
}

// ContextNameFromCtx retrieves the Grafana config context name from a Go context.
// Returns "" if not set, which causes loaders to fall back to the default context.
func ContextNameFromCtx(ctx context.Context) string {
	if name, ok := ctx.Value(configContextKey{}).(string); ok {
		return name
	}
	return ""
}

type namespacedRESTConfigKey struct{}

// WithNamespacedRESTConfig injects a pre-built NamespacedRESTConfig into context.
// When present, ConfigLoader.LoadGrafanaConfig will return this config directly,
// bypassing all file-based loading and env var resolution.
func WithNamespacedRESTConfig(ctx context.Context, cfg NamespacedRESTConfig) context.Context {
	return context.WithValue(ctx, namespacedRESTConfigKey{}, cfg)
}

// NamespacedRESTConfigFromContext retrieves an injected NamespacedRESTConfig.
// Returns false if no config was injected.
func NamespacedRESTConfigFromContext(ctx context.Context) (NamespacedRESTConfig, bool) {
	cfg, ok := ctx.Value(namespacedRESTConfigKey{}).(NamespacedRESTConfig)
	return cfg, ok
}
