package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/httputils"
	"k8s.io/client-go/rest"
)

const (
	renderSoloPanelPathFmt = "/render/d-solo/%s/"
	renderDashboardPathFmt = "/render/d/%s/"
	renderTimeoutOverhead  = 30 * time.Second

	renderErrorExcerptBytes  = 256
	frontendSettingsPath     = "/api/frontend/settings"
	frontendSettingsMaxBytes = 16 << 20
	rendererProbeTimeout     = 10 * time.Second
)

// RenderRequest holds parameters for a dashboard render request.
type RenderRequest struct {
	// UID is the dashboard UID. Required.
	UID string

	// PanelID, if non-zero, renders a single panel via /render/d-solo/.
	PanelID int

	// OrgID is the Grafana organization ID. Defaults to 1.
	OrgID int

	// Width and Height of the rendered image in pixels.
	Width  int
	Height int

	// Theme is "light" or "dark".
	Theme string

	// From and To define the time range. Relative values beginning with "now"
	// are passed through. RFC3339 and Unix-second values are converted to
	// Grafana URL timestamps (Unix milliseconds); Unix-millisecond values are
	// preserved.
	From string
	To   string

	// Tz is the timezone string (e.g. "UTC", "America/New_York").
	Tz string

	// Timeout is the maximum time Grafana's renderer may spend on this render.
	// A zero value leaves the render timeout and HTTP client behavior unchanged.
	Timeout time.Duration

	// Vars holds dashboard template variable overrides (key → value).
	// Each entry is sent as var-{key}={value} on the render URL.
	Vars map[string]string
}

// Client is an HTTP client for Grafana's image renderer API.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new renderer client using auth from the active gcx context.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &Client{
		restConfig: cfg,
		httpClient: httpClient,
	}, nil
}

// Render performs a GET request to the Grafana image renderer and returns the raw PNG bytes.
func (c *Client) Render(ctx context.Context, req RenderRequest) ([]byte, error) {
	renderURL, err := c.buildRenderURL(req)
	if err != nil {
		return nil, fmt.Errorf("failed to build render URL: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, renderURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpClient := c.httpClient
	if req.Timeout > 0 {
		requestClient := *c.httpClient
		requestClient.Timeout = req.Timeout + renderTimeoutOverhead
		httpClient = &requestClient
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute render request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		contentType := resp.Header.Get("Content-Type")
		serverMessage := boundedText(body, renderErrorExcerptBytes)
		// The message also appears in joined errors and in snapshot receipts,
		// where no converter cleans it up, so it never carries raw markup.
		// ServerMessage keeps the body for the converters' HTML detection.
		detail := serverMessage
		if httputils.IsHTML(contentType, body) {
			detail = "Grafana returned an HTML error page"
		}
		return nil, &gcxerrors.HTTPStatusError{
			Status:        resp.StatusCode,
			Message:       fmt.Sprintf("render request failed with status %d: %s", resp.StatusCode, detail),
			ServerMessage: serverMessage,
			ContentType:   contentType,
		}
	}

	if len(body) == 0 {
		return nil, errors.New("render response body is empty")
	}

	return body, nil
}

// RendererAvailable reports whether Grafana says it can render images, read
// from rendererAvailable in /api/frontend/settings. Grafana sets it when a
// renderer plugin is installed or [rendering] server_url points at a remote
// rendering service. The second result is false when the setting could not be
// read, so callers must not draw conclusions from the first in that case.
func (c *Client) RendererAvailable(ctx context.Context) (bool, bool) {
	ctx, cancel := context.WithTimeout(ctx, rendererProbeTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+frontendSettingsPath, nil)
	if err != nil {
		return false, false
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, false
	}

	var settings struct {
		RendererAvailable *bool `json:"rendererAvailable"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, frontendSettingsMaxBytes)).Decode(&settings); err != nil {
		return false, false
	}
	if settings.RendererAvailable == nil {
		return false, false
	}
	return *settings.RendererAvailable, true
}

// boundedText returns body as trimmed, valid UTF-8 of at most limit bytes,
// cut on a rune boundary and marked with an ellipsis when shortened.
func boundedText(body []byte, limit int) string {
	text := strings.ToValidUTF8(strings.TrimSpace(string(body)), "�")
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

func (c *Client) buildRenderURL(req RenderRequest) (string, error) {
	var path string
	if req.PanelID != 0 {
		path = fmt.Sprintf(renderSoloPanelPathFmt, url.PathEscape(req.UID))
	} else {
		path = fmt.Sprintf(renderDashboardPathFmt, url.PathEscape(req.UID))
	}

	u, err := url.Parse(c.restConfig.Host + path)
	if err != nil {
		return "", err
	}

	q := u.Query()

	orgID := req.OrgID
	if orgID == 0 {
		orgID = 1
	}
	q.Set("orgId", strconv.Itoa(orgID))

	if req.PanelID != 0 {
		q.Set("panelId", strconv.Itoa(req.PanelID))
	}

	if req.Width != 0 {
		q.Set("width", strconv.Itoa(req.Width))
	}
	if req.Height != 0 {
		q.Set("height", strconv.Itoa(req.Height))
	}
	from, err := normalizeRenderTime("from", req.From)
	if err != nil {
		return "", err
	}
	if from != "" {
		q.Set("from", from)
	}
	to, err := normalizeRenderTime("to", req.To)
	if err != nil {
		return "", err
	}
	if to != "" {
		q.Set("to", to)
	}
	if req.Tz != "" {
		q.Set("tz", req.Tz)
	}
	if req.Theme != "" {
		q.Set("theme", req.Theme)
	}
	if req.Timeout > 0 {
		q.Set("timeout", strconv.FormatInt(int64(req.Timeout/time.Second), 10))
	}

	// Kiosk mode removes sidebar, nav bar, and other UI chrome so the
	// rendered image contains only dashboard content.
	q.Set("kiosk", "true")
	q.Set("hideNav", "true")
	q.Set("fullPageImage", "true")

	for k, v := range req.Vars {
		q.Set("var-"+k, v)
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

func normalizeRenderTime(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	// Grafana dashboard URLs natively support relative browser time expressions
	// (now, now-1h, now/d, etc.). Keep those as-is.
	if strings.HasPrefix(value, "now") {
		return value, nil
	}

	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return strconv.FormatInt(t.UnixMilli(), 10), nil
	}

	if ts, err := strconv.ParseInt(value, 10, 64); err == nil {
		// Grafana render URLs expect absolute timestamps in epoch milliseconds.
		// Treat values beyond plausible Unix seconds as milliseconds, matching
		// common CLI input and avoiding accidental 1970 ranges.
		if ts >= 10_000_000_000 || ts <= -10_000_000_000 {
			return strconv.FormatInt(ts, 10), nil
		}
		if ts > math.MaxInt64/1000 || ts < math.MinInt64/1000 {
			return "", fmt.Errorf("invalid %s time %q: Unix seconds value overflows milliseconds", name, value)
		}
		return strconv.FormatInt(ts*1000, 10), nil
	}

	if ts, err := strconv.ParseFloat(value, 64); err == nil {
		ms := ts * 1000
		if ms > math.MaxInt64 || ms < math.MinInt64 {
			return "", fmt.Errorf("invalid %s time %q: Unix seconds value overflows milliseconds", name, value)
		}
		return strconv.FormatInt(int64(ms), 10), nil
	}

	return "", fmt.Errorf("invalid %s time %q (use relative time like now-1h, RFC3339, Unix seconds, or Unix milliseconds)", name, value)
}
