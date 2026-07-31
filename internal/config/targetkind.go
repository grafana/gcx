package config

import "sync/atomic"

// TargetKind classifies the Grafana instance a command targeted. The values are
// the ones sent in the usage-stats event.
type TargetKind string

const (
	// TargetKindUnknown means no Grafana target could be determined.
	TargetKindUnknown    TargetKind = ""
	TargetKindCloud      TargetKind = "cloud"
	TargetKindSelfHosted TargetKind = "self-hosted"
)

// capturedTargetKind holds the target kind for the invocation. LoadLayered
// stores it once per call, from the effective context after all layers are
// merged and every override (context selection, env vars) has been applied —
// not once per layer. When a command loads config more than once, the last
// load wins, which is the most specific view of what the command actually
// targeted. It is read once at exit by the usage-stats emitter.
//
// It stays unset for paths that never reach LoadLayered, notably the
// LoadForWrite shortcuts for --config and --file. Those load a single named
// layer instead of the merged view, so classifying from them could report a
// kind the invocation never targeted, and an absent value is preferable to a
// wrong one.
//
//nolint:gochecknoglobals
var capturedTargetKind atomic.Value

// CapturedTargetKind returns target kind of the config loaded by the command invocation.
func CapturedTargetKind() string {
	kind, _ := capturedTargetKind.Load().(string)
	return kind
}

// captureTargetKind classifies the config's effective context and records it.
// Unlike CaptureTargetKind it records TargetKindUnknown too: a load that
// resolved no context is itself the answer for that invocation.
func captureTargetKind(cfg *Config) {
	capturedTargetKind.Store(string(targetKindForContext(cfg.GetCurrentContext())))
}

// CaptureTargetKind records a target kind the caller determined for itself.
// gcx login resolves its target by probing the server and by honouring --cloud,
// both of which outrank the hostname guess in CaptureTargetKindForServer, so it
// reports the resolved value through here.
//
// TargetKindUnknown is ignored rather than stored: a detection that came back
// undecided is no reason to erase a kind already established for this
// invocation.
func CaptureTargetKind(kind TargetKind) {
	if kind == TargetKindUnknown {
		return
	}
	capturedTargetKind.Store(string(kind))
}

// ClearCapturedTargetKind resets the captured kind to unknown.
//
// gcx login uses it once it has resolved neither a context nor a server: the
// kind the config load took from whichever context happened to be current
// describes a target this invocation is not aiming at, and reporting nothing is
// the honest answer. Prefer CaptureTargetKind for a kind that is merely
// undecided — that leaves an established value alone rather than erasing it.
func ClearCapturedTargetKind() {
	capturedTargetKind.Store(string(TargetKindUnknown))
}

// CaptureTargetKindForServer records the target kind implied by a Grafana server
// URL, for the point in gcx login before target detection has run and where no
// configured context describes the requested server. Without it the value
// captured by the preceding config load would describe whichever context
// happened to be current, mislabelling the login target in either direction.
//
// This is a hostname guess and nothing more; a custom domain fronting a Cloud
// stack looks self-hosted here. Callers that later learn the real target must
// report it through CaptureTargetKind. An empty server carries no signal and
// leaves any kind already captured in place.
func CaptureTargetKindForServer(server string) {
	if server == "" {
		return
	}
	CaptureTargetKind(targetKindForContext(&Context{Grafana: &GrafanaConfig{Server: server}}))
}

// targetKindForContext classifies a context as cloud or self-hosted for
// telemetry. It reports TargetKindUnknown when no Grafana target can be
// determined.
//
// A context holding only a cloud entry (an org-wide GCOM credential, no stack
// slug and no Grafana server) is deliberately unknown rather than cloud:
// target_kind describes the Grafana instance a command talked to, and such a
// context names no instance. Commands against the Cloud control plane alone are
// therefore unclassified by design.
func targetKindForContext(context *Context) TargetKind {
	switch {
	case context == nil:
		return TargetKindUnknown
	case context.IsCloud():
		return TargetKindCloud
	case context.Grafana == nil || context.Grafana.Server == "":
		return TargetKindUnknown
	}
	return TargetKindSelfHosted
}
