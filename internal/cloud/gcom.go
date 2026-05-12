// Package cloud provides clients for Grafana Cloud platform APIs.
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/retry"
)

const (
	instancesPath    = "/api/instances/"
	stackRegionsPath = "/api/stack-regions"
	orgsPath         = "/api/orgs/"
)

// StackInfo holds the information about a Grafana Cloud stack as returned by the GCOM API.
type StackInfo struct {
	ID         int    `json:"id"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	OrgID      int    `json:"orgId"`
	OrgSlug    string `json:"orgSlug"`
	Status     string `json:"status"`
	RegionSlug string `json:"regionSlug"`

	// Stack metadata (returned by list/create/update/get).
	Type             string            `json:"type,omitempty"`
	Description      string            `json:"description,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	DeleteProtection bool              `json:"deleteProtection,omitempty"`
	Plan             string            `json:"plan,omitempty"`
	PlanName         string            `json:"planName,omitempty"`
	ClusterSlug      string            `json:"clusterSlug,omitempty"`
	RunningVersion   string            `json:"runningVersion,omitempty"`
	CreatedAt        string            `json:"createdAt,omitempty"`
	CreatedBy        string            `json:"createdBy,omitempty"`
	UpdatedAt        string            `json:"updatedAt,omitempty"`
	UpdatedBy        string            `json:"updatedBy,omitempty"`

	// Prometheus (Hosted Metrics)
	HMInstancePromID        int    `json:"hmInstancePromId"`
	HMInstancePromURL       string `json:"hmInstancePromUrl"`
	HMInstancePromClusterID int    `json:"hmInstancePromClusterId"`

	// Loki (Hosted Logs)
	HLInstanceID  int    `json:"hlInstanceId"`
	HLInstanceURL string `json:"hlInstanceUrl"`

	// Tempo (Hosted Traces)
	HTInstanceID  int    `json:"htInstanceId"`
	HTInstanceURL string `json:"htInstanceUrl"`

	// Pyroscope (Hosted Profiles)
	HPInstanceID  int    `json:"hpInstanceId"`
	HPInstanceURL string `json:"hpInstanceUrl"`

	// Fleet Management (Agent Management)
	AgentManagementInstanceID  int    `json:"agentManagementInstanceId"`
	AgentManagementInstanceURL string `json:"agentManagementInstanceUrl"`

	// Alertmanager
	AMInstanceID  int    `json:"amInstanceId"`
	AMInstanceURL string `json:"amInstanceUrl"`
}

// Region describes a Grafana Cloud stack region as returned by the GCOM API.
type Region struct {
	ID          int    `json:"id"`
	Status      string `json:"status"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Provider    string `json:"provider"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

// CreateStackRequest is the request body for creating a new Grafana Cloud stack.
type CreateStackRequest struct {
	Name             string            `json:"name"`
	Slug             string            `json:"slug"`
	URL              string            `json:"url,omitempty"`
	Region           string            `json:"region,omitempty"`
	Description      string            `json:"description,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	DeleteProtection *bool             `json:"deleteProtection,omitempty"`
}

// UpdateStackRequest is the request body for updating a Grafana Cloud stack.
type UpdateStackRequest struct {
	Name             string            `json:"name,omitempty"`
	Description      *string           `json:"description,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	DeleteProtection *bool             `json:"deleteProtection,omitempty"`
}

// GCOMHTTPError is returned by GCOMClient when the GCOM API responds with a
// non-200 status. Callers can use errors.As to inspect Status and dispatch on
// 401/403/404 etc. without parsing the error message.
type GCOMHTTPError struct {
	Status int
	Body   string
}

func (e *GCOMHTTPError) Error() string {
	return fmt.Sprintf("gcom client: unexpected status %d %s: %s",
		e.Status, http.StatusText(e.Status), e.Body)
}

// GCOMClient is an HTTP client for the Grafana Cloud API (GCOM).
type GCOMClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewGCOMClient returns a new GCOMClient configured to call the given base URL
// using the provided Bearer token.
//
// The client uses a 30-second timeout and will not follow HTTP redirects to a
// different domain than baseURL.
func NewGCOMClient(baseURL, token string) (*GCOMClient, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("gcom client: invalid base URL %q: %w", baseURL, err)
	}

	if parsedBase.Scheme != "https" && !isLoopbackHost(parsedBase.Hostname()) {
		return nil, fmt.Errorf("gcom client: base URL must use HTTPS (got %q)", parsedBase.Scheme)
	}

	httpClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &httputils.UserAgentTransport{Base: &retry.Transport{}},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Host != parsedBase.Host {
				return fmt.Errorf("gcom client: refusing cross-domain redirect to %s (configured base: %s)",
					req.URL.Host, parsedBase.Host)
			}
			return nil
		},
	}

	return &GCOMClient{
		baseURL: baseURL,
		token:   token,
		http:    httpClient,
	}, nil
}

