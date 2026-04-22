package synth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/grafana/gcx/internal/providers/synth/probes"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&SynthProvider{})
}

// checkSchema returns a JSON Schema for the SM Check resource type.
func checkSchema() json.RawMessage {
	return adapter.SchemaFromType[checks.CheckSpec](checks.StaticDescriptor())
}

// checkExample returns an example SM Check manifest as JSON.
func checkExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": checks.APIVersion,
		"kind":       checks.Kind,
		"metadata": map[string]any{
			"name": "web-check",
		},
		"spec": map[string]any{
			"job":              "web-check",
			"target":           "https://grafana.com",
			"frequency":        60000,
			"timeout":          5000,
			"enabled":          true,
			"probes":           []string{"Atlanta", "London", "Tokyo"},
			"settings":         map[string]any{"http": map[string]any{"method": "GET"}},
			"alertSensitivity": "medium",
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("synth/checks: failed to marshal example: %v", err))
	}
	return b
}

// probeSchema returns a JSON Schema for the SM Probe resource type.
func probeSchema() json.RawMessage {
	return adapter.SchemaFromType[probes.Probe](probes.StaticDescriptor())
}

// probeExample returns an example SM Probe manifest as JSON.
func probeExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": probes.APIVersion,
		"kind":       probes.Kind,
		"metadata": map[string]any{
			"name": "my-private-probe",
		},
		"spec": map[string]any{
			"name":      "my-private-probe",
			"latitude":  51.5074,
			"longitude": -0.1278,
			"region":    "Europe",
			"labels":    []map[string]string{{"name": "environment", "value": "production"}},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("synth/probes: failed to marshal example: %v", err))
	}
	return b
}

// SynthProvider manages Grafana Synthetic Monitoring resources.
type SynthProvider struct{}

// Name returns the unique identifier for this provider.
func (p *SynthProvider) Name() string { return "synth" }

// ShortDesc returns a one-line description of the provider.
func (p *SynthProvider) ShortDesc() string {
	return "Manage Grafana Synthetic Monitoring checks and probes"
}

