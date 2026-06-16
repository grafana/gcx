package checks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	querysynth "github.com/grafana/gcx/internal/query/synth"
)

// ErrNotFound is returned when a requested check does not exist (HTTP 404).
var ErrNotFound = errors.New("check not found")

// SM API paths, relative to the SM API v1 root. They are forwarded verbatim by
// the datasource-proxy `sm` route and prefixed with /api/v1 on the direct path.
const (
	checkListPath      = "check/list"
	checkAddPath       = "check/add"
	checkUpdatePath    = "check/update"
	checkByIDPathFmt   = "check/%d"
	checkDeletePathFmt = "check/delete/%d"
	tenantPath         = "tenant"
	probeListPath      = "probe/list"
)

// fallbackLoader resolves direct SM-API credentials for the fallback transport.
// It is invoked lazily, only when the datasource proxy denies access (HTTP 403)
// or no SM datasource UID is available. smcfg.Loader satisfies it.
type fallbackLoader interface {
	LoadSMConfig(ctx context.Context) (baseURL, token, namespace string, err error)
}

// Client is a dual-mode client for the Synthetic Monitoring checks API. It
// prefers Grafana's datasource proxy (the caller's Grafana credential, SM token
// injected server-side) and falls back to the direct SM API + token on a proxy
// 403, or when no SM datasource UID could be resolved.
type Client struct {
	proxy         *querysynth.Client
	datasourceUID string
	fallback      fallbackLoader

	directOnce sync.Once
	directHTTP *http.Client
	directBase string
	directTok  string
	directErr  error
}

// NewClient creates a dual-mode SM checks client.
//
// When datasourceUID is non-empty, requests go through the Grafana datasource
// proxy built from restCfg (carrying the caller's Grafana credential). On a
// proxy 403 — or when datasourceUID is empty — requests fall back to the direct
// SM API, with credentials resolved lazily via fallback.LoadSMConfig. A nil
// fallback disables the direct path.
func NewClient(restCfg config.NamespacedRESTConfig, datasourceUID string, fallback fallbackLoader) (*Client, error) {
	var proxy *querysynth.Client
	if datasourceUID != "" {
		p, err := querysynth.NewClient(restCfg)
		if err != nil {
			return nil, fmt.Errorf("creating SM datasource-proxy client: %w", err)
		}
		proxy = p
	}

	return &Client{
		proxy:         proxy,
		datasourceUID: datasourceUID,
		fallback:      fallback,
	}, nil
}

// List returns all checks for the authenticated tenant.
func (c *Client) List(ctx context.Context) ([]Check, error) {
	status, body, err := c.do(ctx, http.MethodGet, checkListPath, nil)
	if err != nil {
		return nil, fmt.Errorf("listing checks: %w", err)
	}
	if status != http.StatusOK {
		return nil, smcfg.HandleErrorBody(status, body)
	}

	var checks []Check
	if err := json.Unmarshal(body, &checks); err != nil {
		return nil, fmt.Errorf("decoding check list: %w", err)
	}

	if checks == nil {
		return []Check{}, nil
	}

	return checks, nil
}

// Get returns a single check by ID.
func (c *Client) Get(ctx context.Context, id int64) (*Check, error) {
	status, body, err := c.do(ctx, http.MethodGet, fmt.Sprintf(checkByIDPathFmt, id), nil)
	if err != nil {
		return nil, fmt.Errorf("getting check %d: %w", id, err)
	}

	if status == http.StatusNotFound {
		return nil, ErrNotFound
	}

	if status != http.StatusOK {
		return nil, smcfg.HandleErrorBody(status, body)
	}

	var check Check
	if err := json.Unmarshal(body, &check); err != nil {
		return nil, fmt.Errorf("decoding check: %w", err)
	}

	return &check, nil
}

// Create creates a new check. The Check must not have ID or TenantID set.
func (c *Client) Create(ctx context.Context, check Check) (*Check, error) {
	reqBody, err := json.Marshal(check)
	if err != nil {
		return nil, fmt.Errorf("marshalling check: %w", err)
	}

	status, body, err := c.do(ctx, http.MethodPost, checkAddPath, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating check: %w", err)
	}

	if status != http.StatusOK && status != http.StatusCreated {
		return nil, smcfg.HandleErrorBody(status, body)
	}

	var created Check
	if err := json.Unmarshal(body, &created); err != nil {
		return nil, fmt.Errorf("decoding created check: %w", err)
	}

	return &created, nil
}

