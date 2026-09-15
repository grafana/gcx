package version

import (
	"fmt"
	"runtime"

	"github.com/grafana/gcx/internal/agent"
)

//nolint:gochecknoglobals // Set once from main at startup.
var (
	ver    string
	commit string
	date   string
)

// Set stores the application version. Call from main() before HTTP clients are created.
func Set(v string) { ver = v }

// SetBuildInfo stores commit hash and build date alongside the version.
func SetBuildInfo(c, d string) {
	commit = c
	date = d
}

// Get returns the raw version string, defaulting to "SNAPSHOT".
func Get() string {
	if ver == "" {
		return "SNAPSHOT"
	}
	return ver
}

// GetCommit returns the short commit hash, or "unknown" if not set.
func GetCommit() string {
	if commit == "" {
		return "unknown"
	}
	return commit
}

// GetDate returns the build date, or "unknown" if not set.
func GetDate() string {
	if date == "" {
		return "unknown"
	}
	return date
}

// UserAgent returns the formatted User-Agent:
// gcx/{version} ({os}/{arch}; user={human|agent}).
func UserAgent() string {
	return fmt.Sprintf("gcx/%s (%s/%s; user=%s)", Get(), runtime.GOOS, runtime.GOARCH, userType())
}

// userType reports whether a person or an AI agent is driving gcx. It reads
// the same detection that usage stats report, so the header and the telemetry
// event can never disagree about a given invocation.
func userType() string {
	if agent.IsAgentMode() {
		return "agent"
	}

	return "human"
}

// Info returns a structured snapshot of all version metadata.
func Info() InfoData {
	return InfoData{
		Version:   Get(),
		Commit:    GetCommit(),
		BuildDate: GetDate(),
		Go:        runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

// InfoData holds structured version metadata for JSON/YAML serialization.
type InfoData struct {
	Version   string `json:"version"   yaml:"version"`
	Commit    string `json:"commit"    yaml:"commit"`
	BuildDate string `json:"buildDate" yaml:"buildDate"`
	Go        string `json:"go"        yaml:"go"`
	OS        string `json:"os"        yaml:"os"`
	Arch      string `json:"arch"      yaml:"arch"`
}
