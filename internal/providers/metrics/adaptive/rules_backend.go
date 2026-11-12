package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	adaptiveauth "github.com/grafana/gcx/internal/auth/adaptive"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/providers"
)

const (
	rulesProxyRoot  = "/api/plugin-proxy/grafana-adaptive-metrics-app"
	rulesDirectRoot = "/adaptive-metrics/api/v2"

	rulesReadAction   = "grafana-adaptive-metrics-app.rules:read"
	rulesWriteAction  = "grafana-adaptive-metrics-app.rules:write"
	rulesDeleteAction = "grafana-adaptive-metrics-app.rules:delete"
	rulesReadRole     = "Rules Reader"
	rulesEditRole     = "Rules Editor"

	rulesMaxPageSize = 1000
)

type rulesEndpoint struct {
	baseURL  string
	http     *http.Client
	tenantID int
	token    string
}

func (e *rulesEndpoint) do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(e.baseURL, "/")+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("adaptive-metrics: create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if e.token != "" {
		req.SetBasicAuth(strconv.Itoa(e.tenantID), e.token)
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("adaptive-metrics: %w", err)
	}
	defer resp.Body.Close()
	data, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return 0, nil, fmt.Errorf("adaptive-metrics: read response: %w", err)
	}
	return resp.StatusCode, data, nil
}

// rulesBackend talks to the Adaptive Metrics v2 rules API over one of two
// transports that reach the same handlers: Grafana's plugin proxy using the
// context's Grafana credential (preferred), or the Adaptive Metrics API
// directly using the Cloud access-policy token. The direct path is used when
// there is no usable Grafana configuration or when the proxy itself refuses the
// request. It is resolved lazily so proxy-only runs never contact GCOM.
type rulesBackend struct {
	segment string

	proxy    *rulesEndpoint
	proxyErr error

	loadDirect func(context.Context) (*rulesEndpoint, error)
	directOnce sync.Once
	direct     *rulesEndpoint
	directErr  error

	mu        sync.Mutex
	proxySkip map[string]error
}

func newRulesBackend(ctx context.Context, loader *providers.ConfigLoader, segment string) (*rulesBackend, error) {
	if err := validateRuleRef(segment, "", false); err != nil {
		return nil, err
	}
	b := &rulesBackend{
		segment:   segment,
		proxySkip: map[string]error{},
		loadDirect: func(ctx context.Context) (*rulesEndpoint, error) {
			sa, err := adaptiveauth.ResolveSignalAuth(ctx, loader, "metrics")
			if err != nil {
				return nil, err
			}
			return &rulesEndpoint{baseURL: sa.BaseURL, http: sa.HTTPClient, tenantID: sa.TenantID, token: sa.APIToken}, nil
		},
	}
	deps, _, err := providers.LoadGrafanaDepsWithLoader(ctx, loader)
	if err != nil {
		if _, ok := errors.AsType[config.ValidationError](err); !ok {
			return nil, err
		}
		b.proxyErr = err
		return b, nil
	}
	b.proxy = &rulesEndpoint{baseURL: deps.BaseURL, http: deps.HTTP}
	return b, nil
}

type ruleCall struct {
	method     string
	proxyPath  string
	directPath string
	body       []byte
	action     string
	role       string
}

func (b *rulesBackend) segmentPath() string {
	if b.segment == "" {
		return "default"
	}
	return url.PathEscape(b.segment)
}

func (b *rulesBackend) v2Call(method, suffix string, body []byte, action, role string) ruleCall {
	seg := "/segments/" + b.segmentPath()
	return ruleCall{
		method:     method,
		proxyPath:  rulesProxyRoot + "/v2" + seg + suffix,
		directPath: rulesDirectRoot + seg + suffix,
		body:       body,
		action:     action,
		role:       role,
	}
}

func (b *rulesBackend) directEndpoint(ctx context.Context) (*rulesEndpoint, error) {
	b.directOnce.Do(func() { b.direct, b.directErr = b.loadDirect(ctx) })
	return b.direct, b.directErr
}

func (b *rulesBackend) skipReason(method string) (error, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	err, ok := b.proxySkip[method]
	return err, ok
}

func (b *rulesBackend) markSkip(method string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.proxySkip[method] = err
}

func proxyRefusal(c ruleCall, status int, body []byte) error {
	var er providers.ErrorResponse
	_ = json.Unmarshal(body, &er)
	cause := fmt.Errorf("grafana plugin proxy refused the request (status %d): %s", status, er.ErrorMessage())
	if status == http.StatusForbidden && c.action != "" {
		return &providers.PluginRouteDeniedError{Action: c.action, Role: c.role, Cause: cause}
	}
	return cause
}

type proxyOutcome struct {
	status int
	body   []byte
	err    error
	// refused is why the direct path must be used; nil when the proxy answered.
	refused error
	done    bool
}

// sendViaProxy tries the plugin proxy. When done is false the caller falls back
// to the direct path and refused explains why.
func (b *rulesBackend) sendViaProxy(ctx context.Context, c ruleCall) proxyOutcome {
	if b.proxy == nil {
		return proxyOutcome{refused: b.proxyErr}
	}
	if skipErr, skip := b.skipReason(c.method + c.action); skip {
		return proxyOutcome{refused: skipErr}
	}
	status, body, err := b.proxy.do(ctx, c.method, c.proxyPath, c.body)
	if err != nil {
		return proxyOutcome{err: err, done: true}
	}
	if !providers.IsPluginProxyUnavailable(status, body) {
		return proxyOutcome{status: status, body: body, done: true}
	}
	refused := proxyRefusal(c, status, body)
	b.markSkip(c.method+c.action, refused)
	return proxyOutcome{refused: refused}
}

