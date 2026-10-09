// Package activation provides a reusable pre-flight check for whether the
// App Observability plugin is installed and enabled on a stack.
package activation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	internalconfig "github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	k8srest "k8s.io/client-go/rest"
)

// SettingsEndpoint is the plugin-proxy path that reads/writes the App
// Observability plugin's settings; a 404 there is the plugin's own "not
// installed or not enabled" signal. Exported so internal/providers/appo11y/settings
// and .../overrides — which genuinely cannot operate without the plugin, unlike
// the best-effort telemetry commands Gate covers — share this repo's single
// copy of the path instead of each keeping their own.
const SettingsEndpoint = "/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings"

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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.Host+SettingsEndpoint, nil)
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

// Gate runs the activation pre-flight and never blocks the command — it
// surfaces the result as a stderr warning either way. The commands this
// guards (services list/get/map/list-operations/list-labels) read
// target_info and span metrics straight from Prometheus/Tempo; none of that
// data requires the App Observability Grafana app plugin itself to be
// installed, only OTLP ingestion into the stack's own datasources. Treating
// a 404 from the plugin-proxy settings endpoint as fatal would reject a
// perfectly good OTLP-only stack, and an inconclusive result (auth failure,
// 5xx, transport error) is even weaker evidence — in both cases the
// caller's own query is the real source of truth for whether there's
// anything to show.
func Gate(ctx context.Context, cfg internalconfig.NamespacedRESTConfig, stderr io.Writer) {
	activated, err := IsActivated(ctx, cfg)
	if err != nil {
		cmdio.EmitWarn(stderr, fmt.Sprintf("App Observability activation check failed (%v) — continuing to query the datasource directly", err))
		return
	}
	if !activated {
		cmdio.EmitWarn(stderr, NotActivatedError().Error())
	}
}
