// Package cloud provides clients for Grafana Cloud platform APIs.
package cloud

import (
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
	// Build the endpoint URL, preserving percent-encoding of the slug by setting
	// both Path (decoded) and RawPath (encoded) so that url.URL.String() uses
	// the raw path and does not re-encode or normalise percent-encoded sequences.
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: parse base URL: %w", err)
	}
	endpoint := *base
	endpoint.Path = base.Path + "/api/instances/" + slug
	endpoint.RawPath = base.EscapedPath() + "/api/instances/" + url.PathEscape(slug)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return StackInfo{}, fmt.Errorf("gcom client: create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

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
