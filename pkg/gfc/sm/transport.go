package sm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/grafana/gcx/pkg/gfc"
)

const (
	maxResponseBytes = 50 << 20
	smProxyRoute     = "sm"
)

// FallbackLoader resolves direct SM API credentials lazily.
type FallbackLoader interface {
	LoadSMConfig(ctx context.Context) (baseURL, token string, err error)
}

// TransportConfig configures the dual-mode SM transport.
type TransportConfig struct {
	// HTTPClient is an authenticated HTTP client for Grafana API requests
	// (the proxy path). It carries the caller's Grafana credential.
	HTTPClient *http.Client

	// DirectHTTPClient is used for direct SM API requests (the fallback path).
	// It must NOT carry the Grafana credential — the SM bearer token is set
	// per-request by the transport. When nil, a plain http.DefaultClient is
	// used. Callers whose HTTPClient uses an oauth2.Transport or similar
	// auth-injecting round-tripper MUST set this to avoid leaking the Grafana
	// credential to the SM API host.
	DirectHTTPClient *http.Client

	// GrafanaHost is the base URL of the Grafana instance (e.g. "https://mystack.grafana.net").
	GrafanaHost string

	// DatasourceUID is the UID of the SM datasource for proxy mode.
	// When empty, proxy mode is disabled and only direct mode is used.
	DatasourceUID string

	// Fallback resolves direct SM API credentials on demand.
	// When nil, the direct path is disabled and only proxy mode is used.
	Fallback FallbackLoader

	// ClientID is sent as X-Client-Id and classifies the caller in the SM API's
	// request logs. It must match an entry in sm-api's allowedClientTypes;
	// any other value is logged as client_type="unknown". Defaults to "gfc".
	//
	// The User-Agent is not a substitute: Grafana's datasource proxy
	// unconditionally overwrites it with "Grafana/<version>".
	ClientID string

	// ClientVersion is sent as X-Client-Version.
	ClientVersion string
}

// Transport implements [gfc.Transport] with a dual-mode strategy:
//  1. Primary: Grafana datasource proxy (carries caller's Grafana credential)
//  2. Fallback: Direct SM API with bearer token (on proxy 403 or no datasource UID)
type Transport struct {
	httpClient       *http.Client
	directHTTPClient *http.Client
	grafanaHost      string
	datasourceUID    string
	fallback         FallbackLoader
	clientID         string
	clientVersion    string

	directOnce sync.Once
	directBase string
	directTok  string
	directErr  error
}

// Ensure Transport satisfies the interface.
var _ gfc.Transport = (*Transport)(nil)

// NewTransport creates a dual-mode SM transport.
func NewTransport(cfg TransportConfig) (*Transport, error) {
	if cfg.HTTPClient == nil {
		return nil, errors.New("sm: HTTPClient is required")
	}
	clientID := cfg.ClientID
	if clientID == "" {
		clientID = "gfc"
	}
	directClient := cfg.DirectHTTPClient
	if directClient == nil {
		directClient = http.DefaultClient
	}
	return &Transport{
		httpClient:       cfg.HTTPClient,
		directHTTPClient: directClient,
		grafanaHost:      strings.TrimRight(cfg.GrafanaHost, "/"),
		datasourceUID:    cfg.DatasourceUID,
		fallback:         cfg.Fallback,
		clientID:         clientID,
		clientVersion:    cfg.ClientVersion,
	}, nil
}

// Do executes a request against the SM API, trying the proxy first and falling
// back to the direct API on a 403 or when no datasource UID is configured.
func (t *Transport) Do(ctx context.Context, method, smPath string, body []byte) (int, []byte, error) {
	if t.datasourceUID != "" {
		status, respBody, err := t.proxyDo(ctx, method, smPath, body)
		if err != nil {
			return 0, nil, err
		}
		if status != http.StatusForbidden {
			return status, respBody, nil
		}
	}
	return t.directDo(ctx, method, smPath, body)
}

func (t *Transport) proxyDo(ctx context.Context, method, smPath string, body []byte) (int, []byte, error) {
	url := fmt.Sprintf("%s/api/datasources/proxy/uid/%s/%s/%s",
		t.grafanaHost, t.datasourceUID, smProxyRoute, strings.TrimPrefix(smPath, "/"))

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return 0, nil, fmt.Errorf("creating proxy request: %w", err)
	}
	t.setHeaders(req.Header, body != nil)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("executing proxy request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return 0, nil, fmt.Errorf("reading proxy response: %w", err)
	}
	if int64(len(respBody)) > maxResponseBytes {
		return 0, nil, fmt.Errorf("response body exceeds %d MB limit", int64(maxResponseBytes)>>20)
	}

	return resp.StatusCode, respBody, nil
}

func (t *Transport) directDo(ctx context.Context, method, smPath string, body []byte) (int, []byte, error) {
	if err := t.ensureDirect(ctx); err != nil {
		return 0, nil, err
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	url := t.directBase + "/" + strings.TrimPrefix(smPath, "/")
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return 0, nil, fmt.Errorf("creating direct request: %w", err)
	}
	t.setHeaders(req.Header, body != nil)
	req.Header.Set("Authorization", "Bearer "+t.directTok)

	resp, err := t.directHTTPClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("executing direct request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("reading direct response: %w", err)
	}

	return resp.StatusCode, respBody, nil
}

func (t *Transport) ensureDirect(ctx context.Context) error {
	if t.fallback == nil {
		return errors.New("synthetic-monitoring: datasource proxy unavailable and no direct SM API fallback configured")
	}
	t.directOnce.Do(func() {
		baseURL, token, err := t.fallback.LoadSMConfig(ctx)
		if err != nil {
			t.directErr = fmt.Errorf("resolving direct SM API credentials: %w", err)
			return
		}
		t.directBase = strings.TrimRight(baseURL, "/") + "/api/v1"
		t.directTok = token
	})
	return t.directErr
}

func (t *Transport) setHeaders(h http.Header, hasBody bool) {
	h.Set("X-Client-Id", t.clientID)
	if t.clientVersion != "" {
		h.Set("X-Client-Version", t.clientVersion)
	}
	if hasBody {
		h.Set("Content-Type", "application/json")
	}
}