// Commands returns the Cobra commands contributed by this provider.
func (p *SynthProvider) Commands() []*cobra.Command {
	loader := &configLoader{}

	synthCmd := &cobra.Command{
		Use:   "synth",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	// Bind config flags on the parent — all subcommands inherit these.
	loader.BindFlags(synthCmd.PersistentFlags())

	synthCmd.AddCommand(checks.Commands(loader))
	synthCmd.AddCommand(probes.Commands(loader))
	synthCmd.AddCommand(newSetupCommand(p))

	return []*cobra.Command{synthCmd}
}

// ProductName implements framework.StatusDetectable.
func (p *SynthProvider) ProductName() string { return p.Name() }

// Status implements framework.StatusDetectable using a config-key heuristic.
func (p *SynthProvider) Status(ctx context.Context) (*framework.ProductStatus, error) {
	var loader providers.ConfigLoader
	// TODO: add proper error handling once provider setup is implemented
	cfg, _, _ := loader.LoadProviderConfig(ctx, p.Name())
	status := framework.ConfigKeysStatus(p, cfg)
	return &status, nil
}

// InfraCategories implements framework.Setupable.
func (p *SynthProvider) InfraCategories() []framework.InfraCategory { return nil }

// ResolveChoices implements framework.Setupable.
func (p *SynthProvider) ResolveChoices(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// ValidateSetup implements framework.Setupable.
func (p *SynthProvider) ValidateSetup(_ context.Context, _ map[string]string) error { return nil }

// Setup implements framework.Setupable.
func (p *SynthProvider) Setup(_ context.Context, _ map[string]string) error {
	return framework.ErrSetupNotSupported
}

// Validate checks that the given provider configuration is valid.
// Neither sm-url nor sm-token are required here because both can be auto-discovered:
// sm-url from plugin settings, sm-token via the SM register/install API.
func (p *SynthProvider) Validate(_ map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
func (p *SynthProvider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "sm-url", Secret: false},
		{Name: "sm-token", Secret: true},
		{Name: "sm-metrics-datasource-uid", Secret: false},
	}
}

// TypedRegistrations returns adapter registrations for Synth resource types.
func (p *SynthProvider) TypedRegistrations() []adapter.Registration {
	// Register static descriptors for checks and probes so that they appear in
	// the discovery registry and can be used as selectors without initializing
	// the provider config.
	loader := &configLoader{}
	return []adapter.Registration{
		{
			Factory:     checks.NewAdapterFactory(loader),
			Descriptor:  checks.StaticDescriptor(),
			GVK:         checks.StaticGVK(),
			Schema:      checkSchema(),
			Example:     checkExample(),
			URLTemplate: "/a/grafana-synthetic-monitoring-app/checks/{name}",
		},
		{
			Factory:     probes.NewAdapterFactory(loader),
			Descriptor:  probes.StaticDescriptor(),
			GVK:         probes.StaticGVK(),
			Schema:      probeSchema(),
			Example:     probeExample(),
			URLTemplate: "/a/grafana-synthetic-monitoring-app/probes/{name}",
		},
	}
}

// configLoader loads SM credentials from the gcx config + env vars.
// It embeds providers.ConfigLoader for shared config loading infrastructure,
// applying GRAFANA_PROVIDER_SYNTH_* env var overrides via the standard convention.
type configLoader struct {
	providers.ConfigLoader
}

// LoadSMConfig loads the SM base URL, token, and K8s namespace from config.
//
// SM URL resolution priority (highest first):
//  1. GRAFANA_PROVIDER_SYNTH_SM_URL env var / providers.synth.sm-url in config
//  2. Auto-discovery from SM plugin settings (jsonData.apiHost) — requires grafana.server
//  3. Error with actionable guidance
//
// SM token resolution priority (highest first):
//  1. GRAFANA_PROVIDER_SYNTH_SM_TOKEN env var / providers.synth.sm-token in config
//  2. Auto-discovery via SM register/install API — requires cloud.token + stack info from GCOM
//  3. Error with actionable guidance
//
// When auto-discovery succeeds, values are persisted to config so subsequent
// invocations skip the API calls.
func (l *configLoader) LoadSMConfig(ctx context.Context) (string, string, string, error) {
	providerCfg, namespace, err := l.LoadProviderConfig(ctx, "synth")
	if err != nil {
		return "", "", "", err
	}

	smURL := providerCfg["sm-url"]
	smToken := providerCfg["sm-token"]

	// Tier 2: auto-discover SM URL from plugin settings when not explicitly configured.
	if smURL == "" {
		var discoverErr error
		smURL, discoverErr = l.tryDiscoverSMURL(ctx)
		if smURL == "" {
			return "", "", "", fmt.Errorf("SM URL not configured: %w", discoverErr)
		}
	}

	// Tier 2: auto-discover SM token via register/install when not explicitly configured.
	if smToken == "" {
		var discoverErr error
		smToken, discoverErr = l.tryDiscoverSMToken(ctx, smURL)
		if smToken == "" {
			return "", "", "", fmt.Errorf("SM token not configured: %w", discoverErr)
		}
	}

	return smURL, smToken, namespace, nil
}

// tryDiscoverSMURL attempts to auto-discover the SM URL from Grafana plugin settings
// and persists it to config on success. Returns empty string and the reason on failure.
func (l *configLoader) tryDiscoverSMURL(ctx context.Context) (string, error) {
	restCfg, err := l.LoadGrafanaConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("no Grafana server configured: %w", err)
	}

	discovered, err := discoverSMURL(ctx, restCfg)
	if err != nil {
		return "", fmt.Errorf("plugin settings query failed: %w", err)
	}

	// Persist to config so subsequent runs skip the API call.
	if saveErr := l.SaveProviderConfig(ctx, "synth", "sm-url", discovered); saveErr != nil {
		slog.DebugContext(ctx, "failed to cache discovered SM URL to config", "error", saveErr)
	}

	return discovered, nil
}

