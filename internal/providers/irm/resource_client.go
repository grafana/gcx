// Shared authenticated HTTP transport for the independent IRM resource APIs.
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// irmResourceClient talks to the model and message APIs through authenticated
// Grafana plugin resources. The proxy derives tenancy from each request.
type irmResourceClient struct {
	httpClient *http.Client
	endpoint   string // scheme://host[:port], no trailing slash
}

func newIRMResourceClient(httpClient *http.Client, endpoint string) *irmResourceClient {
	return &irmResourceClient{httpClient: httpClient, endpoint: endpoint}
}

// post sends a command body and decodes the response. Every op model command
// is a POST to a subresource, and every one of them returns a small JSON
// document carrying the new revision.
func post[Resp any](ctx context.Context, c *irmResourceClient, path string, body any) (*Resp, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("irm: marshal request: %w", err)
	}
	return do[Resp](ctx, c, http.MethodPost, path, nil, bytes.NewReader(encoded), mapOpModelHTTPError)
}

// get reads a resource, with optional query parameters.
func get[Resp any](ctx context.Context, c *irmResourceClient, path string, query url.Values) (*Resp, error) {
	return do[Resp](ctx, c, http.MethodGet, path, query, nil, mapOpModelHTTPError)
}

func do[Resp any](ctx context.Context, c *irmResourceClient, method, path string, query url.Values, body io.Reader, mapHTTPError func(*http.Response, string) error) (*Resp, error) {
	req, err := http.NewRequestWithContext(ctx, method, pathWithParams(c.endpoint+path, query), body)
	if err != nil {
		return nil, fmt.Errorf("irm: build request: %w", err)
	}
	// No operational-model or message mutation has an idempotency key.
	// The shared retry transport only replays bodies with GetBody; disabling it
	// prevents ambiguous POST replay while leaving read retry policy unchanged.
	if method == http.MethodPost {
		req.GetBody = nil
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, mapIRMTransportError(err, c.endpoint)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, mapHTTPError(resp, c.endpoint)
	}

	out := new(Resp)
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, fmt.Errorf("irm: decode %s %s response: %w", method, path, err)
	}
	return out, nil
}
