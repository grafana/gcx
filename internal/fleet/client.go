package fleet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

const (
	// collectorAppProxyBase is the app plugin-proxy prefix for grafana-collector-app.
	// The plugin is frontend-only (no backend), so it is reached via the app
	// plugin-proxy path, not the backend-plugin /api/plugins/<id>/resources/... route.
	collectorAppProxyBase = "/api/plugin-proxy/grafana-collector-app"

	// fleetManagementAPIPath forwards to the Fleet Management API. The Connect
	// service/method path is appended unchanged; the proxy injects the FM
	// credential and the X-Prom-*/X-Scope-OrgID headers server-side.
	fleetManagementAPIPath = collectorAppProxyBase + "/fleet-management-api"

	// instanceMetadataPath is the Viewer-role route returning the caller's stack
	// instance metadata (proxying GCOM's /api/instances/{stackId}; the plugin
	// injects the stack ID).
	instanceMetadataPath = collectorAppProxyBase + "/grafanacom-api/instances/"
)

// Client is a base HTTP client for the Grafana Fleet Management API, reached
// through the grafana-collector-app plugin proxy at cfg.Host. All Fleet
// Management operations use POST (gRPC/Connect style JSON-over-HTTP). The
// Grafana bearer credential (including OAuth) is injected by the k8s
// round-tripper obtained from rest.HTTPClientFor; this client sets no
// Authorization header or Basic auth of its own.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new Fleet Management base client from a Grafana REST
// config. The HTTP client is built with rest.HTTPClientFor(&cfg.Config) so the
// Grafana bearer token is injected by the round-tripper — the same transport
// the other plugin-proxied providers use.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("fleet: create HTTP client: %w", err)
	}
	return &Client{
		restConfig: cfg,
		httpClient: httpClient,
	}, nil
}

// DoRequest builds and executes a POST request against the Fleet Management API
// via the collector-app plugin proxy. path is the Connect service/method path
// (e.g. "/pipeline.v1.PipelineService/ListPipelines"); it is appended to the
// proxy's fleet-management-api route unchanged. It is exported so packages
// composing this client can call the base transport.
func (c *Client) DoRequest(ctx context.Context, path string, body any) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("fleet: marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	url := c.restConfig.Host + fleetManagementAPIPath + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("fleet: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fleet: execute request: %w", err)
	}

	return resp, nil
}

// FetchInstanceMetadata retrieves the caller's Grafana Cloud stack instance
// metadata through the collector-app's Viewer-role instance-metadata proxy
// route. The response is the same GCOM /api/instances/{stackId} object gcx
// decodes elsewhere, so instrumentation can derive backend datasource URLs from
// it without a Cloud access-policy token. The plugin injects the stack ID.
func (c *Client) FetchInstanceMetadata(ctx context.Context) (cloud.StackInfo, error) {
	url := c.restConfig.Host + instanceMetadataPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return cloud.StackInfo{}, fmt.Errorf("fleet: create instance-metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return cloud.StackInfo{}, fmt.Errorf("fleet: fetch instance metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return cloud.StackInfo{}, &HTTPError{
			Status: resp.StatusCode,
			Path:   instanceMetadataPath,
			Body:   ReadErrorBody(resp),
		}
	}

	var stack cloud.StackInfo
	if err := json.NewDecoder(resp.Body).Decode(&stack); err != nil {
		return cloud.StackInfo{}, fmt.Errorf("fleet: decode instance metadata: %w", err)
	}
	return stack, nil
}