// Update updates an existing check. The Check must have ID and TenantID set.
func (c *Client) Update(ctx context.Context, check Check) (*Check, error) {
	reqBody, err := json.Marshal(check)
	if err != nil {
		return nil, fmt.Errorf("marshalling check: %w", err)
	}

	status, body, err := c.do(ctx, http.MethodPost, checkUpdatePath, reqBody)
	if err != nil {
		return nil, fmt.Errorf("updating check %d: %w", check.ID, err)
	}

	if status != http.StatusOK {
		return nil, smcfg.HandleErrorBody(status, body)
	}

	var updated Check
	if err := json.Unmarshal(body, &updated); err != nil {
		return nil, fmt.Errorf("decoding updated check: %w", err)
	}

	return &updated, nil
}

// Delete deletes a check by ID.
func (c *Client) Delete(ctx context.Context, id int64) error {
	status, body, err := c.do(ctx, http.MethodDelete, fmt.Sprintf(checkDeletePathFmt, id), nil)
	if err != nil {
		return fmt.Errorf("deleting check %d: %w", id, err)
	}

	if status != http.StatusOK && status != http.StatusNoContent {
		return smcfg.HandleErrorBody(status, body)
	}

	return nil
}

// GetTenant returns the SM tenant info (used to obtain tenantId for push).
func (c *Client) GetTenant(ctx context.Context) (*Tenant, error) {
	status, body, err := c.do(ctx, http.MethodGet, tenantPath, nil)
	if err != nil {
		return nil, fmt.Errorf("getting tenant: %w", err)
	}

	if status != http.StatusOK {
		return nil, smcfg.HandleErrorBody(status, body)
	}

	var tenant Tenant
	if err := json.Unmarshal(body, &tenant); err != nil {
		return nil, fmt.Errorf("decoding tenant: %w", err)
	}

	return &tenant, nil
}

// ListProbes returns a minimal list of probes for name/ID resolution.
func (c *Client) ListProbes(ctx context.Context) ([]ProbeRef, error) {
	status, body, err := c.do(ctx, http.MethodGet, probeListPath, nil)
	if err != nil {
		return nil, fmt.Errorf("listing probes: %w", err)
	}

	if status != http.StatusOK {
		return nil, smcfg.HandleErrorBody(status, body)
	}

	var raw []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decoding probe list: %w", err)
	}

	probes := make([]ProbeRef, len(raw))
	for i, p := range raw {
		probes[i] = ProbeRef{ID: p.ID, Name: p.Name}
	}

	return probes, nil
}

// do issues an SM request, preferring the datasource proxy and falling back to
// the direct SM API on a proxy 403 (the OAuth-scope-gap signal) or when no SM
// datasource UID is available. It returns the SM API status code and body so
// callers keep their existing status-based decoding.
func (c *Client) do(ctx context.Context, method, smPath string, body []byte) (int, []byte, error) {
	if c.proxy != nil && c.datasourceUID != "" {
		resp, err := c.proxyDo(ctx, method, smPath, body)
		if err != nil {
			return 0, nil, err
		}
		if resp.StatusCode != http.StatusForbidden {
			return resp.StatusCode, resp.Body, nil
		}
		// Proxy denied access (403) — fall through to the direct SM API.
	}

	return c.directDo(ctx, method, smPath, body)
}

func (c *Client) proxyDo(ctx context.Context, method, smPath string, body []byte) (*querysynth.Response, error) {
	switch method {
	case http.MethodGet:
		return c.proxy.Get(ctx, c.datasourceUID, smPath)
	case http.MethodPost:
		return c.proxy.Post(ctx, c.datasourceUID, smPath, body)
	case http.MethodDelete:
		return c.proxy.Delete(ctx, c.datasourceUID, smPath)
	default:
		return nil, fmt.Errorf("unsupported method %q", method)
	}
}

func (c *Client) directDo(ctx context.Context, method, smPath string, body []byte) (int, []byte, error) {
	if err := c.ensureDirect(ctx); err != nil {
		return 0, nil, err
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	url := c.directBase + "/" + strings.TrimPrefix(smPath, "/")
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return 0, nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.directTok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.directHTTP.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("reading response: %w", err)
	}

	return resp.StatusCode, respBody, nil
}

// ensureDirect lazily resolves direct SM-API credentials once, the first time
// the fallback path is taken.
func (c *Client) ensureDirect(ctx context.Context) error {
	if c.fallback == nil {
		return errors.New("synthetic-monitoring: datasource proxy unavailable and no direct SM API fallback configured")
	}

	c.directOnce.Do(func() {
		baseURL, token, _, err := c.fallback.LoadSMConfig(ctx)
		if err != nil {
			c.directErr = fmt.Errorf("resolving direct SM API credentials: %w", err)
			return
		}
		c.directBase = strings.TrimRight(baseURL, "/") + "/api/v1"
		c.directTok = token
		c.directHTTP = httputils.NewDefaultClient(ctx)
	})

	return c.directErr
}
