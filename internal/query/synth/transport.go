package synth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
)

// FallbackLoader resolves direct SM-API credentials for the fallback transport.
// It is invoked lazily, only when the datasource proxy denies access (HTTP 403)
// or no SM datasource UID is available. smcfg.Loader satisfies it.
type FallbackLoader interface {
	LoadSMConfig(ctx context.Context) (baseURL, token, namespace string, err error)
}

// Transport is the dual-mode SM transport: it prefers Grafana's datasource proxy
// (the caller's Grafana credential, SM token injected server-side) and falls back
// to the direct SM API + token on a proxy 403, or when no SM datasource UID could
// be resolved. See docs/adrs/sm-datasource-proxy/001-dual-mode-transport.md.
//
// It is deliberately free of SM domain types (Check, Probe, …): it speaks only
// status codes and raw bodies, so the typed clients in internal/providers/synth
// own decoding and error mapping.
type Transport struct {
	proxy         *Client
	datasourceUID string
	fallback      FallbackLoader

	directOnce sync.Once
	directHTTP *http.Client
	directBase string
	directTok  string
	directErr  error
}

// NewTransport creates a dual-mode SM transport.
//
// When datasourceUID is non-empty, requests go through the Grafana datasource
// proxy built from restCfg (carrying the caller's Grafana credential). On a
// proxy 403 — or when datasourceUID is empty — requests fall back to the direct
// SM API, with credentials resolved lazily via fallback.LoadSMConfig. A nil
// fallback disables the direct path.
func NewTransport(restCfg config.NamespacedRESTConfig, datasourceUID string, fallback FallbackLoader) (*Transport, error) {
	var proxy *Client
	if datasourceUID != "" {
		p, err := NewClient(restCfg)
		if err != nil {
			return nil, fmt.Errorf("creating SM datasource-proxy client: %w", err)
		}
		proxy = p
	}

	return &Transport{
		proxy:         proxy,
		datasourceUID: datasourceUID,
		fallback:      fallback,
	}, nil
}

// Do issues an SM request, preferring the datasource proxy and falling back to
// the direct SM API on a proxy 403 (the OAuth-scope-gap signal) or when no SM
// datasource UID is available. It returns the SM API status code and body so
// callers keep their status-based decoding.
func (t *Transport) Do(ctx context.Context, method, smPath string, body []byte) (int, []byte, error) {
	if t.proxy != nil && t.datasourceUID != "" {
		resp, err := t.proxyDo(ctx, method, smPath, body)
		if err != nil {
			return 0, nil, err
		}
		if resp.StatusCode != http.StatusForbidden {
			return resp.StatusCode, resp.Body, nil
		}
		// Proxy denied access (403) — fall through to the direct SM API.
	}

	return t.directDo(ctx, method, smPath, body)
}

func (t *Transport) proxyDo(ctx context.Context, method, smPath string, body []byte) (*Response, error) {
	switch method {
	case http.MethodGet:
		return t.proxy.Get(ctx, t.datasourceUID, smPath)
	case http.MethodPost:
		return t.proxy.Post(ctx, t.datasourceUID, smPath, body)
	case http.MethodDelete:
		return t.proxy.Delete(ctx, t.datasourceUID, smPath)
	default:
		return nil, fmt.Errorf("unsupported method %q", method)
	}
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
		return 0, nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+t.directTok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := t.directHTTP.Do(req)
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
func (t *Transport) ensureDirect(ctx context.Context) error {
	if t.fallback == nil {
		return errors.New("synthetic-monitoring: datasource proxy unavailable and no direct SM API fallback configured")
	}

	t.directOnce.Do(func() {
		baseURL, token, _, err := t.fallback.LoadSMConfig(ctx)
		if err != nil {
			t.directErr = fmt.Errorf("resolving direct SM API credentials: %w", err)
			return
		}
		t.directBase = strings.TrimRight(baseURL, "/") + "/api/v1"
		t.directTok = token
		t.directHTTP = httputils.NewDefaultClient(ctx)
	})

	return t.directErr
}
