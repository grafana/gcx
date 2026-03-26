---
type: feature-tasks
title: "ConfigLoader Compliance: Unified Config Loading Across All Providers"
status: draft
spec: docs/specs/feature-configloader-compliance/spec.md
plan: docs/specs/feature-configloader-compliance/plan.md
created: 2026-03-26
---

# Implementation Tasks

## Dependency Graph

```
T1 ──→ T2 ──→ T4
  ╲         ╱
   ──→ T3 ─
```

- T1: Add LoadProviderConfig, SaveProviderConfig, LoadFullConfig to ConfigLoader
- T2: Migrate synth provider to embedded ConfigLoader
- T3: Standardize oncall + k6 providers
- T4: Verification and cleanup

## Wave 1: Framework Extension

### T1: Add LoadProviderConfig, SaveProviderConfig, and LoadFullConfig to ConfigLoader

**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: task

Add three new methods to `providers.ConfigLoader`: `LoadProviderConfig` (loads config via `config.LoadLayered`, extracts named provider's config map from current context, returns provider config + namespace), `SaveProviderConfig` (loads config, sets a key in the named provider section, writes back), and `LoadFullConfig` (returns the full `*config.Config`).

**Deliverables:**
- `internal/providers/configloader.go` (extended with three new methods)
- `internal/providers/configloader_test.go` (new tests for all new methods)

**Acceptance criteria:**
- GIVEN `GRAFANA_PROVIDER_SYNTH_SM_URL=https://env.sm` is set
  WHEN LoadProviderConfig(ctx, "synth") is called
  THEN it returns sm-url = https://env.sm (AC-1)

- GIVEN no env vars AND config file has providers.synth.sm-url: https://file.sm
  WHEN LoadProviderConfig is called
  THEN it returns sm-url = https://file.sm (AC-2)

- GIVEN both env var and config file set
  WHEN LoadProviderConfig is called
  THEN env var takes precedence (AC-3)

- GIVEN a ConfigLoader
  WHEN LoadGrafanaConfig is called
  THEN it behaves identically to current implementation (AC-4)

- GIVEN a ConfigLoader
  WHEN SaveProviderConfig("synth", "sm-metrics-datasource-uid", "abc123") is called
  THEN the config file is updated and a subsequent load returns the value (AC-6)

- GIVEN a ConfigLoader
  WHEN LoadFullConfig is called
  THEN it returns a non-nil *config.Config (AC-7)

---

## Wave 2: Synth Migration

### T2: Migrate synth provider to embedded ConfigLoader

**Priority**: P0
**Effort**: Medium-Large
**Depends on**: T1
**Type**: task

Replace synth's local `configLoader` struct with an embedding of `providers.ConfigLoader`. Rewrite `LoadSMConfig` to call `LoadProviderConfig("synth")` and extract sm-url/sm-token from the returned map. Rewrite `SaveMetricsDatasourceUID` to call `SaveProviderConfig`. Rewrite `LoadConfig` to call `LoadFullConfig`. Delegate `LoadGrafanaConfig` to the embedded loader. Delete local `loadConfig()`, `envOverride()`, `configSource()` functions. Remove all `GRAFANA_SM_URL` / `GRAFANA_SM_TOKEN` legacy env var handling — use `GRAFANA_PROVIDER_SYNTH_SM_URL` / `GRAFANA_PROVIDER_SYNTH_SM_TOKEN` via standard convention. Switch from `config.Load` to `config.LoadLayered`. Ensure all smcfg interfaces are still satisfied.

**Deliverables:**
- `internal/providers/synth/provider.go` (rewritten configLoader, deleted dead code)
- `internal/providers/synth/provider_test.go` (updated/new tests)

**Acceptance criteria:**
- GIVEN GRAFANA_PROVIDER_SYNTH_SM_URL and GRAFANA_PROVIDER_SYNTH_SM_TOKEN are set
  WHEN synth configLoader.LoadSMConfig is called
  THEN it returns the correct baseURL, token, and namespace (AC-5)

- GIVEN a synth configLoader
  WHEN SaveMetricsDatasourceUID("prom-123") is called
  THEN the value persists to providers.synth.sm-metrics-datasource-uid and a subsequent load returns it (AC-6)

- GIVEN the synth provider source code after this task
  WHEN inspected
  THEN there is no local loadConfig method, no envOverride function, no configSource function (AC-13, FR-008)

- GIVEN the synth configLoader
  WHEN LoadConfig is called
  THEN it returns a valid *config.Config via LoadFullConfig (AC-7)

- GIVEN the synth configLoader type
  WHEN checked at compile time
  THEN it satisfies smcfg.Loader, smcfg.StatusLoader, smcfg.GrafanaConfigLoader, smcfg.ConfigLoader, and smcfg.DatasourceUIDSaver interfaces

---

## Wave 3: OnCall and K6 Standardization

### T3: Standardize oncall and k6 providers to use LoadProviderConfig

**Priority**: P1
**Effort**: Medium
**Depends on**: T1
**Type**: task

**OnCall (FR-009, FR-010):** Update `discoverOnCallURL` to call `LoadProviderConfig("oncall")` and check for `oncall-url` key before falling back to plugin discovery. Remove ad-hoc `os.Getenv("GRAFANA_ONCALL_URL")` and `os.Getenv("GRAFANA_PROVIDER_ONCALL_ONCALL_URL")` calls. The oncall-url key is now resolved via `GRAFANA_PROVIDER_ONCALL_ONCALL_URL` or config file.

**K6 (FR-011):** Update `authenticatedClient` in `k6/resource_adapter.go` to use `LoadProviderConfig("k6")` instead of `cfg.ProviderConfig("k6")`. The `DefaultAPIDomain` fallback MUST remain when `api-domain` is not in the returned config.

**Unchanged providers (FR-012):** Verify alert, fleet, incidents, kg, slo compile and their existing tests pass without modification.

**Deliverables:**
- `internal/providers/oncall/provider.go` (updated discoverOnCallURL)
- `internal/providers/k6/resource_adapter.go` (use LoadProviderConfig)
- `internal/providers/oncall/provider_test.go` (new/updated tests)

**Acceptance criteria:**
- GIVEN GRAFANA_PROVIDER_ONCALL_ONCALL_URL=https://oncall.example.com is set
  WHEN oncall resolves its URL
  THEN it uses https://oncall.example.com without calling plugin discovery (AC-8)

- GIVEN no oncall URL in env vars or config AND plugin discovery succeeds
  WHEN oncall resolves its URL
  THEN it returns the plugin-discovered URL (AC-9)

- GIVEN GRAFANA_PROVIDER_K6_API_DOMAIN=https://custom.k6.io is set
  WHEN k6 authenticatedClient is called
  THEN the client uses https://custom.k6.io (AC-10)

- GIVEN no k6 api-domain in config or env vars
  WHEN k6 authenticatedClient is called
  THEN the client uses DefaultAPIDomain (AC-11)

- GIVEN the alert provider source code
  WHEN this task is complete
  THEN zero lines in alert, fleet, incidents, kg, slo provider files have changed (AC-12)

---

## Wave 4: Verification

### T4: End-to-end verification and test suite pass

**Priority**: P1
**Effort**: Small
**Depends on**: T2, T3
**Type**: chore

Run `GRAFANACTL_AGENT_MODE=false make all` to verify the full build: linting, all unit tests, docs generation. Verify no test file references deleted synth functions. Confirm no regressions in unmodified providers.

**Deliverables:**
- Clean `make all` output (no lint errors, all tests pass, docs up to date)

**Acceptance criteria:**
- GIVEN all changes from T1, T2, T3 applied
  WHEN `GRAFANACTL_AGENT_MODE=false make all` is run
  THEN it completes with exit code 0 (AC-14)

- GIVEN the complete codebase after all changes
  WHEN searching for `func envOverride`, `func (l *configLoader) loadConfig`, `func (l *configLoader) configSource` in `internal/providers/synth/`
  THEN zero matches are found (AC-13)
