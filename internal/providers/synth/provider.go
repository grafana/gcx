package synth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/grafana/gcx/internal/providers/synth/probes"
	"github.com/grafana/gcx/internal/resources/adapter"
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

	return []*cobra.Command{synthCmd}
}

// Validate checks that the given provider configuration is valid.
// sm-url is not required here because it can be auto-discovered from plugin settings.
func (p *SynthProvider) Validate(cfg map[string]string) error {
	if cfg["sm-token"] == "" {
		return errors.New("sm-token is required for the synth provider")
	}
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
// SM URL resolution priority (highest first):
//  1. GRAFANA_PROVIDER_SYNTH_SM_URL env var / providers.synth.sm-url in config
//  2. Auto-discovery from SM plugin settings (jsonData.apiHost) — requires grafana.server
//  3. Error with actionable guidance
//
// When auto-discovery succeeds the URL is persisted to config so subsequent
// invocations skip the API call.
func (l *configLoader) LoadSMConfig(ctx context.Context) (string, string, string, error) {
	providerCfg, namespace, err := l.LoadProviderConfig(ctx, "synth")
	if err != nil {
		return "", "", "", err
	}

	smURL := providerCfg["sm-url"]
	smToken := providerCfg["sm-token"]

	// Tier 2: auto-discover SM URL from plugin settings when not explicitly configured.
	if smURL == "" {
		smURL = l.tryDiscoverSMURL(ctx)
	}

	if smURL == "" {
		return "", "", "", errors.New(
			"SM URL not configured: auto-discovery from Grafana plugin settings failed or no Grafana server configured")
	}
	if smToken == "" {
		return "", "", "", errors.New(
			"SM token not configured: set providers.synth.sm-token in config or GRAFANA_PROVIDER_SYNTH_SM_TOKEN env var")
	}

	return smURL, smToken, namespace, nil
}

// tryDiscoverSMURL attempts to auto-discover the SM URL from Grafana plugin settings
// and persists it to config on success. Returns empty string on failure.
func (l *configLoader) tryDiscoverSMURL(ctx context.Context) string {
	restCfg, err := l.LoadGrafanaConfig(ctx)
	if err != nil {
		return ""
	}

	discovered, err := DiscoverSMURL(ctx, restCfg)
	if err != nil {
		slog.DebugContext(ctx, "SM URL auto-discovery failed", "error", err)
		return ""
	}

	// Persist to config so subsequent runs skip the API call.
	if saveErr := l.SaveProviderConfig(ctx, "synth", "sm-url", discovered); saveErr != nil {
		slog.DebugContext(ctx, "failed to cache discovered SM URL to config", "error", saveErr)
	}

	return discovered
}

// DiscoverSMURL fetches the SM API URL from the SM plugin settings endpoint.
// This queries /api/plugins/grafana-synthetic-monitoring-app/settings and reads
// jsonData.apiHost, which contains the regional SM API base URL.
func DiscoverSMURL(ctx context.Context, cfg config.NamespacedRESTConfig) (string, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP client: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		cfg.Host+"/api/plugins/grafana-synthetic-monitoring-app/settings", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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
