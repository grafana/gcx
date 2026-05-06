package synth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
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
		Use:     "synthetic-monitoring",
		Aliases: []string{"sm", "synth"},
		Short:   p.ShortDesc(),
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

// Validate checks that the given provider configuration is valid. The synth
// provider has no required keys — the SM datasource UID is resolved from the
// context's datasources.synth field (or auto-discovered via the Grafana API).
func (p *SynthProvider) Validate(_ map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
// sm-metrics-datasource-uid is the Prometheus datasource used by status/timeline
// queries; the SM datasource UID itself lives under datasources.synth, not here.
func (p *SynthProvider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
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

// LoadSMConfig resolves the Grafana REST config and the SM datasource UID
// needed to route SM API calls through the datasource proxy.
//
// SM datasource UID resolution priority (highest first):
//  1. datasources.synth in the active context's config
//  2. Auto-discovery via /api/datasources when Grafana exposes a single SM datasource
//
// When auto-discovery succeeds, the UID is persisted to config so subsequent
// invocations skip the API call.
func (l *configLoader) LoadSMConfig(ctx context.Context) (config.NamespacedRESTConfig, string, string, error) {
	_, namespace, err := l.LoadProviderConfig(ctx, "synth")
	if err != nil {
		return config.NamespacedRESTConfig{}, "", "", err
	}

	restCfg, err := l.LoadGrafanaConfig(ctx)
	if err != nil {
		return config.NamespacedRESTConfig{}, "", "", fmt.Errorf("loading Grafana REST config: %w", err)
	}

	var cfgCtx *config.Context
	if fullCfg, cfgErr := l.LoadFullConfig(ctx); cfgErr == nil {
		cfgCtx = fullCfg.GetCurrentContext()
	} else {
		slog.DebugContext(ctx, "could not load full config; falling back to auto-discovery", "error", cfgErr)
	}

	uid, err := dsquery.ResolveAndSaveDatasource(ctx, l, "", cfgCtx, restCfg, "synth")
	if err != nil {
		return config.NamespacedRESTConfig{}, "", "", fmt.Errorf("resolving SM datasource: %w", err)
	}

	return restCfg, uid, namespace, nil
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
