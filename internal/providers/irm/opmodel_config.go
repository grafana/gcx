package irm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/client-go/rest"
)

const defaultOpModelTimeout = 5 * time.Second

type OpModelClientLoader interface {
	LoadOpModel(ctx context.Context, timeout time.Duration) (OpModelAPI, error)
}

func (l *configLoader) LoadOpModel(ctx context.Context, timeout time.Duration) (OpModelAPI, error) {
	return l.loadIRMResourceClient(ctx, timeout)
}

func (l *configLoader) loadIRMResourceClient(ctx context.Context, timeout time.Duration) (*irmResourceClient, error) {
	restCfg, err := l.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, err
	}

	restConfig := restCfg.Config
	restConfig.Timeout = timeout
	httpClient, err := rest.HTTPClientFor(&restConfig)
	if err != nil {
		return nil, fmt.Errorf("create authenticated IRM client: %w", err)
	}

	endpoint := strings.TrimRight(restCfg.Host, "/") + "/api/plugins/grafana-irm-app/resources"
	return newIRMResourceClient(httpClient, endpoint), nil
}
