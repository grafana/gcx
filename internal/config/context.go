package config

import "context"

// configContextKey is a private key type for storing the Grafana config context
// name in a Go context.Context. Using a named type prevents collisions with
// other packages that also use context.WithValue.
type configContextKey struct{}

// configFileContextKey is a private key type for storing an explicitly
// selected config file in a Go context.Context. Resource adapter factories are
// created independently from Cobra flag binding, so the context is the
// immutable hand-off between the command that owns --config and every lazy
// provider loader it invokes.
type configFileContextKey struct{}

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

// ContextWithConfigFile attaches an explicitly selected config file to a Go
// context. The selection is intentionally a value, rather than a mutation of a
// shared provider loader, so concurrent adapter factories cannot leak config
// selection across invocations.
func ContextWithConfigFile(ctx context.Context, path string) context.Context {
	if path == "" {
		return ctx
	}
	return context.WithValue(ctx, configFileContextKey{}, path)
}

// ConfigFileFromCtx retrieves the explicitly selected config file from a Go
// context. It returns "" when the command did not select one, allowing loaders
// to retain their existing GCX_CONFIG and layered-discovery behavior.
func ConfigFileFromCtx(ctx context.Context) string {
	if path, ok := ctx.Value(configFileContextKey{}).(string); ok {
		return path
	}
	return ""
}
