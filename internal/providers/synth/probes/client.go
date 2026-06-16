package probes

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

// SM API paths, relative to the SM API v1 root. They are forwarded verbatim by
// the datasource-proxy `sm` route and prefixed with /api/v1 on the direct path.
const (
	probeListPath      = "probe/list"
	probeAddPath       = "probe/add"
	probeUpdatePath    = "probe/update"
	probeDeletePathFmt = "probe/delete/%d"
)

// fallbackLoader resolves direct SM-API credentials for the fallback transport.
// It is invoked lazily, only when the datasource proxy denies access (HTTP 403)
// or no SM datasource UID is available. smcfg.Loader satisfies it.
type fallbackLoader interface {
	LoadSMConfig(ctx context.Context) (baseURL, token, namespace string, err error)
}

// Client is a dual-mode client for the Synthetic Monitoring probes API. It
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

// NewClient creates a dual-mode SM probes client.
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

// CreateResponse is the API response from creating a probe, containing the
// created probe and its authentication token.
type CreateResponse struct {
	Probe Probe  `json:"probe"`
	Token string `json:"token"`
}

// updateResponse wraps the probe returned from the update endpoint.
type updateResponse struct {
	Probe Probe `json:"probe"`
}

// List returns all probes visible to the authenticated tenant.
func (c *Client) List(ctx context.Context) ([]Probe, error) {
	status, body, err := c.do(ctx, http.MethodGet, probeListPath, nil)
	if err != nil {
		return nil, fmt.Errorf("listing probes: %w", err)
	}

	if status != http.StatusOK {
		return nil, smcfg.HandleErrorBody(status, body)
	}

	var probeList []Probe
	if err := json.Unmarshal(body, &probeList); err != nil {
		return nil, fmt.Errorf("decoding probe list: %w", err)
	}

	if probeList == nil {
		return []Probe{}, nil
	}

	return probeList, nil
}

// Create creates a new private probe. The probe is sent as flat JSON.
// The response contains the created probe and its authentication token.
func (c *Client) Create(ctx context.Context, probe Probe) (*CreateResponse, error) {
	reqBody, err := json.Marshal(probe)
	if err != nil {
		return nil, fmt.Errorf("marshalling probe: %w", err)
	}

	status, body, err := c.do(ctx, http.MethodPost, probeAddPath, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating probe: %w", err)
	}

	if status != http.StatusOK {
		return nil, smcfg.HandleErrorBody(status, body)
	}

	var created CreateResponse
	if err := json.Unmarshal(body, &created); err != nil {
		return nil, fmt.Errorf("decoding created probe: %w", err)
	}

	return &created, nil
}

// Get returns a single probe by ID. The SM API has no single-probe endpoint,
// so this calls List and filters by ID.
func (c *Client) Get(ctx context.Context, id int64) (*Probe, error) {
	all, err := c.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting probe %d: %w", id, err)
	}

	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}

	return nil, fmt.Errorf("probe %d not found", id)
}

// ResetToken updates a probe with resetToken set to true, causing the API
// to issue a new authentication token for the probe.
// The SM update API expects flat JSON: all probe fields at top level plus resetToken.
func (c *Client) ResetToken(ctx context.Context, probe Probe) (*Probe, error) {
	raw, err := json.Marshal(probe)
	if err != nil {
		return nil, fmt.Errorf("marshalling probe: %w", err)
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("converting probe to map: %w", err)
	}

	m["resetToken"] = true

	reqBody, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshalling update request: %w", err)
	}

	status, body, err := c.do(ctx, http.MethodPost, probeUpdatePath, reqBody)
	if err != nil {
		return nil, fmt.Errorf("resetting probe token %d: %w", probe.ID, err)
	}

	if status != http.StatusOK {
		return nil, smcfg.HandleErrorBody(status, body)
	}

	var updated updateResponse
	if err := json.Unmarshal(body, &updated); err != nil {
		return nil, fmt.Errorf("decoding updated probe: %w", err)
	}

	return &updated.Probe, nil
}

// Delete deletes a probe by ID.
func (c *Client) Delete(ctx context.Context, id int64) error {
	status, body, err := c.do(ctx, http.MethodDelete, fmt.Sprintf(probeDeletePathFmt, id), nil)
	if err != nil {
		return fmt.Errorf("deleting probe %d: %w", id, err)
	}

	if status != http.StatusOK && status != http.StatusNoContent {
		return smcfg.HandleErrorBody(status, body)
	}

	return nil
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
