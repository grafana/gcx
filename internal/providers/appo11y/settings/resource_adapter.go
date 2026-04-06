package settings

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

const settingsEndpoint = "/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings"

// StaticDescriptor returns the resource descriptor for App Observability settings.
func StaticDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "appo11y.ext.grafana.app",
			Version: "v1alpha1",
		},
		Kind:     Kind,
		Singular: "settings",
		Plural:   "settings",
	}
}

// SettingsSchema returns a JSON Schema for the Settings resource type.
func SettingsSchema() json.RawMessage {
	return adapter.SchemaFromType[PluginSettings](StaticDescriptor())
}

// SettingsExample returns an example Settings manifest as JSON.
func SettingsExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name": "default",
		},
		"spec": map[string]any{
			"jsonData": map[string]any{
				"defaultLogQueryMode": "loki",
				"metricsMode":         "otel",
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("appo11y/settings: failed to marshal example: %v", err))
	}
	return b
}

// settingsAPIClient is a minimal HTTP client for the settings endpoint.
// It avoids a circular import with the parent appo11y package which imports
// settings types. Each subpackage owns its own HTTP wiring.
type settingsAPIClient struct {
	host       string
	httpClient *http.Client
}

func newSettingsAPIClient(cfg internalconfig.NamespacedRESTConfig) (*settingsAPIClient, error) {
	httpClient, err := k8srest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &settingsAPIClient{
		host:       cfg.Host,
		httpClient: httpClient,
	}, nil
}

func (c *settingsAPIClient) getSettings(ctx context.Context) (*PluginSettings, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+settingsEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if err := checkSettingsStatus(resp); err != nil {
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}

	var s PluginSettings
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, fmt.Errorf("failed to decode settings response: %w", err)
	}

	return &s, nil
}

func (c *settingsAPIClient) updateSettings(ctx context.Context, s *PluginSettings) error {
	body, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+settingsEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	return checkSettingsStatus(resp)
}

func checkSettingsStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return errors.New("Grafana App Observability plugin is not installed or not enabled") //nolint:staticcheck // "Grafana" is a proper noun, capitalization is intentional
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

// NewTypedCRUD creates a TypedCRUD for App Observability settings.
// It loads config via ConfigLoader (same pattern as the adapter factory).
func NewTypedCRUD(ctx context.Context) (*adapter.TypedCRUD[PluginSettings], internalconfig.NamespacedRESTConfig, error) {
	var loader providers.ConfigLoader
	loader.SetContextName(internalconfig.ContextNameFromCtx(ctx))

	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to load REST config for App Observability settings: %w", err)
	}

	client, err := newSettingsAPIClient(cfg)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to create settings HTTP client: %w", err)
	}

	crud := &adapter.TypedCRUD[PluginSettings]{
		GetFn: func(ctx context.Context, _ string) (*PluginSettings, error) {
			return client.getSettings(ctx)
		},
		UpdateFn: func(ctx context.Context, _ string, s *PluginSettings) (*PluginSettings, error) {
			if err := client.updateSettings(ctx, s); err != nil {
				return nil, fmt.Errorf("failed to update App Observability settings: %w", err)
			}
			updated, err := client.getSettings(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch updated settings: %w", err)
			}
			return updated, nil
		},
		Namespace:  cfg.Namespace,
		Descriptor: StaticDescriptor(),
	}

	return crud, cfg, nil
}

// NewLazyFactory returns an adapter.Factory that loads its config lazily from the
// default config file when invoked.
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewTypedCRUD(ctx)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// NewFactoryFromConfig returns an adapter.Factory for App Observability settings
// that creates a client using the provided NamespacedRESTConfig.
// The factory is lazy — the client is only created when the factory is invoked.
func NewFactoryFromConfig(cfg internalconfig.NamespacedRESTConfig) adapter.Factory {
	return func(_ context.Context) (adapter.ResourceAdapter, error) {
		client, err := newSettingsAPIClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create settings HTTP client: %w", err)
		}

		crud := &adapter.TypedCRUD[PluginSettings]{
			GetFn: func(ctx context.Context, _ string) (*PluginSettings, error) {
				return client.getSettings(ctx)
			},
			UpdateFn: func(ctx context.Context, _ string, s *PluginSettings) (*PluginSettings, error) {
				if err := client.updateSettings(ctx, s); err != nil {
					return nil, fmt.Errorf("failed to update App Observability settings: %w", err)
				}
				updated, err := client.getSettings(ctx)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch updated settings: %w", err)
				}
				return updated, nil
			},
			Namespace:  cfg.Namespace,
			Descriptor: StaticDescriptor(),
		}

		return crud.AsAdapter(), nil
	}
}
