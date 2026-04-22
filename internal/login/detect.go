package login

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
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
func probeTarget(ctx context.Context, server string, httpClient *http.Client) (Target, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	settingsURL := strings.TrimSuffix(server, "/") + "/api/frontend/settings"
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, settingsURL, nil)
	if err != nil {
		return TargetUnknown, nil
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return TargetUnknown, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return TargetUnknown, nil
	}

	var settings struct {
		BuildInfo struct {
			GrafanaURL string `json:"grafanaUrl"`
		} `json:"buildInfo"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&settings); err != nil {
		return TargetUnknown, nil
	}

	u := strings.ToLower(settings.BuildInfo.GrafanaURL)
	for _, d := range []string{
		".grafana.net", ".grafana-dev.net", ".grafana-ops.net", // real Cloud stack URLs
		"grafana.com", "grafana-dev.com", "grafana-ops.com", // Cloud root domains
	} {
		if strings.Contains(u, d) {
			return TargetCloud, nil
		}
	}

	// Valid probe, no Cloud markers → definitively on-prem
	return TargetOnPrem, nil
}
