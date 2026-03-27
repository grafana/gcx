# Provider Implementation Guide

> How to add a new provider to gcx — from interface to registry registration.

## Overview

Providers are compile-time extension points that contribute Cobra commands and
typed configuration to gcx. A provider encapsulates everything needed
to manage a specific Grafana product (e.g., SLO, OnCall, Synthetic Monitoring):
its CLI commands, its config schema, and its validation logic.

When to create a provider:
- You want to add top-level commands for a Grafana Cloud product
- The product requires product-specific authentication or configuration keys
- You want those config keys to integrate with `gcx config set` and
  `GRAFANA_PROVIDER_*` environment variables automatically

Architecture reference: [patterns.md – Provider Plugin System](../architecture/patterns.md#11-provider-plugin-system-high-confidence-93) (Pattern 11),
[config-system.md](../architecture/config-system.md) (Provider config section).

---

## Step 1: Implement the Provider Interface

Create a new package under `internal/providers/` for your provider, or add it
to an existing package. The interface is defined in `internal/providers/provider.go`:

```go
type Provider interface {
    Name()       string
    ShortDesc()  string
    Commands()   []*cobra.Command
    Validate(cfg map[string]string) error
    ConfigKeys() []ConfigKey
}
```

A minimal skeleton:

```go
package slo

import (
    "github.com/spf13/cobra"
    "github.com/grafana/gcx/internal/providers"
)

// SLOProvider manages Grafana SLO resources.
type SLOProvider struct{}

var _ providers.Provider = &SLOProvider{}

func (p *SLOProvider) Name() string      { return "slo" }
func (p *SLOProvider) ShortDesc() string { return "Manage Grafana SLO resources." }
```

**Naming rules:**
- `Name()` is the map key used in config and env vars — use lowercase, no spaces
- `Name()` must be unique across all registered providers
- `ShortDesc()` should be one sentence ending with a period

---

## Step 2: Declare Config Keys

`ConfigKeys()` tells gcx which config keys your provider uses and which
are secrets. This drives the secure-by-default redaction in `gcx config view`.

```go
func (p *SLOProvider) ConfigKeys() []providers.ConfigKey {
    return []providers.ConfigKey{
        {Name: "token",   Secret: true},   // redacted in config view
        {Name: "url",     Secret: false},  // shown in plain text
        {Name: "org-id",  Secret: false},
    }
}
```

**Redaction model (secure by default):**

| Situation | Behaviour |
|-----------|-----------|
| Known provider, `Secret: true` key | Redacted |
| Known provider, `Secret: false` key | Shown as-is |
| Known provider, **undeclared** key | Redacted |
| Unknown provider (not in registry) | **All** values redacted |
| Empty value | Never redacted |

Declare every key your provider reads, otherwise it will be silently redacted
when users run `gcx config view`.

---

## Step 3: Implement Validate

`Validate` receives the full provider config as a `map[string]string` and
returns an error if required keys are missing or malformed. It is called by
your commands before making API calls.

```go
import "fmt"

func (p *SLOProvider) Validate(cfg map[string]string) error {
    if cfg["token"] == "" {
        return fmt.Errorf("slo provider: token is required; "+
            "set it with: gcx config set contexts.<ctx>.providers.slo.token <value>")
    }
    return nil
}
```

Guidelines:
- Return actionable error messages that tell the user what to do
- Only validate what is strictly required — optional keys should not fail here
- Do not perform network calls inside `Validate`

---

## Step 4: Implement Commands

`Commands()` returns the Cobra commands to add under the gcx root. Each
command receives provider config by reading the active context at call time.

Follow the Options pattern used by all other commands — accept `*cmdconfig.Options`
as a constructor argument and call `configOpts.LoadConfig(cmd.Context())` inside `RunE`:

```go
import cmdconfig "github.com/grafana/gcx/cmd/gcx/config"

// Commands returns a "slo" command group with subcommands underneath it.
// Config flags are bound once on the parent's PersistentFlags so every
// subcommand inherits them automatically.
func (p *SLOProvider) Commands() []*cobra.Command {
    configOpts := &cmdconfig.Options{}

    sloCmd := &cobra.Command{
        Use:   "slo",
        Short: p.ShortDesc(),
    }

    // Bind once on the parent — all subcommands inherit these flags.
    configOpts.BindFlags(sloCmd.PersistentFlags())

    sloCmd.AddCommand(newListCommand(configOpts))
    // sloCmd.AddCommand(newGetCommand(configOpts))  // add more subcommands here

    return []*cobra.Command{sloCmd}
}

func newListCommand(configOpts *cmdconfig.Options) *cobra.Command {
    return &cobra.Command{
        Use:   "list",
        Short: "List SLO definitions.",
        RunE: func(cmd *cobra.Command, _ []string) error {
            cfg, err := configOpts.LoadConfig(cmd.Context())
            if err != nil {
                return err
            }
            curCtx := cfg.GetCurrentContext()

            providerCfg := curCtx.Providers["slo"]  // map[string]string

            // Validate before use
            p := &SLOProvider{}
            if err := p.Validate(providerCfg); err != nil {
                return err
            }

            token := providerCfg["token"]
            url   := providerCfg["url"]
            // ... make API calls ...
            _ = token
            _ = url
            return nil
        },
    }
}
```

**Wiring note:** The root command automatically adds every provider's commands
via `p.Commands()...` — you do not need to touch `cmd/gcx/root/command.go`.

---

## Step 4a: Building PromQL Queries

If your provider queries Prometheus datasources, use `github.com/grafana/promql-builder/go/promql`
to construct PromQL expressions instead of `fmt.Sprintf`. This eliminates string
injection risks and makes complex queries composable and readable.

### Simple metric query

```go
import "github.com/grafana/promql-builder/go/promql"

func buildMetricQuery(metricName, uuidRegex string) (string, error) {
    expr, err := promql.Vector(metricName).
        LabelMatchRegexp("grafana_slo_uuid", uuidRegex).
        Build()
    if err != nil {
        return "", err
    }
    return expr.String(), nil
}
// Output: grafana_slo_sli_window{grafana_slo_uuid=~"uuid1|uuid2"}
```

### Complex computed query (burn rate example)

```go
func buildBurnRateQuery(uuidRegex string) (string, error) {
    label := "grafana_slo_uuid"

    successRate := promql.Sum(
        promql.AvgOverTime(
            promql.Vector("grafana_slo_success_rate_5m").
                LabelMatchRegexp(label, uuidRegex).Range("1h"),
        ),
    ).By([]string{label})

    totalRate := promql.Sum(
        promql.AvgOverTime(
            promql.Vector("grafana_slo_total_rate_5m").
                LabelMatchRegexp(label, uuidRegex).Range("1h"),
        ),
    ).By([]string{label})

    // burn_rate = (1 - clamp_max(success/total, 1)) / (1 - objective)
    errorRate := promql.Sub(promql.N(1),
        promql.ClampMax(promql.Div(successRate, totalRate), 1))
    allowedError := promql.Sub(promql.N(1),
        promql.Vector("grafana_slo_objective").
            LabelMatchRegexp(label, uuidRegex))

    burnRate := promql.Div(errorRate, allowedError).On([]string{label})

    expr, err := burnRate.Build()
    if err != nil {
        return "", err
    }
    return expr.String(), nil
}
```

### Batch-querying pattern

Join multiple resource UUIDs with `|` and pass as a regex matcher via
`.LabelMatchRegexp()`. Group results back to individual resources using
`sum by (uuid_label)(...)`. This minimizes the number of Prometheus queries
while returning per-resource values.

```go
uuids := []string{"abc123", "def456", "ghi789"}
uuidRegex := strings.Join(uuids, "|")

query, _ := buildMetricQuery("grafana_slo_sli_window", uuidRegex)
// Result: grafana_slo_sli_window{grafana_slo_uuid=~"abc123|def456|ghi789"}
```

### Data fetching rule

Always fetch all available metrics regardless of the `--output` format. The
output format controls **display**, not **data acquisition**. Table codecs
choose which columns to show; JSON/YAML codecs serialize the full struct. See
Pattern 13 in `patterns.md`.

Reference: `internal/providers/slo/definitions/status.go`, `internal/query/prometheus/client.go`

---

## Step 5: Register the Provider

Providers self-register using the `Register()` function in your provider's `init()` function.
Add this to your provider package (typically in `internal/providers/{provider}/provider.go`):

```go
func init() {
    providers.Register(&SLOProvider{})
}
```

The `Register()` function appends your provider to the global registry automatically. Once registered via `init()`:
- Its commands appear under `gcx`
- Its name and description appear in `gcx providers`
- Its secrets are correctly redacted by `gcx config view`
- Its config is loaded from YAML and env vars automatically

This self-registration pattern (via `init()`) is handled by Go's import system — just ensure your provider package is imported somewhere in the application startup (e.g., in `cmd/gcx/root/command.go`). Reference: `internal/providers/slo/provider.go` for the full implementation.

---

## Step 6: Configuration Patterns

### YAML Config

Provider config lives in the `providers` map within a context:

```yaml
# ~/.config/gcx/config.yaml
current-context: prod
contexts:
  prod:
    grafana:
      server: https://grafana.example.com
      token: gf_...
    providers:
      slo:
        token: glsa_...
        url: https://slo.example.com
      oncall:
        token: glsa_...
```

Set individual keys with the config command (dotted-path notation):

```bash
gcx config set contexts.prod.providers.slo.token glsa_abc123
gcx config set contexts.prod.providers.slo.url https://slo.example.com
```

### Environment Variables

Any config key can be set via environment variable using the pattern:

```
GRAFANA_PROVIDER_{PROVIDER_NAME}_{CONFIG_KEY}=value
```

Provider names and keys are lowercased automatically, and underscores in the
config key portion are converted to dashes (matching the kebab-case YAML
convention). The suffix after `GRAFANA_PROVIDER_` is split on the **first
underscore only** — everything before it becomes the provider name, everything
after becomes the config key (with `_` → `-` normalization):

```bash
# GRAFANA_PROVIDER_SLO_TOKEN    → provider=slo, key=token
# GRAFANA_PROVIDER_SLO_ORG_ID   → provider=slo, key=org-id
export GRAFANA_PROVIDER_SLO_TOKEN=glsa_abc123
export GRAFANA_PROVIDER_SLO_ORG_ID=42
```

Env vars take precedence over YAML config values.

---

## Step 7: Testing

Use the `mockProvider` helper pattern from `internal/providers/provider_test.go`
when writing tests that need a fake provider:

```go
type mockProvider struct {
    name       string
    shortDesc  string
    commands   []*cobra.Command
    validateFn func(cfg map[string]string) error
    configKeys []providers.ConfigKey
}

var _ providers.Provider = &mockProvider{}

func (m *mockProvider) Name() string                         { return m.name }
func (m *mockProvider) ShortDesc() string                    { return m.shortDesc }
func (m *mockProvider) Commands() []*cobra.Command           { return m.commands }
func (m *mockProvider) Validate(cfg map[string]string) error { return m.validateFn(cfg) }
func (m *mockProvider) ConfigKeys() []providers.ConfigKey    { return m.configKeys }
```

Test the interface contract directly:

```go
func TestSLOProvider(t *testing.T) {
    p := &SLOProvider{}

    t.Run("name is stable", func(t *testing.T) {
        assert.Equal(t, "slo", p.Name())
    })

    t.Run("token is required", func(t *testing.T) {
        err := p.Validate(map[string]string{})
        assert.ErrorContains(t, err, "token is required")
    })

    t.Run("valid config passes", func(t *testing.T) {
        err := p.Validate(map[string]string{"token": "glsa_xxx"})
        assert.NoError(t, err)
    })

    t.Run("token declared as secret", func(t *testing.T) {
        keys := p.ConfigKeys()
        for _, k := range keys {
            if k.Name == "token" {
                assert.True(t, k.Secret, "token must be declared as secret")
                return
            }
        }
        t.Fatal("token key not declared in ConfigKeys")
    })
}
```

Test redaction behaviour separately using `providers.RedactSecrets` directly —
see `internal/providers/redact_test.go` for table-driven examples.

---

## Checklist

When implementing a new provider (see also [design-guide.md Section 7](design-guide.md#7-provider-command-checklist) for UX compliance requirements):

- [ ] Struct implements all five `Provider` interface methods
- [ ] `Name()` is lowercase, unique, and stable (it is the map key in config files)
- [ ] All config keys read by commands are declared in `ConfigKeys()`
- [ ] Secret keys (`passwords`, `tokens`, `api_keys`) have `Secret: true`
- [ ] `Validate` returns a helpful error message pointing to the `config set` command
- [ ] Provider is added to `internal/providers/registry.go:All()`
- [ ] `make build` succeeds
- [ ] `make tests` passes
- [ ] `gcx providers` lists the new provider
- [ ] `gcx config view` redacts secrets correctly
