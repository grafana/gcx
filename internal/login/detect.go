package login

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/grafana"
)

// DetectTarget classifies the Grafana server URL into a Target.
// Detection order (D5): Cloud domain → local hostname → /api/frontend/settings probe → TargetUnknown.
// Explicit Target (opts.Target) is handled by the caller before invoking DetectTarget.
func DetectTarget(ctx context.Context, server string, httpClient *http.Client) (Target, error) {
	if _, ok := config.StackSlugFromServerURL(server); ok {
		return TargetCloud, nil
	}

	parsed, err := url.Parse(server)
	if err != nil {
		return TargetUnknown, err
	}
	if isLocalHostname(parsed.Hostname()) {
		return TargetOnPrem, nil
	}

	return probeTarget(ctx, server, httpClient)
}

// isLocalHostname returns true for loopback addresses, RFC 1918 private IPv4 ranges,
// and IPv6 ULA (fd00::/8). Enterprise-intranet suffixes (.local, .internal, .corp, .lan)
// are NOT treated as local — NC-009.
func isLocalHostname(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}

	if strings.HasSuffix(host, ".localhost") {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 10:
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return true
		case ip4[0] == 192 && ip4[1] == 168:
			return true
		}
		return false
	}

	// IPv6 ULA fd00::/8
	return len(ip) == 16 && ip[0] == 0xfd
}

// probeTarget calls /api/frontend/settings with a ≤3s timeout and checks for Cloud markers.
// A valid response with no Cloud markers is definitively on-prem (FR-006c).
// Any error, timeout, or non-200 status yields TargetUnknown.
//
// httpClient is forwarded to FetchAnonymousSettings so that callers can supply
// a TLS-aware client for mTLS servers. When nil, a default client is used.
func probeTarget(ctx context.Context, server string, httpClient *http.Client) (Target, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	settings, err := grafana.FetchAnonymousSettings(probeCtx, server, httpClient)
	if err != nil {
		// Any error (network failure, timeout, non-200, decode failure) → TargetUnknown.
		return TargetUnknown, nil
	}

	// Parse the grafanaUrl from buildInfo to extract a clean hostname for the Cloud check.
	parsed, err := url.Parse(settings.BuildInfo.GrafanaURL)
	if err != nil || parsed.Hostname() == "" {
		// Unparseable or empty grafanaUrl — valid probe but no Cloud marker → OnPrem.
		return TargetOnPrem, nil
	}

	host := strings.ToLower(parsed.Hostname())
	if config.IsGrafanaCloudHost(host) {
		return TargetCloud, nil
	}

	// Valid probe, no Cloud markers → definitively on-prem
	return TargetOnPrem, nil
}
