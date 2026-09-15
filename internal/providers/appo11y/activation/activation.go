// Package activation provides a reusable pre-flight check for whether the
// App Observability plugin is installed and enabled on a stack.
package activation

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	k8srest "k8s.io/client-go/rest"
)

// settingsEndpoint is the same plugin-proxy path
// internal/providers/appo11y/settings uses to read/write plugin settings; a
// 404 there is the plugin's own "not installed or not enabled" signal.
const settingsEndpoint = "/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings"

// notActivatedDocsURL points to App Observability's get-started doc.
const notActivatedDocsURL = "https://grafana.com/docs/grafana-cloud/monitor-applications/application-observability/"

// IsActivated reports whether the App Observability plugin is installed and
// enabled for the stack cfg targets. A definitive "no" is (false, nil): HTTP
// 404 from the plugin-proxy settings endpoint. Any other failure (transport
// error, non-404 non-2xx status, malformed body) is INCONCLUSIVE and
// returned as (false, err) — callers must not treat that as "not activated".
func IsActivated(ctx context.Context, cfg internalconfig.NamespacedRESTConfig) (bool, error) {
	httpClient, err := k8srest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return false, fmt.Errorf("failed to create HTTP client for activation check: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.Host+settingsEndpoint, nil)
	if err != nil {
		return false, fmt.Errorf("failed to build activation check request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("activation check request failed: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return true, nil
	default:
		return false, providers.HandleErrorResponse(resp)
	}
}

// NotActivatedError returns the error to surface when IsActivated has
// positively confirmed (false, nil) — i.e. App O11y is definitively not
// activated for this stack.
func NotActivatedError() error {
	return errors.New("App Observability is not activated for this stack — install/enable the App Observability plugin, or see " + notActivatedDocsURL) //nolint:staticcheck // "App" is a proper noun, capitalization is intentional
}

// Gate runs the activation pre-flight and returns an error only when
// IsActivated has definitively confirmed the plugin is not activated. An
// inconclusive result (IsActivated's (false, err) case — auth failure, 5xx,
// transport error) does not block the command: the caller's own
// Prometheus/Tempo request will either succeed or surface a more specific
// error than this best-effort check could.
func Gate(ctx context.Context, cfg internalconfig.NamespacedRESTConfig) error {
	activated, err := IsActivated(ctx, cfg)
	if err != nil {
		return nil //nolint:nilerr // deliberate: an inconclusive check must not block the command
	}
	if !activated {
		return NotActivatedError()
	}
	return nil
}
