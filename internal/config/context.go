package config

import (
	"context"
	"io"
	"sync"
)

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

// warningWriterContextKey carries the command's diagnostic stream into the
// config library. Keeping the writer request-scoped avoids process-global
// output while allowing operational warnings that must be visible at the
// default log level to stay on stderr rather than corrupting command output.
type warningWriterContextKey struct{}

type ignoredLocalKeychainWarningLatchKey struct{}

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

// ContextWithWarningWriter attaches the command's diagnostic stream to ctx.
// Config loading uses it only for actionable warnings that must be visible at
// the default log level. A nil writer leaves the context unchanged.
func ContextWithWarningWriter(ctx context.Context, writer io.Writer) context.Context {
	if writer == nil {
		return ctx
	}
	if _, ok := ctx.Value(ignoredLocalKeychainWarningLatchKey{}).(*sync.Once); !ok {
		ctx = context.WithValue(ctx, ignoredLocalKeychainWarningLatchKey{}, &sync.Once{})
	}
	return context.WithValue(ctx, warningWriterContextKey{}, writer)
}

func warningWriterFromCtx(ctx context.Context) io.Writer {
	writer, _ := ctx.Value(warningWriterContextKey{}).(io.Writer)
	return writer
}

// ignoredLocalKeychainWarningLatchFallback backs the "ignored local keychain
// policy" warning when no context-scoped latch was installed. Installing the
// latch is normally a side effect of ContextWithWarningWriter, but a caller
// that never attaches a warning writer (e.g. server/embedded use) must still
// get the "warns once per invocation" behavior the docs promise, so this
// process-wide fallback keeps the latch alive independent of that call.
//
//nolint:gochecknoglobals // process-wide fallback latch for a once-per-invocation notice.
var ignoredLocalKeychainWarningLatchFallback sync.Once

func ignoredLocalKeychainWarningLatchFromCtx(ctx context.Context) *sync.Once {
	if latch, ok := ctx.Value(ignoredLocalKeychainWarningLatchKey{}).(*sync.Once); ok {
		return latch
	}
	return &ignoredLocalKeychainWarningLatchFallback
}
