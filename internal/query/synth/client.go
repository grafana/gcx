// Package synth provides a low-level transport for the Synthetic Monitoring
// API reached through Grafana's datasource proxy.
//
// It builds /api/datasources/proxy/uid/<uid>/sm/<path> requests and executes
// them with the caller's Grafana credential (SAT or OAuth gat_), supplied via
// the rest.Config. The SM API token is injected server-side by the plugin's
// `sm` proxy route, so callers never handle it on this path — that is the
// migration's core win over the direct SM API transport.
//
// This package is deliberately free of SM domain types (Check, Probe, …): the
// typed clients in internal/providers/synth own those and decode the raw
// Response bodies, keeping the dependency direction providers -> query.
package synth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/version"
	"k8s.io/client-go/rest"
)

// maxResponseBytes caps how much of a proxied response we read into memory,
// matching the other datasource query clients (loki, pyroscope, …).
const maxResponseBytes = 50 << 20 // 50 MB

// smRoute is the datasource-proxy route prefix the SM plugin forwards to the SM
// API ({{.JsonData.apiHost}}/api/v1/). A logical SM path like "check/list" is
// reached at "<proxy>/sm/check/list".
const smRoute = "sm"

// The SM API classifies callers in its request logs (the "mux" subsystem) by the
// X-Client-ID / X-Client-Version headers — NOT the User-Agent, which Grafana's
// datasource proxy unconditionally overwrites with "Grafana/<version>".
//
// clientIDValue must match an entry in sm-api's allowedClientTypes; any other
// value is logged as client_type="unknown". Both headers survive the assistant
// CLI proxy and datasource proxy hops intact (verified end-to-end).
const (
	clientIDHeader      = "X-Client-Id"
	clientVersionHeader = "X-Client-Version"
	clientIDValue       = "gcx"
)

// setClientIdentity stamps gcx's SM-API client identity onto a request so the SM
// API attributes it as client_type="gcx" rather than "unknown". Applied on both
// the datasource-proxy and direct SM-API request paths.
func setClientIdentity(h http.Header) {
	h.Set(clientIDHeader, clientIDValue)
	h.Set(clientVersionHeader, version.Get())
}

// Response is the outcome of a proxied SM request. Non-2xx statuses are
// returned here (not as an error) so callers can inspect StatusCode and decide
// whether to fall back to the direct SM API.
type Response struct {
	StatusCode int
	Body       []byte
}

// Client is a transport for the Synthetic Monitoring API via Grafana's
// datasource proxy.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new SM datasource-proxy transport. The HTTP client is
// built from the rest.Config so it carries the caller's Grafana credential.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &Client{
		restConfig: cfg,
		httpClient: httpClient,
	}, nil
}

// Get performs a GET against the SM API path (e.g. "check/list") via the proxy.
func (c *Client) Get(ctx context.Context, datasourceUID, smPath string) (*Response, error) {
	return c.do(ctx, http.MethodGet, datasourceUID, smPath, nil)
}

// Post performs a POST against the SM API path (e.g. "check/add") via the proxy.
// The body is sent as application/json.
func (c *Client) Post(ctx context.Context, datasourceUID, smPath string, body []byte) (*Response, error) {
	return c.do(ctx, http.MethodPost, datasourceUID, smPath, body)
}

// Delete performs a DELETE against the SM API path (e.g. "check/delete/42") via
// the proxy.
func (c *Client) Delete(ctx context.Context, datasourceUID, smPath string) (*Response, error) {
	return c.do(ctx, http.MethodDelete, datasourceUID, smPath, nil)
}

func (c *Client) do(ctx context.Context, method, datasourceUID, smPath string, body []byte) (*Response, error) {
	url := c.restConfig.Host + c.buildProxyPath(datasourceUID, smPath)

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	setClientIdentity(req.Header)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if int64(len(respBody)) > maxResponseBytes {
		return nil, fmt.Errorf("synthetic-monitoring response body exceeds %d MB limit", int64(maxResponseBytes)>>20)
	}

	return &Response{StatusCode: resp.StatusCode, Body: respBody}, nil
}

func (c *Client) buildProxyPath(datasourceUID, smPath string) string {
	return fmt.Sprintf("/api/datasources/proxy/uid/%s/%s/%s",
		datasourceUID, smRoute, strings.TrimPrefix(smPath, "/"))
}