// GetStack calls GET /api/instances/{slug} on the GCOM API and returns the
// corresponding StackInfo. It returns an error if the response status is not 200.
func (c *GCOMClient) GetStack(ctx context.Context, slug string) (StackInfo, error) {
	endpoint, err := c.buildURL(instancesPath + url.PathEscape(slug))
	if err != nil {
		return StackInfo{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return StackInfo{}, &GCOMHTTPError{
			Status: resp.StatusCode,
			Body:   strings.TrimSpace(string(body)),
		}
	}

	var info StackInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: decode response: %w", err)
	}

	return info, nil
}

// ListStacks calls GET /api/orgs/{orgSlug}/instances on the GCOM API and
// returns the stacks belonging to the given organisation.
func (c *GCOMClient) ListStacks(ctx context.Context, orgSlug string) ([]StackInfo, error) {
	endpoint, err := c.buildURL(orgsPath + url.PathEscape(orgSlug) + "/instances")
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("gcom client: create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gcom client: do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gcom client: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &GCOMHTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	var envelope struct {
		Items []StackInfo `json:"items"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("gcom client: decode response: %w", err)
	}
	return envelope.Items, nil
}

// CreateStack calls POST /api/instances on the GCOM API to create a new stack.
func (c *GCOMClient) CreateStack(ctx context.Context, r CreateStackRequest) (StackInfo, error) {
	payload, err := json.Marshal(r)
	if err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: marshal request: %w", err)
	}

	endpoint, err := c.buildURL(strings.TrimRight(instancesPath, "/"))
	if err != nil {
		return StackInfo{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: create request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return StackInfo{}, &GCOMHTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	var info StackInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: decode response: %w", err)
	}
	return info, nil
}

// UpdateStack calls POST /api/instances/{slug} on the GCOM API to update a stack.
func (c *GCOMClient) UpdateStack(ctx context.Context, slug string, r UpdateStackRequest) (StackInfo, error) {
	payload, err := json.Marshal(r)
	if err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: marshal request: %w", err)
	}

	endpoint, err := c.buildURL(instancesPath + url.PathEscape(slug))
	if err != nil {
		return StackInfo{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: create request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return StackInfo{}, &GCOMHTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	var info StackInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: decode response: %w", err)
	}
	return info, nil
}

// DeleteStack calls DELETE /api/instances/{slug} on the GCOM API.
// Returns a GCOMHTTPError with Status 409 when delete protection is enabled.
func (c *GCOMClient) DeleteStack(ctx context.Context, slug string) error {
	endpoint, err := c.buildURL(instancesPath + url.PathEscape(slug))
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("gcom client: create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gcom client: do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("gcom client: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &GCOMHTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	return nil
}

// ListRegions calls GET /api/stack-regions on the GCOM API and returns
// the available regions for stack creation.
func (c *GCOMClient) ListRegions(ctx context.Context) ([]Region, error) {
	endpoint, err := c.buildURL(stackRegionsPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("gcom client: create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gcom client: do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gcom client: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &GCOMHTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	var envelope struct {
		Items []Region `json:"items"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("gcom client: decode response: %w", err)
	}
	return envelope.Items, nil
}

// buildURL constructs a full URL from the base and the given raw path.
// The rawPath must already be percent-encoded where necessary.
// Both Path (decoded) and RawPath (encoded) are set so url.URL.String()
// preserves the encoded form rather than re-encoding.
func (c *GCOMClient) buildURL(rawPath string) (string, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("gcom client: parse base URL: %w", err)
	}
	endpoint := *base
	endpoint.RawPath = base.EscapedPath() + rawPath
	decoded, err := url.PathUnescape(rawPath)
	if err != nil {
		decoded = rawPath
	}
	endpoint.Path = base.Path + decoded
	return endpoint.String(), nil
}

// setHeaders sets the standard Authorization and Accept headers on a request.
func (c *GCOMClient) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
}

// isLoopbackHost reports whether host refers to the loopback interface.
// It accepts bare hostnames (e.g. "localhost", "127.0.0.1", "::1") as
// returned by url.URL.Hostname() — port stripping and IPv6 bracket removal
// are already handled by that method.
//
// NOTE: a similar (broader) helper exists in internal/login.isLocalHostname.
// The two packages cannot import each other (cycle), so this narrower copy
// lives here. A refactor to a shared package is left as a follow-up.
func isLoopbackHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	if strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
