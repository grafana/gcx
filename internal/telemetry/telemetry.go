// Package telemetry implements gcx's anonymous usage stats: one flat event
// per invocation describing the shape of usage (command path, flag names,
// outcome) and never its content (argument values, resource names, hosts).
// The only correlator is a random, resettable, per-install device ID.
//
// This package is a library only: event construction and emission are wired
// at the CLI lifecycle boundaries (cmd/gcx/main.go), not here.
package telemetry

import (
	"os"
	"strings"
)

// Mode is the resolved telemetry state for an invocation.
type Mode string

const (
	// ModeEnabled emits the event.
	ModeEnabled Mode = "enabled"
	// ModeDisabled emits nothing.
	ModeDisabled Mode = "disabled"
	// ModeLog prints the event that would be sent to stderr and sends nothing.
	ModeLog Mode = "log"
)

// defaultMode is the resolved mode when no env var or config setting applies.
// It stays disabled until privacy/legal and usage-stats owner sign-off clear;
// flipping to ModeEnabled is deliberately a one-line change gated on those.
const defaultMode = ModeDisabled

// Env documents the environment variables that control telemetry. The env
// tags are read by scripts/env-vars-reference (docs generation); resolution
// itself happens in ResolveMode.
type Env struct {
	// Telemetry controls anonymous usage telemetry for this invocation:
	// "enabled", "disabled", or "log" (print the event to stderr and send
	// nothing). Takes precedence over DO_NOT_TRACK and the
	// `diagnostics.telemetry` config field.
	Telemetry string `env:"GCX_TELEMETRY"`

	// DoNotTrack disables anonymous usage telemetry when set to "1" or
	// "true" (cross-tool DO_NOT_TRACK convention). Overridden by
	// GCX_TELEMETRY.
	DoNotTrack string `env:"DO_NOT_TRACK"`

	// Endpoint overrides the URL usage telemetry is sent to.
	Endpoint string `env:"GCX_TELEMETRY_ENDPOINT"`
}

// ResolveMode resolves the telemetry mode for this invocation. Precedence,
// highest first: GCX_TELEMETRY, DO_NOT_TRACK, the diagnostics.telemetry
// config value, the built-in default. Unrecognised values fall through to
// the next level. configValue is a func so callers only pay the config-file
// read when the environment doesn't already decide the mode.
func ResolveMode(configValue func() string) Mode {
	return resolveMode(os.Getenv, configValue)
}

// Env var names read by this package. envConsistencyTest asserts they match
// the Env struct tags the docs generator reads, so the documented names
// cannot drift from the resolved ones.
const (
	envTelemetry  = "GCX_TELEMETRY"
	envDoNotTrack = "DO_NOT_TRACK"
	envEndpoint   = "GCX_TELEMETRY_ENDPOINT"
)

func resolveMode(getenv func(string) string, configValue func() string) Mode {
	if m, ok := parseMode(getenv(envTelemetry)); ok {
		return m
	}
	if isDoNotTrack(getenv(envDoNotTrack)) {
		return ModeDisabled
	}
	if m, ok := parseMode(configValue()); ok {
		return m
	}
	return defaultMode
}

func parseMode(s string) (Mode, bool) {
	m := Mode(strings.ToLower(s))
	switch m {
	case ModeEnabled, ModeDisabled, ModeLog:
		return m, true
	default:
		return "", false
	}
}

func isDoNotTrack(s string) bool {
	switch strings.ToLower(s) {
	case "1", "true":
		return true
	default:
		return false
	}
}
