// Package synth is a thin client for the Grafana Synthetic Monitoring
// datasource proxy at /api/datasources/proxy/uid/<uid>/sm/<path>.
//
// Probe and Check types are reused from internal/providers/synth/{probes,checks}
// rather than redefined here. Authentication follows whatever the underlying
// NamespacedRESTConfig is wired with — a SAT bearer today, OAuth once the
// assistant-proxy scope mapping is fixed (see
// docs/research/2026-05-04-sm-datasource-oauth-gap.md).
//
// Per-resource methods live in their own files (probes.go, checks.go) and
// share this base client and proxyGet helper.
package synth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

const (
	maxResponseBytes = 10 << 20
	datasourceKind   = "synthetic-monitoring-datasource"
)

// Client queries the Synthetic Monitoring datasource through Grafana's
// /api/datasources/proxy/uid/<uid>/sm/... routes.
type Client struct {
	host       string
	httpClient *http.Client
}

// NewClient creates a new SM datasource proxy client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{
		host:       cfg.Host,
		httpClient: httpClient,
	}, nil
}

// proxyGet performs a GET against the SM datasource proxy at
// /api/datasources/proxy/uid/<uid>/<path> and returns the raw response body.
// Per-resource methods unmarshal the body into their typed shape.
func (c *Client) proxyGet(ctx context.Context, datasourceUID, path, op string) ([]byte, error) {
	fullURL := c.host + "/api/datasources/proxy/uid/" + url.PathEscape(datasourceUID) + "/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody(datasourceKind, op, resp.StatusCode, body)
	}
	return body, nil
}
