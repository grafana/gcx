package config

import "sync/atomic"

const (
	targetKindCloud      = "cloud"
	targetKindSelfHosted = "self-hosted"
)

// capturedTargetKind holds the target kind for the invocation. LoadLayered
// stores it once per call, from the effective context after all layers are
// merged and every override (context selection, env vars) has been applied —
// not once per layer. When a command loads config more than once, the last
// load wins, which is the most specific view of what the command actually
// targeted. It is read once at exit by the usage-stats emitter.
//
// It stays "" for paths that never reach LoadLayered, notably the LoadForWrite
// shortcuts for --config and --file. Those load a single named layer instead of
// the merged view, so classifying from them could report a kind the invocation
// never targeted; an absent value is preferable to a wrong one, and config
// writes do not talk to a Grafana instance anyway.
//
//nolint:gochecknoglobals
var capturedTargetKind atomic.Value

// CapturedTargetKind returns target kind of the config loaded by the command invocation.
func CapturedTargetKind() string {
	kind, _ := capturedTargetKind.Load().(string)
	return kind
}

// captureTargetKind classifies the config's effective context and records it.
func captureTargetKind(cfg *Config) {
	capturedTargetKind.Store(targetKindForContext(cfg.GetCurrentContext()))
}

// CaptureTargetKindForServer records the target kind for a Grafana server URL
// that no configured context describes yet — gcx login building a new context
// from --server. Without it the value captured by the preceding config load
// would describe whichever context happened to be current, mislabelling the
// login target in either direction. An empty server carries no signal, so it
// leaves any kind already captured for this invocation in place.
func CaptureTargetKindForServer(server string) {
	if server == "" {
		return
	}
	capturedTargetKind.Store(targetKindForContext(&Context{Grafana: &GrafanaConfig{Server: server}}))
}

// targetKindForContext classifies a context as cloud or self-hosted for
// telemetry. It reports "" when no Grafana target can be determined.
//
// A context holding only a cloud entry (an org-wide GCOM credential, no stack
// slug and no Grafana server) is deliberately "" rather than cloud: target_kind
// describes the Grafana instance a command talked to, and such a context names
// no instance. Commands against the Cloud control plane alone are therefore
// unclassified by design.
func targetKindForContext(context *Context) string {
	switch {
	case context == nil:
		return ""
	case context.IsCloud():
		return targetKindCloud
	case context.Grafana == nil || context.Grafana.Server == "":
		return ""
	}
	return targetKindSelfHosted
}