// tryDiscoverSMToken attempts to auto-discover the SM token via the SM register/install
// API using cloud credentials from GCOM. Persists to config on success. Returns empty string and the reason on failure.
func (l *configLoader) tryDiscoverSMToken(ctx context.Context, smURL string) (string, error) {
	cloudCfg, err := l.LoadCloudConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("no cloud config: %w", err)
	}

	token, err := registerSMInstall(ctx, smURL, cloudCfg.Token, cloudCfg.Stack)
	if err != nil {
		return "", fmt.Errorf("register/install API failed: %w", err)
	}

	if saveErr := l.SaveProviderConfig(ctx, "synth", "sm-token", token); saveErr != nil {
		slog.DebugContext(ctx, "failed to cache discovered SM token to config", "error", saveErr)
	}

	return token, nil
}

// registerSMInstall calls the SM register/install endpoint to obtain an access token.
// This uses a Grafana Cloud access policy token and GCOM stack info to register with
// the SM API, which returns a publisher token for subsequent API calls.
func registerSMInstall(ctx context.Context, smURL, cloudToken string, stack cloud.StackInfo) (string, error) {
	reqBody := struct {
		StackID           int    `json:"stackId"`
		MetricsInstanceID int    `json:"metricsInstanceId"`
		LogsInstanceID    int    `json:"logsInstanceId"`
		PublisherToken    string `json:"publisherToken"`
		RegionSlug        string `json:"regionSlug"`
	}{
		StackID:           stack.ID,
		MetricsInstanceID: stack.HMInstancePromID,
		LogsInstanceID:    stack.HLInstanceID,
		PublisherToken:    cloudToken,
		RegionSlug:        stack.RegionSlug,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal register/install request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(smURL, "/")+"/api/v1/register/install", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cloudToken)

	resp, err := httputils.NewDefaultClient(ctx).Do(req)
	if err != nil {
		return "", fmt.Errorf("SM register/install request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("SM register/install returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode register/install response: %w", err)
	}

	if result.AccessToken == "" {
		return "", errors.New("SM register/install returned empty access token")
	}

	return result.AccessToken, nil
}

// discoverSMURL fetches the SM API URL from the SM plugin settings endpoint.
// This queries /api/plugins/grafana-synthetic-monitoring-app/settings and reads
// jsonData.apiHost, which contains the regional SM API base URL.
//
// In OAuth proxy mode, requests are routed through cfg.Host (the proxy) which
// forwards them to the real Grafana server with proper auth. In direct mode,
// requests go straight to cfg.GrafanaURL with the configured bearer token.
func discoverSMURL(ctx context.Context, cfg config.NamespacedRESTConfig) (string, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP client: %w", err)
	}

	// In OAuth proxy mode, cfg.Host routes through the proxy which handles auth.
	// In direct mode, use GrafanaURL (the real Grafana server).
	baseURL := cfg.GrafanaURL
	if cfg.IsOAuthProxy() {
		baseURL = cfg.Host
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		baseURL+"/api/plugins/grafana-synthetic-monitoring-app/settings", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get SM plugin settings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("SM plugin settings returned HTTP %d", resp.StatusCode)
	}

	var settings struct {
		JSONData struct {
			APIHost string `json:"apiHost"`
		} `json:"jsonData"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&settings); err != nil {
		return "", fmt.Errorf("failed to decode SM plugin settings: %w", err)
	}

	if settings.JSONData.APIHost == "" {
		return "", errors.New("apiHost not found in SM plugin settings")
	}

	return settings.JSONData.APIHost, nil
}

// LoadConfig loads the full config for datasource UID lookup from context settings.
func (l *configLoader) LoadConfig(ctx context.Context) (*config.Config, error) {
	return l.LoadFullConfig(ctx)
}

// SaveMetricsDatasourceUID persists an auto-discovered Prometheus datasource UID to
// providers.synth.sm-metrics-datasource-uid in the config file.
func (l *configLoader) SaveMetricsDatasourceUID(ctx context.Context, uid string) error {
	return l.SaveProviderConfig(ctx, "synth", "sm-metrics-datasource-uid", uid)
}
