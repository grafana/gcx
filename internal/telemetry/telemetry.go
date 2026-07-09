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
	// "true" (cross-tool convention, see https://consoledonottrack.com).
	// Overridden by GCX_TELEMETRY.
	DoNotTrack string `env:"DO_NOT_TRACK"`
}

// ResolveMode resolves the telemetry mode for this invocation. Precedence,
// highest first: GCX_TELEMETRY, DO_NOT_TRACK, the diagnostics.telemetry
// config value, the built-in default. Unrecognised values fall through to
// the next level.
func ResolveMode(configValue string) Mode {
	return resolveMode(os.Getenv, configValue)
}

func resolveMode(getenv func(string) string, configValue string) Mode {
	env := Env{
		Telemetry:  getenv("GCX_TELEMETRY"),
		DoNotTrack: getenv("DO_NOT_TRACK"),
	}
	if m, ok := parseMode(env.Telemetry); ok {
		return m
	}
	if isDoNotTrack(env.DoNotTrack) {
		return ModeDisabled
	}
	if m, ok := parseMode(configValue); ok {
		return m
	}
	return defaultMode
}

func parseMode(s string) (Mode, bool) {
	switch Mode(strings.ToLower(s)) {
	case ModeEnabled, ModeDisabled, ModeLog:
		return Mode(strings.ToLower(s)), true
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
