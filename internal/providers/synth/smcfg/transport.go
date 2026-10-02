package smcfg

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/version"
	"github.com/grafana/gcx/pkg/gfc/sm"
	"k8s.io/client-go/rest"
)

// smClientID must match an entry in sm-api's allowedClientTypes; any other
// value is logged as client_type="unknown" in the SM API's request logs.
const smClientID = "gcx"

// FallbackLoader resolves direct SM-API credentials for the fallback transport.
// It is invoked lazily, only when the datasource proxy denies access (HTTP 403)
// or no SM datasource UID is available. Loader satisfies it.
type FallbackLoader interface {
	LoadSMConfig(ctx context.Context) (baseURL, token, namespace string, err error)
}

// NewSMTransport builds the shared dual-mode SM transport from gcx's config.
//
// The proxy path uses the caller's Grafana credential via the k8s REST
// transport (which chains LoggingRoundTripper). The direct path deliberately
// uses a separate httputils client: the SM bearer token is set per-request, and
// an auth-injecting round-tripper on the Grafana client would overwrite it —
// sending the Grafana credential to the SM API host. A nil fallback disables
// the direct path.
func NewSMTransport(ctx context.Context, restCfg config.NamespacedRESTConfig, datasourceUID string, fallback FallbackLoader) (*sm.Transport, error) {
	httpClient, err := rest.HTTPClientFor(&restCfg.Config)
	if err != nil {
		return nil, fmt.Errorf("creating SM datasource-proxy client: %w", err)
	}

	cfg := sm.TransportConfig{
		HTTPClient:       httpClient,
		DirectHTTPClient: httputils.NewDefaultClient(ctx),
		GrafanaHost:      restCfg.Host,
		DatasourceUID:    datasourceUID,
		ClientID:         smClientID,
		ClientVersion:    version.Get(),
	}
	if fallback != nil {
		cfg.Fallback = fallbackAdapter{inner: fallback}
	}

	return sm.NewTransport(cfg)
}

// fallbackAdapter drops the namespace return value that gcx's loader carries but
// the library transport does not need.
type fallbackAdapter struct {
	inner FallbackLoader
}

func (a fallbackAdapter) LoadSMConfig(ctx context.Context) (string, string, error) {
	baseURL, token, _, err := a.inner.LoadSMConfig(ctx)
	return baseURL, token, err
}
