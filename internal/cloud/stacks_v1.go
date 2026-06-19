package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// The v1 stacks API (GET/POST/DELETE /api/v1/stacks, GET /api/v1/stack-regions)
// is the versioned replacement for the legacy /api/instances and
// /api/stack-regions endpoints. The stacks-api service registers these routes
// internally at /v1/..., but they are exposed behind grafana.com's /api gateway
// prefix (same as the legacy endpoints). Enabled by default, no feature flag.
//
// These methods are kept separate from the legacy GetStack/StackInfo path,
// which the login flow and signal providers depend on for the per-signal
// instance URLs (HMInstancePromURL etc.) that the v1 StackV1 shape omits.
const (
	stacksV1Path       = "/api/v1/stacks"
	stackRegionsV1Path = "/api/v1/stack-regions"

	// stacksV1PageSize is the page size used when paging through the list
	// endpoint to collect all stacks in an org.
	stacksV1PageSize = 100
)

// StackV1 is a Grafana Cloud stack as returned by the /v1/stacks API. It is a
// slimmer shape than StackInfo: it carries identity and metadata only, not the
// per-signal instance URLs returned by the legacy /api/instances endpoint.
type StackV1 struct {
	ID               int               `json:"id"`
	Slug             string            `json:"slug"`
	Name             string            `json:"name"`
	Description      *string           `json:"description"`
	Region           string            `json:"region"`
	OrgID            int               `json:"orgId"`
	OrgSlug          string            `json:"orgSlug"`
	URL              string            `json:"url"`
	DeleteProtection bool              `json:"deleteProtection"`
	Labels           map[string]string `json:"labels"`
}

// RegionV1 is a stack region as returned by the /v1/stack-regions API.
type RegionV1 struct {
	ID          int64   `json:"id"`
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Provider    string  `json:"provider"`
	PublicName  *string `json:"publicName"`
	Visibility  string  `json:"visibility"`
}

// paginatedStacksV1 mirrors the pagination envelope returned by GET /v1/stacks.
type paginatedStacksV1 struct {
	Total    int       `json:"total"`
	Pages    int       `json:"pages"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
	Items    []StackV1 `json:"items"`
}

// ListStacksV1 calls GET /v1/stacks for the given org slug, paging through all
// results so callers get the full set regardless of the server page size.
func (c *GCOMClient) ListStacksV1(ctx context.Context, orgSlug string) ([]StackV1, error) {
	var all []StackV1
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("org", orgSlug)
		q.Set("page", strconv.Itoa(page))
		q.Set("pageSize", strconv.Itoa(stacksV1PageSize))

		var envelope paginatedStacksV1
		if err := c.doJSONV1(ctx, http.MethodGet, stacksV1Path, q, nil, &envelope); err != nil {
			return nil, err
		}

		all = append(all, envelope.Items...)
		if len(envelope.Items) == 0 || len(all) >= envelope.Total || page >= envelope.Pages {
			break
		}
	}
	return all, nil
}

// GetStackV1 calls GET /v1/stacks/{idOrSlug}.
func (c *GCOMClient) GetStackV1(ctx context.Context, idOrSlug string) (StackV1, error) {
	var stack StackV1
	err := c.doJSONV1(ctx, http.MethodGet, stacksV1Path+"/"+url.PathEscape(idOrSlug), nil, nil, &stack)
	return stack, err
}

// CreateStackV1 calls POST /v1/stacks to create a new stack.
func (c *GCOMClient) CreateStackV1(ctx context.Context, r CreateStackRequest) (StackV1, error) {
	var stack StackV1
	err := c.doJSONV1(ctx, http.MethodPost, stacksV1Path, nil, r, &stack)
	return stack, err
}

// UpdateStackV1 calls POST /v1/stacks/{idOrSlug} to update a stack.
func (c *GCOMClient) UpdateStackV1(ctx context.Context, idOrSlug string, r UpdateStackRequest) (StackV1, error) {
	var stack StackV1
	err := c.doJSONV1(ctx, http.MethodPost, stacksV1Path+"/"+url.PathEscape(idOrSlug), nil, r, &stack)
	return stack, err
}

// DeleteStackV1 calls DELETE /v1/stacks/{idOrSlug}.
// Returns a GCOMHTTPError with Status 409 when delete protection is enabled.
func (c *GCOMClient) DeleteStackV1(ctx context.Context, idOrSlug string) error {
	return c.doJSONV1(ctx, http.MethodDelete, stacksV1Path+"/"+url.PathEscape(idOrSlug), nil, nil, nil)
}

// ListRegionsV1 calls GET /v1/stack-regions.
func (c *GCOMClient) ListRegionsV1(ctx context.Context) ([]RegionV1, error) {
	var envelope struct {
		Items []RegionV1 `json:"items"`
	}
	if err := c.doJSONV1(ctx, http.MethodGet, stackRegionsV1Path, nil, nil, &envelope); err != nil {
		return nil, err
	}
	return envelope.Items, nil
}

// doJSONV1 performs a JSON request against the v1 API. rawPath must already be
// percent-encoded. When body is non-nil it is JSON-encoded as the request body.
// When out is non-nil the (200) response body is decoded into it.
func (c *GCOMClient) doJSONV1(ctx context.Context, method, rawPath string, query url.Values, body, out any) error {
	endpoint, err := c.buildURL(rawPath)
	if err != nil {
		return err
	}
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("gcom client: marshal request: %w", err)
		}
		reqBody = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return fmt.Errorf("gcom client: create request: %w", err)
	}
	c.setHeaders(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gcom client: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("gcom client: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &GCOMHTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(respBody))}
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("gcom client: decode response: %w", err)
	}
	return nil
}