// send issues one call, preferring the plugin proxy. The direct path is taken
// only when the proxy is unconfigured or refused the request before it reached
// the backend, so a request is never applied twice.
func (b *rulesBackend) send(ctx context.Context, c ruleCall) (int, []byte, error) {
	out := b.sendViaProxy(ctx, c)
	if out.done {
		return out.status, out.body, out.err
	}
	proxyErr := out.refused

	ep, err := b.directEndpoint(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("%w; direct access unavailable: %w", proxyErr, err)
	}
	status, body, err := ep.do(ctx, c.method, c.directPath, c.body)
	if err != nil {
		return 0, nil, err
	}
	if (status == http.StatusUnauthorized || status == http.StatusForbidden) && proxyErr != nil {
		return 0, nil, fmt.Errorf("%w; direct access also failed: %w", proxyErr, ruleError("authorize", status, body))
	}
	return status, body, nil
}

// ruleError converts a non-2xx v2 response into an error. The v2 error body
// carries a trace_id on server errors, which the shared formatter reads as
// traceID.
func ruleError(op string, status int, body []byte) error {
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) == nil {
		if id, ok := raw["trace_id"]; ok {
			raw["traceID"] = id
			if b, err := json.Marshal(raw); err == nil {
				body = b
			}
		}
	}
	return fmt.Errorf("adaptive-metrics: %s: %w", op, providers.FormatError(status, body))
}

func isRuleNotFound(status int, body []byte) bool {
	if status != http.StatusNotFound {
		return false
	}
	var er providers.ErrorResponse
	return json.Unmarshal(body, &er) == nil && strings.HasPrefix(er.ErrorMessage(), "rule not found")
}

func (b *rulesBackend) List(ctx context.Context, limit int64) ([]MetricRule, error) {
	rules := []MetricRule{}
	token := ""
	for {
		pageSize := int64(rulesMaxPageSize)
		if limit > 0 && limit-int64(len(rules)) < pageSize {
			pageSize = limit - int64(len(rules))
		}
		q := url.Values{"page_size": {strconv.FormatInt(pageSize, 10)}}
		if token != "" {
			q.Set("page_token", token)
		}
		status, body, err := b.send(ctx, b.v2Call(http.MethodGet, "/rules?"+q.Encode(), nil, rulesReadAction, rulesReadRole))
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, ruleError("list rules", status, body)
		}
		var page struct {
			Rules         []ruleV2 `json:"rules"`
			NextPageToken string   `json:"next_page_token"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("adaptive-metrics: list rules: decode: %w", err)
		}
		for _, r := range page.Rules {
			rules = append(rules, fromRuleV2(r))
		}
		if page.NextPageToken == "" || (limit > 0 && int64(len(rules)) >= limit) {
			return rules, nil
		}
		if page.NextPageToken == token {
			return nil, errors.New("adaptive-metrics: list rules: server repeated a page token")
		}
		token = page.NextPageToken
	}
}

func (b *rulesBackend) Get(ctx context.Context, metric string) (*MetricRule, error) {
	if err := validateRuleRef(b.segment, metric, true); err != nil {
		return nil, err
	}
	status, body, err := b.send(ctx, b.v2Call(http.MethodGet, "/rules/"+url.PathEscape(metric), nil, rulesReadAction, rulesReadRole))
	if err != nil {
		return nil, err
	}
	if isRuleNotFound(status, body) {
		return nil, ErrRuleNotFound
	}
	if status != http.StatusOK {
		return nil, ruleError("get rule", status, body)
	}
	var r ruleV2
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("adaptive-metrics: get rule: decode: %w", err)
	}
	out := fromRuleV2(r)
	return &out, nil
}

func (b *rulesBackend) put(ctx context.Context, op string, rule *MetricRule) (*MetricRule, error) {
	if err := validateRuleRef(b.segment, rule.Metric, true); err != nil {
		return nil, err
	}
	wire, err := toRuleV2(*rule)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: %s: %w", op, err)
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: %s: marshal: %w", op, err)
	}
	status, body, err := b.send(ctx, b.v2Call(http.MethodPut, "/rules/"+url.PathEscape(rule.Metric), data, rulesWriteAction, rulesEditRole))
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, ruleError(op, status, body)
	}
	var echoed ruleV2
	if err := json.Unmarshal(body, &echoed); err != nil {
		return nil, fmt.Errorf("adaptive-metrics: %s: decode: %w", op, err)
	}
	out := fromRuleV2(echoed)
	return &out, nil
}

// Create is an unconditional upsert, as the v2 API defines it.
func (b *rulesBackend) Create(ctx context.Context, rule *MetricRule) (*MetricRule, error) {
	return b.put(ctx, "create rule", rule)
}

func (b *rulesBackend) Update(ctx context.Context, metric string, rule *MetricRule) (*MetricRule, error) {
	if rule.Metric != metric {
		return nil, fmt.Errorf("adaptive-metrics: update rule: metric %q does not match rule metric %q", metric, rule.Metric)
	}
	return b.put(ctx, "update rule", rule)
}

// Delete is idempotent: the v2 API succeeds whether or not the rule existed.
func (b *rulesBackend) Delete(ctx context.Context, metric string) error {
	if err := validateRuleRef(b.segment, metric, true); err != nil {
		return err
	}
	status, body, err := b.send(ctx, b.v2Call(http.MethodDelete, "/rules/"+url.PathEscape(metric), nil, rulesDeleteAction, rulesEditRole))
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return ruleError("delete rule", status, body)
	}
	return nil
}
