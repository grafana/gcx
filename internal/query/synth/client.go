// Package synth is a thin client for the Grafana Synthetic Monitoring
// datasource proxy at /api/datasources/proxy/uid/<uid>/sm/<path>.
//
// The client exposes only byte-level primitives (ProxyGet, ProxyPost,
// ProxyDelete). Typed marshaling lives in providers/synth/checks and
// providers/synth/probes alongside the SM types — keeping this package
// type-agnostic avoids an import cycle. Long-term, the SM types should
// come from the synthetic-monitoring-agent protobuf definitions.
//
// Authentication follows whatever the underlying NamespacedRESTConfig is
// wired with — a SAT bearer today, OAuth once the assistant-proxy scope
// mapping is fixed (see docs/research/2026-05-04-sm-datasource-oauth-gap.md).
package synth

import (
	"bytes"
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

// ProxyGet performs a GET against /api/datasources/proxy/uid/<uid>/<path>
// and returns the raw response body. The op string is included in errors
// for diagnostics (e.g. "list checks", "get tenant").
func (c *Client) ProxyGet(ctx context.Context, datasourceUID, path, op string) ([]byte, error) {
	return c.proxyDo(ctx, http.MethodGet, datasourceUID, path, nil, op)
}

// ProxyPost performs a POST with a JSON body. Accepts 200 OK and 201 Created
// as success.
func (c *Client) ProxyPost(ctx context.Context, datasourceUID, path string, body []byte, op string) ([]byte, error) {
	return c.proxyDo(ctx, http.MethodPost, datasourceUID, path, body, op)
}

// ProxyDelete performs a DELETE. Accepts 200 OK and 204 No Content as success.
func (c *Client) ProxyDelete(ctx context.Context, datasourceUID, path, op string) ([]byte, error) {
	return c.proxyDo(ctx, http.MethodDelete, datasourceUID, path, nil, op)
}

func (c *Client) proxyDo(ctx context.Context, method, datasourceUID, path string, body []byte, op string) ([]byte, error) {
	fullURL := c.host + "/api/datasources/proxy/uid/" + url.PathEscape(datasourceUID) + "/" + path

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if !isSuccess(method, resp.StatusCode) {
		return nil, queryerror.FromBody(datasourceKind, op, resp.StatusCode, respBody)
	}
	return respBody, nil
}

func isSuccess(method string, status int) bool {
	switch method {
	case http.MethodPost:
		return status == http.StatusOK || status == http.StatusCreated
	case http.MethodDelete:
		return status == http.StatusOK || status == http.StatusNoContent
	default:
		return status == http.StatusOK
	}
}
