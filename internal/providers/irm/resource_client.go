package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/retry"
)

type irmResourceClient struct {
	httpClient *http.Client
	endpoint   string
}

func newIRMResourceClient(httpClient *http.Client, endpoint string) *irmResourceClient {
	return &irmResourceClient{httpClient: httpClient, endpoint: endpoint}
}

func post[Resp any](ctx context.Context, c *irmResourceClient, path string, body any, mapHTTPError func(*http.Response, string) error) (*Resp, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("irm: marshal request: %w", err)
	}
	return do[Resp](ctx, c, http.MethodPost, path, nil, bytes.NewReader(encoded), mapHTTPError)
}

func get[Resp any](ctx context.Context, c *irmResourceClient, path string, query url.Values) (*Resp, error) {
	return do[Resp](ctx, c, http.MethodGet, path, query, nil, mapOpMemHTTPError)
}

func do[Resp any](ctx context.Context, c *irmResourceClient, method, path string, query url.Values, body io.Reader, mapHTTPError func(*http.Response, string) error) (*Resp, error) {
	if method == http.MethodPost {
		ctx = retry.WithoutRetries(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, method, pathWithParams(c.endpoint+path, query), body)
	if err != nil {
		return nil, fmt.Errorf("irm: build request: %w", err)
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
