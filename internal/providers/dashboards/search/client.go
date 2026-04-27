package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

// searchAPIVersion is pinned to v0alpha1 — the only Grafana API version that
// exposes the full-text search endpoint. This is intentionally hardcoded;
// there is no version-negotiation mechanism for this endpoint.
// See ADR 001 §Search command for the v0alpha1 exception.
const searchAPIVersion = "v0alpha1"

// searchAPIGroup is the Grafana dashboard API group.
const searchAPIGroup = "dashboard.grafana.app"

// searchClient is a thin HTTP client for the Grafana dashboard search API.
// It uses the REST config transport (auth, TLS, retry inherited from the
// k8s.io/client-go pipeline) and calls:
//
//	GET /apis/dashboard.grafana.app/v0alpha1/namespaces/{ns}/search
type searchClient struct {
	httpClient *http.Client
	baseURL    string
	namespace  string
}

// newSearchClient creates a searchClient from the given NamespacedRESTConfig.
// The HTTP client inherits auth, TLS, and retry transports from rest.HTTPClientFor.
func newSearchClient(cfg config.NamespacedRESTConfig) (*searchClient, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client for search: %w", err)
	}

	return &searchClient{
		httpClient: httpClient,
		baseURL:    strings.TrimSuffix(cfg.Host, "/"),
		namespace:  cfg.Namespace,
	}, nil
}

// Search calls the Grafana dashboard search API and returns the parsed response.
//
// The endpoint URL is constructed as:
//
//	{host}/apis/dashboard.grafana.app/v0alpha1/namespaces/{namespace}/search
//
// Query parameters: query, folder (repeatable), tag (repeatable), limit, sort, deleted.
// Note: the "type" parameter is intentionally NOT sent — the server silently
// ignores it and client-side filtering on the "resource" field is used instead.
func (c *searchClient) Search(ctx context.Context, params SearchParams) (*wireSearchResponse, error) {
	u, err := url.Parse(fmt.Sprintf(
		"%s/apis/%s/%s/namespaces/%s/search",
		c.baseURL, searchAPIGroup, searchAPIVersion, url.PathEscape(c.namespace),
	))
	if err != nil {
		return nil, fmt.Errorf("failed to build search URL: %w", err)
	}

	q := u.Query()
	if params.Query != "" {
		q.Set("query", params.Query)
	}
	for _, folder := range params.Folders {
		q.Add("folder", folder)
	}
	for _, tag := range params.Tags {
		q.Add("tag", tag)
	}
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Sort != "" {
		q.Set("sort", params.Sort)
	}
	if params.Deleted {
		q.Set("deleted", "true")
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create search request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("search request failed: %s: %s", resp.Status, strings.TrimSpace(string(bodyBytes)))
	}

	var result wireSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)

	return &result, nil
}
