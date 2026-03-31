package overrides

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8srest "k8s.io/client-go/rest"
)

const (
	overridesPath = "/api/plugin-proxy/grafana-app-observability-app/overrides"
)

// StaticDescriptor returns the resource descriptor for App O11y Overrides.
func StaticDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "appo11y.ext.grafana.app",
			Version: "v1alpha1",
		},
		Kind:     "Overrides",
		Singular: "overrides",
		Plural:   "overrides",
	}
}

// OverridesSchema returns a JSON Schema for the Overrides resource type.
func OverridesSchema() json.RawMessage {
	return adapter.SchemaFromType[MetricsGeneratorConfig](StaticDescriptor())
}

// OverridesExample returns an example Overrides manifest as JSON.
func OverridesExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name": "default",
		},
		"spec": map[string]any{
			"metrics_generator": map[string]any{
				"disable_collection":  false,
				"collection_interval": "60s",
				"processor": map[string]any{
					"service_graphs": map[string]any{
						"dimensions": []string{"http.method", "http.status_code"},
					},
					"span_metrics": map[string]any{
						"dimensions": []string{"http.method"},
					},
				},
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("overrides: failed to marshal example: %v", err))
	}
	return b
}

// NewTypedCRUD creates a TypedCRUD for App O11y Overrides.
// GetFn populates the ETag annotation via MetadataFn.
// UpdateFn extracts the ETag from the spec (set by the command layer via SetETag) and
// passes it to the API as an If-Match header.
// ListFn, CreateFn, and DeleteFn are nil (singleton, no collection endpoint).
func NewTypedCRUD(ctx context.Context) (*adapter.TypedCRUD[MetricsGeneratorConfig], internalconfig.NamespacedRESTConfig, error) {
	var loader providers.ConfigLoader
	loader.SetContextName(internalconfig.ContextNameFromCtx(ctx))

	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to load REST config for overrides: %w", err)
	}

	c, err := newClient(cfg)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to create overrides client: %w", err)
	}

	crud := &adapter.TypedCRUD[MetricsGeneratorConfig]{
		GetFn: func(ctx context.Context, _ string) (*MetricsGeneratorConfig, error) {
			return c.getOverrides(ctx)
		},
		UpdateFn: func(ctx context.Context, _ string, cfg *MetricsGeneratorConfig) (*MetricsGeneratorConfig, error) {
			etag := cfg.ETag()
			if err := c.updateOverrides(ctx, cfg, etag); err != nil {
				return nil, fmt.Errorf("failed to update overrides: %w", err)
			}
			updated, err := c.getOverrides(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to re-fetch overrides after update: %w", err)
			}
			return updated, nil
		},
		MetadataFn: func(cfg MetricsGeneratorConfig) map[string]any {
			etag := cfg.ETag()
			if etag == "" {
				return nil
			}
			return map[string]any{
				"annotations": map[string]any{
					ETagAnnotation: etag,
				},
			}
		},
		Namespace:  cfg.Namespace,
		Descriptor: StaticDescriptor(),
	}

	return crud, cfg, nil
}

// NewLazyFactory returns an adapter.Factory that loads its config lazily from the
// default config file when invoked. Used for global adapter registration in init()
// and by AppO11yProvider.TypedRegistrations().
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewTypedCRUD(ctx)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// ---------------------------------------------------------------------------
// local HTTP client (avoids circular import with parent appo11y package)
// ---------------------------------------------------------------------------

type client struct {
	host       string
	httpClient *http.Client
}

func newClient(cfg internalconfig.NamespacedRESTConfig) (*client, error) {
	hc, err := k8srest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &client{host: cfg.Host, httpClient: hc}, nil
}

func (c *client) getOverrides(ctx context.Context) (*MetricsGeneratorConfig, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, overridesPath, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get overrides: %w", err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp); err != nil {
		return nil, err
	}

	var cfg MetricsGeneratorConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode overrides response: %w", err)
	}

	cfg.SetETag(resp.Header.Get("ETag"))
	return &cfg, nil
}

func (c *client) updateOverrides(ctx context.Context, cfg *MetricsGeneratorConfig, etag string) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal overrides: %w", err)
	}

	var extraHeaders map[string]string
	if etag != "" {
		extraHeaders = map[string]string{"If-Match": etag}
	}

	resp, err := c.doRequest(ctx, http.MethodPost, overridesPath, bytes.NewReader(body), extraHeaders)
	if err != nil {
		return fmt.Errorf("failed to update overrides: %w", err)
	}
	defer resp.Body.Close()

	return checkStatus(resp)
}

func (c *client) doRequest(ctx context.Context, method, path string, body io.Reader, extraHeaders map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.host+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	return resp, nil
}

func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return errors.New("Grafana App Observability plugin is not installed or not enabled") //nolint:staticcheck // "Grafana" is a proper noun, capitalization is intentional
	}

	if resp.StatusCode == http.StatusPreconditionFailed {
		return errors.New("concurrent modification conflict: overrides were modified since last read — re-fetch and retry")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("request failed with status %d (could not read body: %w)", resp.StatusCode, err)
	}

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("request failed with status %d", resp.StatusCode)
}
