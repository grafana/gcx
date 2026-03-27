---
type: feature-spec
title: "ConfigLoader Compliance: Unified Config Loading Across All Providers"
status: done
beads_id: gcx-experiments-3ip.11
created: 2026-03-26
---

# ConfigLoader Compliance: Unified Config Loading Across All Providers

## Problem Statement

The gcx provider ecosystem has inconsistent config loading patterns. The `synth` provider duplicates ~100 lines of `providers.ConfigLoader` logic (env var parsing, context resolution, config file loading) in a local `configLoader` struct. This duplication creates maintenance risk: any change to env var precedence, context resolution, or config layering must be replicated in two places. Additionally, `ConfigLoader` lacks methods for provider-specific config extraction, forcing providers like `synth`, `oncall`, and `k6` to implement custom workarounds.

**Who is affected:** Maintainers adding or modifying providers, and users who expect consistent env var and config behavior across all providers.

**Current workarounds:**
- `synth` reimplements `ConfigLoader` entirely with its own `loadConfig()`, `envOverride()`, and `configSource()` functions, plus legacy `GRAFANA_SM_*` env vars.
- `oncall` embeds `ConfigLoader` but handles URL discovery (`GRAFANA_ONCALL_URL`) via ad-hoc `os.Getenv` calls.
- `k6` extracts `api-domain` from `ProviderConfig` map ad-hoc in its resource adapter factory.

## Scope

### In Scope

- Add `LoadProviderConfig`, `SaveProviderConfig`, and `LoadFullConfig` methods to `providers.ConfigLoader`
- Migrate `synth` provider from its custom `configLoader` to embedded `providers.ConfigLoader`
- Remove legacy `GRAFANA_SM_URL`, `GRAFANA_SM_TOKEN` env var support from synth (use standard `GRAFANA_PROVIDER_SYNTH_*` convention)
- Remove legacy `GRAFANA_ONCALL_URL` env var support from oncall (use standard `GRAFANA_PROVIDER_ONCALL_*` convention)
- Standardize `k6` provider's `api-domain` extraction to use `LoadProviderConfig`
- Verify that `alert`, `fleet`, `incidents`, `kg`, and `slo` providers remain unaffected
- Support `synth` provider's `SaveMetricsDatasourceUID` (config write-back) via `SaveProviderConfig`
- Migrate `synth` provider from `config.Load` to `config.LoadLayered`

### Out of Scope

- **Changing the Provider interface** (`Name`, `ShortDesc`, `Commands`, `Validate`, `ConfigKeys`, `TypedRegistrations`) -- unchanged
- **Changing config file format** -- `contexts.[name].providers.[provider].[key]` unchanged
- **Modifying non-provider config loading** (e.g., `cmd/gcx/config` Options) -- only `internal/providers/` is in scope
- **Changing provider business logic** -- only config loading/client construction code is modified
- **Plugin-based URL discovery** -- `oncall`'s `DiscoverOnCallURL` Grafana plugin API call remains as-is
- **Hook/extension interfaces** -- not needed; three new methods on ConfigLoader are sufficient

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Drop legacy env vars | Remove GRAFANA_SM_URL, GRAFANA_SM_TOKEN, GRAFANA_ONCALL_URL | Simplifies implementation; all providers use uniform GRAFANA_PROVIDER_<NAME>_<KEY> convention | User decision |
| No ProviderHooks interface | Three new methods on ConfigLoader only; no hooks/extension pattern | No current provider needs post-load hooks; YAGNI — add hooks when a concrete use case arises | User decision |
| Config write-back | Add SaveProviderConfig method to ConfigLoader | Generalized from synth's SaveMetricsDatasourceUID; available to all providers | Bead .2 |
| synth LoadConfig access | Add LoadFullConfig method returning *config.Config | Synth needs full config for datasource UID lookup | Bead .2 |
| config.Load vs config.LoadLayered | Synth MUST switch to config.LoadLayered | ConfigLoader uses LoadLayered; synth's config.Load is non-standard | Code analysis |

## Functional Requirements

**FR-001:** The `providers.ConfigLoader` MUST provide a `LoadProviderConfig(ctx, providerName) (map[string]string, string, error)` method that returns the resolved provider config map and namespace, applying `GRAFANA_PROVIDER_<NAME>_<KEY>` env var overrides and config file values.

**FR-002:** The `providers.ConfigLoader` MUST provide a `SaveProviderConfig(ctx, providerName, key, value) error` method that persists a single key-value pair to `contexts.[current].providers.[providerName].[key]` in the config file.

**FR-003:** The `providers.ConfigLoader` MUST provide a `LoadFullConfig(ctx) (*config.Config, error)` method that returns the fully resolved config object.

**FR-004:** The `synth` provider MUST replace its local `configLoader` struct with an embedded `providers.ConfigLoader`.

**FR-005:** The `synth` provider MUST use `GRAFANA_PROVIDER_SYNTH_SM_URL` and `GRAFANA_PROVIDER_SYNTH_SM_TOKEN` env vars (standard convention) instead of legacy `GRAFANA_SM_URL` and `GRAFANA_SM_TOKEN`.

**FR-006:** The `synth` provider MUST continue to support `SaveMetricsDatasourceUID` via `ConfigLoader.SaveProviderConfig`.

**FR-007:** The `synth` provider MUST use `config.LoadLayered` (via the embedded ConfigLoader) instead of `config.Load` with explicit `Source`.

**FR-008:** The `synth` provider's local `envOverride` function, `configSource` function, and `loadConfig` method MUST be deleted after migration.

**FR-009:** The `oncall` provider MUST remove ad-hoc `os.Getenv("GRAFANA_ONCALL_URL")` calls and use `GRAFANA_PROVIDER_ONCALL_ONCALL_URL` via `LoadProviderConfig` instead.

