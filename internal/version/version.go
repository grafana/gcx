package version

import (
	"fmt"
	"runtime"
)

var ver string //nolint:gochecknoglobals // Set once from main at startup.

// Set stores the application version. Call from main() before HTTP clients are created.
func Set(v string) { ver = v }

// Get returns the raw version string, defaulting to "SNAPSHOT".
func Get() string {
	if ver == "" {
		return "SNAPSHOT"
	}
	return ver
}

// UserAgent returns the formatted User-Agent: gcx/{version} ({os}/{arch}).
func UserAgent() string {
	return fmt.Sprintf("gcx/%s (%s/%s)", Get(), runtime.GOOS, runtime.GOARCH)
}
