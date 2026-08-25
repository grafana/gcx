package instances

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	k8srest "k8s.io/client-go/rest"
)

// Database Observability gates its own UI behind a per-stack "activation"
// resource — DbO11yConfig under the shared productactivation.ext.grafana.com
// API group other Grafana Cloud o11y products (App O11y, Host O11y, K8s O11y)
// also use. Confirmed from the grafana-dbo11y-app v2.30.0 production bundle
// (module.js chunk 993): the plugin's own ActivationService does
//
//	GET /apis/productactivation.ext.grafana.com/v1alpha1/namespaces/<ns>/dbo11yconfigs/global
//
// and treats a 404 (config never created) or `spec.enabled == false` as "not
// activated". There is no dedicated activation-status endpoint separate from
// this resource — the plugin's own onboarding flow POSTs the same resource
// (spec: {enabled: true}) to activate.
const (
	productActivationAPIGroup = "productactivation.ext.grafana.com/v1alpha1"
	dbo11yConfigResource      = "dbo11yconfigs"
	dbo11yConfigName          = "global"
)

// dbo11yActivationDocsURL is Grafana's own get-started doc, referenced in the
// not-activated hint below.
const dbo11yActivationDocsURL = "https://grafana.com/docs/grafana-cloud/monitor-applications/database-observability/get-started/"

// checkActivation reports whether Database Observability is activated for
// the stack cfg targets. The second return value is an unexpected-failure
// error (transport failure, non-404 non-2xx status, malformed body) — callers
// should treat that as inconclusive (log it, don't block on it) rather than
// as "not activated", since the activation resource is a best-effort
// diagnostic enrichment, not a hard dependency of the metrics-based commands.
func checkActivation(ctx context.Context, cfg internalconfig.NamespacedRESTConfig) (bool, error) {
	hc, err := k8srest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return false, fmt.Errorf("failed to create HTTP client for activation check: %w", err)
	}

	url := fmt.Sprintf("%s/apis/%s/namespaces/%s/%s/%s", cfg.Host, productActivationAPIGroup, cfg.Namespace, dbo11yConfigResource, dbo11yConfigName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to build activation check request: %w", err)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return false, fmt.Errorf("activation check request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, providers.HandleErrorResponse(resp)
	}

	var body struct {
		Spec struct {
			Enabled bool `json:"enabled"`
		} `json:"spec"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, fmt.Errorf("failed to decode activation config response: %w", err)
	}
	return body.Spec.Enabled, nil
}

// notActivatedError is the error surfaced in place of a generic "no data"
// message once checkActivation has positively confirmed the product isn't
// enabled for this stack.
func notActivatedError() error {
	return errors.New("Database Observability is not activated for this stack — activate it in Grafana Cloud " + //nolint:staticcheck // "Grafana" is a proper noun, capitalization is intentional
		"(Connections > Add new connection > Database Observability), or see " + dbo11yActivationDocsURL)
}