**FR-010:** The `oncall` provider's `discoverOnCallURL` function MUST check `LoadProviderConfig("oncall")` for the `oncall-url` key before falling back to plugin discovery.

**FR-011:** The `k6` provider MUST extract `api-domain` via `LoadProviderConfig("k6")` instead of ad-hoc `ProviderConfig` map extraction.

**FR-012:** The `alert`, `fleet`, `incidents`, `kg`, and `slo` providers MUST continue to work with `providers.ConfigLoader` without any code changes (backward compatible).

## Acceptance Criteria

- GIVEN `GRAFANA_PROVIDER_SYNTH_SM_URL=https://sm.example.com` is set
  WHEN `LoadProviderConfig(ctx, "synth")` is called
  THEN it returns `sm-url` = `https://sm.example.com`

- GIVEN no env vars set AND the config file has `providers.synth.sm-url: https://file.sm`
  WHEN `LoadProviderConfig(ctx, "synth")` is called
  THEN it returns `sm-url` = `https://file.sm`

- GIVEN `GRAFANA_PROVIDER_SYNTH_SM_URL=https://env.sm` AND config file has `providers.synth.sm-url: https://file.sm`
  WHEN `LoadProviderConfig(ctx, "synth")` is called
  THEN it returns `sm-url` = `https://env.sm` (env var takes precedence)

- GIVEN a ConfigLoader
  WHEN `LoadGrafanaConfig(ctx)` is called
  THEN it behaves identically to the current implementation (backward compatible)

- GIVEN the synth provider after migration
  WHEN `synth checks list` is invoked with `GRAFANA_PROVIDER_SYNTH_SM_URL` and `GRAFANA_PROVIDER_SYNTH_SM_TOKEN` env vars
  THEN the command connects to the SM API at the URL using the token

- GIVEN the synth provider after migration
  WHEN `SaveMetricsDatasourceUID(ctx, "abc123")` is called
  THEN the config file at `contexts.[current].providers.synth.sm-metrics-datasource-uid` is set to `abc123`

- GIVEN the synth provider after migration
  WHEN `LoadConfig(ctx)` is called
  THEN a fully resolved `*config.Config` is returned

- GIVEN `GRAFANA_PROVIDER_ONCALL_ONCALL_URL=https://oncall.example.com` is set
  WHEN the oncall provider resolves its URL
  THEN it uses `https://oncall.example.com` as its base URL

- GIVEN no oncall URL in env vars or config
  WHEN the oncall provider resolves its URL
  THEN it falls back to Grafana plugin API discovery (existing behavior)

- GIVEN `GRAFANA_PROVIDER_K6_API_DOMAIN=https://custom.k6.io` is set
  WHEN k6 authenticatedClient is called
  THEN the client uses `https://custom.k6.io` as its API domain

- GIVEN no k6 api-domain in config or env vars
  WHEN k6 authenticatedClient is called
  THEN the client uses the hardcoded `DefaultAPIDomain` fallback

- GIVEN the alert provider (no changes expected)
  WHEN `alert rules list` is invoked
  THEN it uses `providers.ConfigLoader.LoadGrafanaConfig` exactly as before

- GIVEN the synth provider source code after migration
  WHEN `internal/providers/synth/provider.go` is inspected
  THEN the local `configLoader` struct, `loadConfig` method, `configSource` function, and `envOverride` function do NOT exist

- GIVEN the full test suite
  WHEN `make tests` is run
  THEN all tests pass including all synth, oncall, k6, fleet, alert, incidents, kg, and slo provider tests

## Negative Constraints

- NEVER change the config file schema at `contexts.[name].providers.[provider].[key]`. The on-disk format MUST remain identical.

- NEVER require any provider to implement a hooks interface or additional contract. The three new methods on ConfigLoader MUST work without provider-side opt-in.

- NEVER change the `Provider` interface (the 6-method contract in `internal/providers/provider.go`). All changes MUST be additive to `ConfigLoader`.

- DO NOT add provider-specific logic to `providers.ConfigLoader` itself. Provider-specific behavior (e.g., default values, fallback URLs) MUST remain in each provider package.

- DO NOT change how `oncall`'s Grafana plugin API discovery works. Only the env var check portion of `discoverOnCallURL` is standardized.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Synth migration breaks SM credential resolution due to differences between config.Load and config.LoadLayered | Users cannot authenticate to SM API | Write integration tests that verify env var resolution with LoadLayered before removing old code |
| Removing legacy GRAFANA_SM_URL/GRAFANA_SM_TOKEN breaks existing user workflows | Users must update env vars | Document migration in changelog; env var names follow standard GRAFANA_PROVIDER_ convention |
| Removing legacy GRAFANA_ONCALL_URL breaks existing oncall user workflows | Users must update env vars | Document migration in changelog |
| Synth SaveMetricsDatasourceUID write-back path differs between config.Load and config.LoadLayered | Config file corruption or lost writes | Verify SaveProviderConfig round-trips correctly: save then load and assert value persists |

## Open Questions

- [RESOLVED] Should we preserve legacy env var names (GRAFANA_SM_URL, GRAFANA_ONCALL_URL)? -- No. All providers use the standard GRAFANA_PROVIDER_<NAME>_<KEY> convention.

- [RESOLVED] Do we need a ProviderHooks interface (PostLoad, LegacyEnvOverrides)? -- No. No current provider needs hooks. YAGNI — add when a concrete use case arises.

- [RESOLVED] Should ConfigLoader support config write-back for arbitrary providers? -- Yes. SaveProviderConfig is the generalized mechanism.

- [DEFERRED] Should HTTP client construction be formalized into ConfigLoader? -- Out of scope. Current split documented but not enforced.
