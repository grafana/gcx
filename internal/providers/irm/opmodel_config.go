// opmodel_config.go; authenticated Grafana proxy configuration for the op
// model HTTP client.
package irm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/client-go/rest"
)

const defaultOpModelTimeout = 5 * time.Second

// OpModelClientLoader produces a configured op model client. Command tests
// substitute a fake returning a stub API.
type OpModelClientLoader interface {
	LoadOpModel(ctx context.Context, timeout time.Duration) (OpModelAPI, error)
}

// LoadOpModel uses the active Grafana context for the target and
// authentication. The IRM plugin resource proxy derives tenancy from that
// authenticated context and supplies the organization scope needed by
// Incident-backed delivery.
func (l *configLoader) LoadOpModel(ctx context.Context, timeout time.Duration) (OpModelAPI, error) {
	return l.loadIRMResourceClient(ctx, timeout)
}

func (l *configLoader) loadIRMResourceClient(ctx context.Context, timeout time.Duration) (*irmResourceClient, error) {
	restCfg, err := l.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, err
	}

	// --timeout is set as the client timeout, which makes it a genuine
	// per-request deadline: `shares publish` issues one request per entity and
	// evidence link, and one budget for the whole sequence would fail a legal package
	// partway, leaving entities that cannot be rolled back.
	restConfig := restCfg.Config
	restConfig.Timeout = timeout
	httpClient, err := rest.HTTPClientFor(&restConfig)
	if err != nil {
		return nil, fmt.Errorf("create authenticated IRM client: %w", err)
	}

	endpoint := strings.TrimRight(restCfg.Host, "/") + "/api/plugins/grafana-irm-app/resources"
	return newIRMResourceClient(httpClient, endpoint), nil
}
