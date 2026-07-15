package alert

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources/query"
	"k8s.io/client-go/rest"
)

// ErrRulerNotFound is returned when a requested ruler namespace or group does
// not exist.
var ErrRulerNotFound = errors.New("ruler resource not found")

// RulerClient manages datasource-managed (Mimir/Loki ruler) rule groups via
// Grafana's per-datasource ruler proxy: /api/ruler/{dsUID}/api/v1/rules.
// Responses are YAML per the Lotex ruler contract, but rule-group POST bodies
// must be JSON: Grafana's proxy binds the incoming body as JSON before
// re-serializing it toward the ruler (a YAML body gets 400 "bad request data").
type RulerClient struct {
	httpClient *http.Client
	host       string
	basePath   string
	subtype    string
}

// NewRulerClient creates a ruler client for the given datasource UID.
// subtype selects the backend path flavor on the Grafana side ("mimir" for
// Grafana Cloud Mimir; empty for backends that need no override, e.g. Loki).
func NewRulerClient(cfg config.NamespacedRESTConfig, dsUID, subtype string) (*RulerClient, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &RulerClient{
		httpClient: httpClient,
		host:       cfg.Host,
		basePath:   "/api/ruler/" + url.PathEscape(dsUID) + "/api/v1/rules",
		subtype:    subtype,
	}, nil
}

// rulerSubtypeForDatasourceType maps a datasource plugin type to the ruler
// proxy subtype. Returns an error for datasource types without a ruler API.
// The plugin type is normalized first, so Prometheus forks (Mimir, Amazon
// Managed Prometheus, ...) map to "mimir" like plain Prometheus.
func rulerSubtypeForDatasourceType(dsType string) (string, error) {
	switch query.NormalizeKind(dsType) {
	case "prometheus":
		return "mimir", nil
	case "loki":
		return "", nil
	default:
		return "", fmt.Errorf("datasource type %q has no ruler API (supported: prometheus-flavored, loki)", dsType)
	}
}

func (c *RulerClient) path(segments ...string) string {
	var p strings.Builder
	p.WriteString(c.basePath)
	for _, s := range segments {
		p.WriteString("/")
		p.WriteString(url.PathEscape(s))
	}
	if c.subtype != "" {
		p.WriteString("?subtype=")
		p.WriteString(url.QueryEscape(c.subtype))
	}
	return p.String()
}

// doYAML performs a bodyless HTTP request and optionally decodes a YAML
// response. 2xx with an empty body is success. 404 maps to ErrRulerNotFound.
func (c *RulerClient) doYAML(ctx context.Context, method, path string, out any) error {
	return doBody(ctx, c.httpClient, method, c.host+path, path, yamlBodyCodec(), ErrRulerNotFound, nil, out)
}

// ListNamespaces returns all rule groups keyed by namespace. An empty ruler
// tenant is returned as an empty map (the Loki ruler answers 404 "no rule
// groups found" when the tenant has no rules at all).
func (c *RulerClient) ListNamespaces(ctx context.Context) (map[string][]RulerRuleGroup, error) {
	out := map[string][]RulerRuleGroup{}
	err := c.doYAML(ctx, http.MethodGet, c.path(), &out)
	if errors.Is(err, ErrRulerNotFound) {
		return map[string][]RulerRuleGroup{}, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListGroups returns the rule groups in a single namespace, keyed by
// namespace (the ruler returns the same map shape as the full listing).
func (c *RulerClient) ListGroups(ctx context.Context, namespace string) (map[string][]RulerRuleGroup, error) {
	out := map[string][]RulerRuleGroup{}
	if err := c.doYAML(ctx, http.MethodGet, c.path(namespace), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetGroup returns a single rule group.
func (c *RulerClient) GetGroup(ctx context.Context, namespace, group string) (*RulerRuleGroup, error) {
	var out RulerRuleGroup
	if err := c.doYAML(ctx, http.MethodGet, c.path(namespace, group), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ApplyGroup creates or updates a rule group in the given namespace. The body
// is JSON: Grafana's ruler proxy rejects YAML bodies (see RulerClient docs).
func (c *RulerClient) ApplyGroup(ctx context.Context, namespace string, group RulerRuleGroup) error {
	path := c.path(namespace)
	return doBody(ctx, c.httpClient, http.MethodPost, c.host+path, path, jsonBodyCodec(), ErrRulerNotFound, group, nil)
}

// DeleteGroup deletes a single rule group.
func (c *RulerClient) DeleteGroup(ctx context.Context, namespace, group string) error {
	return c.doYAML(ctx, http.MethodDelete, c.path(namespace, group), nil)
}

// DeleteNamespace deletes a namespace and all rule groups in it.
func (c *RulerClient) DeleteNamespace(ctx context.Context, namespace string) error {
	return c.doYAML(ctx, http.MethodDelete, c.path(namespace), nil)
}
