package config

import "sync/atomic"

const (
	targetKindCloud       = "cloud"
	targetKindSelfManaged = "self-hosted"
)

// capturedTargetKind holds the targetKind for the invocation. It is updated when each layer of the config is read. It is read once at exit by the usage-stats emitter.
//
//nolint:gochecknoglobals
var capturedTargetKind atomic.Value

// CapturedTargetKind returns target kind of the config loaded by the command invocation.
func CapturedTargetKind() string {
	kind, _ := capturedTargetKind.Load().(string)
	return kind
}

// captureTargetKind writes the current config layer's classification to the capturedTargetKind atomic value.
func captureTargetKind(cfg *Config) {
	capturedTargetKind.Store(targetKindForContext(cfg.GetCurrentContext()))
}

func targetKindForContext(context *Context) string {
	switch {
	case context == nil:
		return ""
	case context.IsCloud():
		return targetKindCloud
	case context.Grafana == nil || context.Grafana.Server == "":
		return ""
	}
	return targetKindSelfManaged
}
