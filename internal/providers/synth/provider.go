package synth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/grafana/gcx/internal/providers/synth/probes"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
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
func (p *SynthProvider) Validate(cfg map[string]string) error {
	if cfg["sm-url"] == "" {
		return errors.New("sm-url is required for the synth provider")
	}
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
			Factory:    checks.NewAdapterFactory(loader),
			Descriptor: checks.StaticDescriptor(),
			GVK:        checks.StaticGVK(),
			Schema:     checkSchema(),
			Example:    checkExample(),
		},
		{
			Factory:    probes.NewAdapterFactory(loader),
			Descriptor: probes.StaticDescriptor(),
			GVK:        probes.StaticGVK(),
			Schema:     probeSchema(),
			Example:    probeExample(),
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
// Priority (highest first):
//  1. GRAFANA_PROVIDER_SYNTH_SM_URL / GRAFANA_PROVIDER_SYNTH_SM_TOKEN env vars
//  2. Config file: providers.synth.sm-url / sm-token
func (l *configLoader) LoadSMConfig(ctx context.Context) (string, string, string, error) {
	providerCfg, namespace, err := l.LoadProviderConfig(ctx, "synth")
	if err != nil {
		return "", "", "", err
	}

	smURL := providerCfg["sm-url"]
	smToken := providerCfg["sm-token"]

	if smURL == "" {
		return "", "", "", errors.New(
			"SM URL not configured: set providers.synth.sm-url in config or GRAFANA_PROVIDER_SYNTH_SM_URL env var")
	}
	if smToken == "" {
		return "", "", "", errors.New(
			"SM token not configured: set providers.synth.sm-token in config or GRAFANA_PROVIDER_SYNTH_SM_TOKEN env var")
	}

	return smURL, smToken, namespace, nil
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
