package checks

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// knownCheckTypes is the set of valid SM check type names.
//
//nolint:gochecknoglobals // Static lookup table used in ValidateCheckSpec.
var knownCheckTypes = map[string]bool{
	"http":       true,
	"ping":       true,
	"dns":        true,
	"tcp":        true,
	"traceroute": true,
	"scripted":   true,
	"browser":    true,
	"grpc":       true,
	"multihttp":  true,
}

// ValidateCheckSpec validates a CheckSpec against known probe names and type rules.
// Returns a slice of human-readable error strings. Empty means valid.
func ValidateCheckSpec(spec *CheckSpec, probeIDMap map[string]int64) []string {
	var errs []string

	if spec.Target == "" {
		errs = append(errs, "target is required")
	}

	checkType := spec.Settings.CheckType()
	if !knownCheckTypes[checkType] {
		errs = append(errs, fmt.Sprintf("unknown check type %q", checkType))
	}

	for _, probeName := range spec.Probes {
		if _, ok := probeIDMap[probeName]; !ok {
			errs = append(errs, fmt.Sprintf("probe %q not found", probeName))
		}
	}

	// DNS checks should use a hostname, not a URL.
	if checkType == "dns" && (strings.HasPrefix(spec.Target, "http://") || strings.HasPrefix(spec.Target, "https://")) {
		errs = append(errs, "dns check target should be a hostname, not a URL (e.g. example.com)")
	}

	return errs
}

// AllProbesOffline returns true if every probe in probeNames is offline according to onlineMap.
// Returns false for an empty probe list or if any probe is online.
func AllProbesOffline(probeNames []string, onlineMap map[string]bool) bool {
	if len(probeNames) == 0 {
		return false
	}
	for _, name := range probeNames {
		if online, ok := onlineMap[name]; ok && online {
			return false
		}
	}
	return true
}

// ValidateHTTPTarget performs a HEAD request against the target URL.
// Only validates checks of type "http"; all other types return nil immediately.
// Returns nil on success (2xx/3xx/4xx responses are acceptable — only 5xx and
// connection errors are reported). This is advisory — callers should warn, not fail.
func ValidateHTTPTarget(checkType, target string, timeout time.Duration) error {
	if checkType != "http" {
		return nil
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Head(target) //nolint:noctx // advisory validation, timeout is set on the client
	if err != nil {
		return fmt.Errorf("target %q is unreachable: %w", target, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("target %q returned HTTP %d", target, resp.StatusCode)
	}
	return nil
}
