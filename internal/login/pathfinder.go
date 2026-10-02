package login

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"k8s.io/client-go/rest"
)

// PathfinderAppID is the plugin ID of Pathfinder, the in-product learning
// app that serves interactive guides inside Grafana.
const PathfinderAppID = "grafana-pathfinder-app"

const (
	pathfinderSettingsPath = "/api/plugins/" + PathfinderAppID + "/settings"
	pathfinderAppPath      = "/a/" + PathfinderAppID

	// pathfinderProbeTimeout bounds the probe so an unresponsive plugin API
	// cannot delay a login that has already succeeded.
	pathfinderProbeTimeout = 5 * time.Second

	pathfinderMaxResponseBytes = 1 << 20
)

// PathfinderURL returns the Pathfinder app URL on the given Grafana server.
func PathfinderURL(server string) string {
	return strings.TrimSuffix(server, "/") + pathfinderAppPath
}

// DetectPathfinder reports whether the Pathfinder plugin is installed and
// enabled on the stack behind restCfg. The probe is advisory: any failure
// (transport error, non-200 status, undecodable body) reports false, so it
// never fails a login.
func DetectPathfinder(ctx context.Context, restCfg config.NamespacedRESTConfig) bool {
	httpClient, err := rest.HTTPClientFor(&restCfg.Config)
	if err != nil {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, pathfinderProbeTimeout)
	defer cancel()

	url := strings.TrimSuffix(restCfg.Host, "/") + pathfinderSettingsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	body, err := httputils.ReadResponseBody(resp.Body, pathfinderMaxResponseBytes)
	if err != nil {
		return false
	}
	var settings struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		return false
	}
	return settings.Enabled
}

// probePathfinder runs the Pathfinder probe when the caller asked for it and
// the target is a Cloud stack, honouring the PathfinderFn test seam.
func probePathfinder(ctx context.Context, opts *Options, target Target, restCfg config.NamespacedRESTConfig) bool {
	if !opts.DetectPathfinder || target != TargetCloud {
		return false
	}
	if opts.PathfinderFn != nil {
		return opts.PathfinderFn(ctx, restCfg)
	}
	return DetectPathfinder(ctx, restCfg)
}
