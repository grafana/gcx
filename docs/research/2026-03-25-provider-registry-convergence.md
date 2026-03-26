# Research Report: Provider Registry Convergence

*Generated: 2026-03-25 | Sources: 6 domain analyses | Overall Confidence: 93% (High)*

## Executive Summary

- grafanactl has two disconnected global registries (`providers.Register()` and `adapter.Register()`) with 13 `init()` functions across 8 providers registering 31 resource types
- The Provider interface is largely vestigial: `Validate()` is dead code at runtime (only synth has real logic), `ConfigKeys()` is consumed only for secret redaction in `config view`, and `ResourceAdapters()` is superseded by direct `init()` registration in 6/8 providers
- Non-CRUD commands are significant and irreducible in 6/8 providers — SLO (status/timeline), OnCall (escalate, silence), K6 (testrun, auth), KG (15+ analytical commands), Incidents (close, activity), Alert (groups status)
- ConfigKeys/Validate should move to a new `ProviderMeta` type (not into Registration), because Registration is per-resource while config is per-provider — embedding creates duplication for multi-resource providers
- A single `RegisterProvider()` call can replace the dual `init()` pattern, populating both registries atomically and enabling `providers list` to work from a unified source

## Confidence Assessment

| Section | Score | Rationale |
|---------|-------|-----------|
| Minimal Provider Interface | 95% | All 8 provider implementations read with line-level citations[1][2][3] |
| ConfigKeys/Validate Placement | 94% | Complete call-chain traced; only consumer identified[4][5] |
| Lazy Initialization | 97% | Factory pattern, router, sync.Once all fully traced[6][7][8] |
| Non-CRUD Command Inventory | 92% | Most commands enumerated; KG tail and fleet tenant not fully read[2] |
| `providers list` from Registrations | 96% | Both registries and all consumers fully mapped[9][10] |
| Test Migration Path | 88% | Test patterns observed but not all test files read exhaustively |
| Init Pattern Merge | 97% | All 13 init() functions cataloged with exact line references[11] |
| **Overall** | **93%** | High confidence — grounded in direct code reading across all providers[1][2][3][4][5] |

---

## 1. What Is the Minimal Provider Interface After CRUD Commands Are Folded into Resources?

### Current Interface

```go
// internal/providers/provider.go:16-40[1]
type Provider interface {
    Name() string
    ShortDesc() string
    Commands() []*cobra.Command
    Validate(cfg map[string]string) error
    ConfigKeys() []ConfigKey
    ResourceAdapters() []adapter.Factory
}
```

### Method-by-Method Analysis

| Method | Real Logic | Providers with Real Logic | Verdict |
|--------|-----------|--------------------------|---------|
| `Name()` | Trivial literal | All (string return) | **Keep** — needed for `providers list` and config key namespace |
| `ShortDesc()` | Trivial literal | All (string return) | **Keep** — needed for `providers list` display |
| `Commands()` | Always real | All 8 | **Transform** → `ExtraCommands()` returning only non-CRUD commands |
| `Validate()` | Only synth | 1/8 (synth checks sm-url/sm-token) | **Remove from interface** — dead at runtime; synth validates in its own config loader |
| `ConfigKeys()` | synth (3 keys), k6 (1 key) | 2/8 | **Move** — to config metadata, not the Provider interface[4][5] |
| `ResourceAdapters()` | slo, synth return non-nil | 2/8 (and both also register via init()) | **Remove** — fully superseded by `adapter.Register()` in init()[11] |

### Proposed Minimal Interface

After CRUD commands fold into the resources pipeline, the Provider interface reduces to:

```go
// Minimal Provider — only for providers that contribute non-CRUD CLI commands
type Provider interface {
    Name() string
    ShortDesc() string
    ExtraCommands() []*cobra.Command  // non-CRUD commands only; may return nil
}
```

**Providers that would implement this** (have non-CRUD commands):
- **slo** — `status`, `timeline` (for both definitions and reports)
- **oncall** — `escalate`, `alert-groups silence/unsilence`, `users current`, `schedules final-shifts`
- **k6** — `testrun`, `auth token`
- **kg** — 15+ analytical/lifecycle commands (setup, inspect, assertions, search, health, etc.)
- **incidents** — `close`, `open`, `activity list/add`, `severities list`
- **fleet** — `tenant`
- **alert** — `groups status`

**Providers that could drop Provider entirely** (all commands are CRUD)[3]:
- **synth** — all commands (checks + probes) are list/get/push/pull/delete

However, synth is the only provider with meaningful `ConfigKeys()` and `Validate()`[4][5], so it would still need config registration even without a Provider interface.

---

## 2. Should ConfigKeys/Validate Move to Registration or to a Separate ProviderConfig Type?

### Why NOT into Registration

Registration is a **per-resource-type** struct[12] (31 instances across 8 providers). ConfigKeys and Validate are **per-provider** concerns[4]:

```
synth provider: 3 config keys (sm-url, sm-token, sm-metrics-datasource-uid)
  ├── synth.Check registration  ← would duplicate all 3 keys
  └── synth.Probe registration  ← would duplicate all 3 keys

oncall provider: 0 config keys (URL auto-discovered)
  ├── oncall.Integration registration  ← 0 keys × 17 registrations = noise
  ├── oncall.EscalationChain registration
  └── ... (15 more)
```

Embedding provider-level config into resource-level Registration would create:
1. **Duplication** — multi-resource providers (oncall=17, k6=5, synth=2) would carry identical config keys on every registration
2. **Mapping loss** — the clean `providerName → configKeys` relationship (used by `config view` redaction) would require re-derivation by grouping registrations
3. **Conceptual mismatch** — config keys describe how to authenticate to a *product API*, not how to manage a *resource type*

### Recommended: Separate ProviderMeta Type

```go
// ProviderMeta carries provider-level identity and config contract.
// One per provider. Registered alongside resource adapters.
type ProviderMeta struct {
    Name      string              // e.g. "synth", "oncall"
    ShortDesc string              // for `providers list`
    ConfigKeys []ConfigKey        // for secret redaction in `config view`
    Validate   func(map[string]string) error  // optional; nil for most providers
}
```

This cleanly separates the three concerns:
1. **Resource adapters** (Registration) — per-resource-type, consumed by resources pipeline
2. **Provider metadata** (ProviderMeta) — per-provider, consumed by `providers list` and `config view`
3. **Extra CLI commands** (Provider interface) — per-provider, consumed by root command mounting

### Current ConfigKeys/Validate Usage (runtime)

| Consumer | What it reads | Where |
|----------|--------------|-------|
| `config view` (non-raw) | `ConfigKeys()` via `providers.RedactSecrets()`[4] | `cmd/grafanactl/config/command.go:251` |
| **Nothing** | `Validate()`[5] | Dead at runtime — only in tests |

`Validate()` is called in exactly 0 production code paths[5]. Even `config check` only validates `Context.Grafana` (server URL, org-id/stack-id) — it never calls `Provider.Validate()`. Synth's real validation happens inside `LoadSMConfig()`[5], not through the interface method.

**Recommendation**: Keep `ConfigKeys` on `ProviderMeta` for redaction. Drop `Validate` from the interface entirely — providers that need validation do it in their config loaders already.

---

## 3. How Does Lazy Initialization Work When Registration Owns Config?

### Current Lazy Init Flow

```
init() time (eager, startup)[11]
  adapter.Register(Registration{Factory: closureOverLoader, ...})
    → appends to global []Registration slice
    → NO config loaded, NO HTTP calls

Command setup time
  adapter.RegisterAll(ctx, discoveryRegistry)[12]
    → discoveryRegistry.RegisterAdapter(factory, desc, aliases)
    → factories stored in registry by GVK

First resource access (lazy, on-demand)[7]
  ResourceClientRouter.getAdapter(ctx, gvk)
    → sync.Once per GVK
    → factory(ctx) invoked ONCE
    → factory reads config from disk/env via captured loader
    → returns (ResourceAdapter, error)
    → adapter cached in router.instances[gvk]
```

### Config Loading Patterns in Factories

Three patterns exist, all compatible with Registration owning config metadata:

| Pattern | Used By | Config Source | Loader |
|---------|---------|--------------|--------|
| **Grafana REST** | SLO, OnCall | Standard server/token from kubeconfig-style config[8] | `providers.ConfigLoader` |
| **Provider-specific** | Synth | `providers.synth.sm-url/sm-token` from config or `GRAFANA_SM_*` env vars[5] | `synth.configLoader` (implements `smcfg.Loader`) |
| **Cloud GCOM** | K6, Fleet, KG, Incidents | Cloud token → GCOM API → stack info + optional provider-specific keys[8] | `providers.ConfigLoader.LoadCloudConfig()` |

### Key Insight: Config Metadata ≠ Config Loading

`ConfigKeys` is **metadata** (what keys exist, which are secrets) — used only for display/redaction. The factory's config **loader** independently reads the same values from disk/env at invocation time. There is no plumbing from `ConfigKeys → factory`. This means:

1. Moving ConfigKeys to ProviderMeta has **zero impact** on factory initialization
2. The factory closure captures a stateless loader at `init()` time
3. The loader reads config lazily when the factory is invoked
4. Context name (which config context to use) is threaded via `context.Context`

### Would Registration-Owned Config Change the Factory Signature?

**No.** The factory signature `func(ctx context.Context) (ResourceAdapter, error)` does not need to change. Config loading is already encapsulated in the closure. The ProviderMeta's ConfigKeys would be consumed by `config view` redaction — a completely separate path from factory invocation.

If we wanted **pre-validated config injection** (future optimization), Option C from the factory analysis is the natural extension: the Registration provides a factory-creator that closes over the same loader used for validation. But this is optional — the current deferred-validation pattern works and is well-understood.

---

## 4. Which Providers Have Non-CRUD Commands That Cannot Fold into Resources?

### Complete Inventory

| Provider | Non-CRUD Commands | Nature |
|----------|------------------|--------|
| **SLO** | `definitions status`, `definitions timeline`, `reports status`, `reports timeline`[2] | PromQL-based observability queries + terminal chart rendering |
| **OnCall** | `escalate`, `alert-groups {silence, unsilence}`, `users current`, `schedules final-shifts`[2] | Actions (escalation trigger), temporal mutations (silence duration), identity query, computed view |
| **K6** | `testrun`, `auth token`[2] | Process exec (`exec.Command` to k6 binary), token management |
| **KG** | `setup`, `enable`, `status`, `health`, `open`, `inspect`, `datasets activate`, `assertions {query, summary, graph, active, entity-metric, source-metrics, example}`, `search {assertions, sample, entities, example}`[2] | Lifecycle ops, deep analytics, free-form search — 15+ commands |
| **Incidents** | `close`, `open`, `activity {list, add}`, `severities list`[2] | State machine transition, browser launch, event log, reference data |
| **Fleet** | `tenant`[2] | Tenant-level configuration (not a pipeline/collector resource) |
| **Alert** | `groups status`[2] | Evaluation state check (not a resource field read) |
| **Synth** | *(none)*[3] | All commands are standard CRUD on checks/probes |

### Providers After CRUD Folding

```
Post-convergence command tree:

grafanactl resources {get, push, pull, delete, schemas, examples}
  └── handles ALL 31 registered resource types uniformly

grafanactl slo
  ├── status <uuid>          (error budget chart)
  └── timeline <uuid>        (burn rate chart)

grafanactl oncall
  ├── escalate               (fire escalation)
  ├── alert-groups {silence, unsilence, acknowledge, resolve, ...}
  ├── users current
  └── schedules final-shifts <id>

grafanactl k6
  ├── testrun                (exec k6 binary)
  └── auth token

grafanactl kg               (15+ commands — almost entirely non-CRUD)
  └── {setup, enable, status, health, open, inspect, datasets activate,
       assertions {query, summary, graph, ...}, search {...}}

grafanactl incidents
  ├── close <id>
  ├── open <id>
  ├── activity {list, add}
  └── severities list

grafanactl fleet
  └── tenant

grafanactl alert
  └── groups status
```

**Providers that effectively disappear as top-level commands**: synth (all CRUD). Alert would shrink to just `groups status`.

---

## 5. Can `providers list` Work from `adapter.AllRegistrations()` Alone?

### Answer: No — but it can work from a unified ProviderMeta registry.

**Why AllRegistrations() is insufficient:**

1. **Wrong abstraction level**: AllRegistrations() returns 31 resource-type entries, not 8 provider entries. There's no `ProviderName` field to group by.
2. **No provider metadata**: Registration has no `Name()` or `ShortDesc()` — these are Provider interface methods.
3. **No back-pointer**: Given a Registration for `oncall.Integration`, there's no way to find which Provider owns it.

**Current `providers list` implementation**[9] (`cmd/grafanactl/providers/command.go:41-106`):

```go
for _, p := range providers.All() {
    items = append(items, providerItem{Name: p.Name(), Description: p.ShortDesc()})
}
```

This iterates the `providers.registry` slice — completely separate from `adapter.registrations`[12].

### Solution: ProviderMeta Registry

With a `ProviderMeta` type registered alongside adapters[9][10]:

```go
// Single registration call in init():
provider.Register(ProviderMeta{
    Name:      "oncall",
    ShortDesc: "Manage Grafana OnCall resources.",
    ConfigKeys: nil,  // URL auto-discovered
}, []adapter.Registration{...})  // 17 resource types
```

Then `providers list` reads from the ProviderMeta registry (8 entries), while `resources get` reads from the adapter registry (31 entries). Both populated by the same `Register()` call[10].

**Bonus**: A `providers list --resources` flag could cross-reference both registries to show resource types per provider.

---

## 6. What Is the Migration Path for Existing Provider Tests?

### Current Test Patterns

Provider tests fall into three categories:

1. **Interface compliance tests** (`internal/providers/provider_test.go`):
   - Iterates `providers.All()`, checks `Name()` non-empty, `Commands()` non-nil
   - Calls `Validate()` with empty config — tests the method exists and doesn't panic

2. **Per-provider validation tests** (e.g., `slo/provider_test.go`, `synth/provider_test.go`, `alert/provider_test.go`):
   - Test `Validate()` with specific config maps
   - Test `ConfigKeys()` returns expected keys

3. **Adapter registration tests** (e.g., `k6/resource_adapter_test.go`):
   - Calls `adapter.AllRegistrations()`, looks up by GVK
   - Verifies Descriptor, Aliases, Schema, Example fields

### Migration Strategy

**Phase 1 — No test changes needed** (steps 3-4 of the migration path):
- CRUD commands become deprecation shims → existing CLI tests still pass
- Provider interface unchanged → all interface tests still pass

**Phase 2 — Test updates for ProviderMeta extraction** (step 5):
- `provider_test.go` generic loop: update to iterate new ProviderMeta registry instead of `providers.All()`
- Per-provider `Validate()` tests: for synth, move validation test to config loader test. For others (returning nil), delete trivially.
- Per-provider `ConfigKeys()` tests: move to ProviderMeta unit tests

**Phase 3 — Test updates for unified Register()** (step 6):
- Adapter registration tests (`k6/resource_adapter_test.go`): unchanged — `adapter.AllRegistrations()` still works
- Add new tests verifying that `Register()` populates both registries atomically
- `cmd/grafanactl/root/command_test.go`: update mock provider to match new interface

**Phase 4 — Cleanup** (step 7):
- Delete `ResourceAdapters()` tests (already nil in 6/8 providers)
- Delete `Validate()` from Provider interface → compile errors guide removal

### Key Risk: Test Coupling to Provider Interface

The main risk is `cmd/grafanactl/root/command_test.go` and `cmd/grafanactl/providers/command_test.go` which use mock providers implementing the full interface. These would need updating when the interface changes. The fix is mechanical — remove methods from mock structs.

---

## 7. Should the Two `init()` Patterns Merge into a Single `Register()` Call?

### Current Patterns

**13 init() functions** across 8 providers, using 3 patterns:

| Pattern | Providers | Description |
|---------|-----------|-------------|
| **Split** (2 init() per provider) | alert, incidents, k6, kg | `provider.go:init()` → `providers.Register()`, `resource_adapter.go:init()` → `adapter.Register()` |
| **Combined** (1 init()) | fleet, oncall, synth | Both `providers.Register()` and `adapter.Register()` in one `init()` |
| **Sub-package** | slo | `slo/provider.go:init()` → `providers.Register()`, `slo/definitions/resource_adapter.go:init()` → `adapter.Register()` (via Go import ordering) |

### Answer: Yes — merge into a single `RegisterProvider()` call.

**Benefits:**
1. **Atomicity** — impossible to register a Provider without its adapters (or vice versa)
2. **Discoverability** — one place to see everything a provider contributes
3. **Cross-reference** — ProviderMeta and Registrations linked at registration time
4. **Reduced init() count** — 13 → 8 (one per provider package)

### Proposed API

```go
// In a new or extended registration package:
func RegisterProvider(meta ProviderMeta, resources []Registration, extra func() []*cobra.Command) {
    // 1. Store ProviderMeta in provider-level registry
    providerRegistry = append(providerRegistry, meta)

    // 2. Store each Registration in adapter-level registry
    for _, r := range resources {
        adapter.Register(r)
    }

    // 3. If extra commands provided, store for root command mounting
    if extra != nil {
        commandRegistry[meta.Name] = extra
    }
}
```

### Example: Synth provider migration

**Before** (combined pattern, `synth/provider.go`)[3][11]:
```go
func init() {
    providers.Register(&SynthProvider{})  // Provider registry
    adapter.Register(Registration{...})   // Check adapter
    adapter.Register(Registration{...})   // Probe adapter
}
```

**After** (single call)[11]:
```go
func init() {
    registration.RegisterProvider(
        ProviderMeta{
            Name:      "synth",
            ShortDesc: "Manage Grafana Synthetic Monitoring resources.",
            ConfigKeys: []ConfigKey{
                {Name: "sm-url", Secret: false},
                {Name: "sm-token", Secret: true},
                {Name: "sm-metrics-datasource-uid", Secret: false},
            },
        },
        []Registration{
            {Factory: checks.NewAdapterFactory(loader), ...},
            {Factory: probes.NewAdapterFactory(loader), ...},
        },
        nil,  // synth has no non-CRUD commands post-migration
    )
}
```

### Migration Order for init() Consolidation

Providers can be migrated incrementally, one at a time[11]:

1. **synth** — simplest case (all CRUD, no extra commands post-migration, combined pattern already)[3][11]
2. **alert** — read-only, split pattern, one non-CRUD command (`groups status`)[2]
3. **fleet** — combined pattern, one extra command (`tenant`)[2][11]
4. **incidents** — split pattern, a few extra commands[2][11]
5. **slo** — sub-package pattern (requires collapsing sub-package init)[11]
6. **oncall** — combined pattern, 17 resources + several extra commands[2][11]
7. **k6** — split pattern, 5 resources + testrun/auth[2][11]
8. **kg** — split pattern, 1 resource + 15+ extra commands (mostly non-CRUD)[2][11]

---

## 8. Concrete Incremental Migration Plan

Building on the stub's hypothesis and grounded in code analysis:

### Phase 1: Foundation (already done)
- [x] TypedResourceAdapter foundation (ResourceIdentity, TypedObject, typed methods)
- [x] Provider CRUD commands migrate to TypedCRUD

### Phase 2: ProviderMeta Type + Unified Registration API
**Goal**: Define the target registration API without breaking anything.

1. Create `ProviderMeta` struct in `internal/providers/` (or `internal/resources/adapter/`)
2. Create `RegisterProvider(meta, resources, extraCmds)` function
3. Add `AllProviderMeta() []ProviderMeta` accessor
4. **No changes to existing code yet** — this is additive only

### Phase 3: Migrate `providers list` to ProviderMeta
**Goal**: `providers list` reads from ProviderMeta registry.

1. Migrate one provider (synth) to use `RegisterProvider()` — proves the API
2. Keep backward compat: `providers.All()` still works for unmigrated providers
3. `providers list` reads from both sources during migration
4. Migrate remaining 7 providers one at a time (order from section 7)
5. Once all migrated, delete `providers.Register()` and `providers.All()`

### Phase 4: CRUD Command Deprecation Shims
**Goal**: Provider CRUD commands print deprecation warnings and delegate to resources pipeline.

1. For each provider with CRUD commands, replace `list/get/push/pull/delete` implementations with shims that print "Use `grafanactl resources get <type>` instead" and delegate
2. Keep command tree structure for backward compat (aliases)
3. Test that both paths produce identical output

### Phase 5: Remove Vestigial Interface Methods
**Goal**: Clean up the Provider interface.

1. Remove `ResourceAdapters()` from interface (already nil in 6/8, superseded in 2/8)
2. Remove `Validate()` from interface (dead at runtime)
3. Remove `ConfigKeys()` from interface (moved to ProviderMeta)
4. Update test mocks to match

### Phase 6: Remove CRUD Command Shims
**Goal**: Provider packages only contribute non-CRUD commands.

1. Remove deprecated CRUD shims after sufficient deprecation period
2. Providers with no remaining commands (synth) drop Provider interface entirely
3. Final Provider interface: `Name() + ShortDesc() + ExtraCommands()`

---

## Areas of Uncertainty

⚠️ **[SPECULATIVE]**: The migration order for init() consolidation (section 7) is based on estimated complexity, not verified dependencies[11]. Actual order may need adjustment based on test coupling.

🔍 **[NEEDS VERIFICATION]**: `fleet tenant` command — categorized as non-CRUD based on naming, but the command body was not read[2]. Could potentially be a resource operation.

🔍 **[NEEDS VERIFICATION]**: Whether any integration or e2e test exercises `Provider.Validate()` indirectly through a path not captured by the grep analysis[5].

📝 **[SINGLE SOURCE]**: The `ResourceAdapters()` vestigial status is inferred from comments in provider files and absence of non-init call sites, but a comprehensive grep for `ResourceAdapters()` invocations was not performed across all findings[11].

⚠️ **[SPECULATIVE]**: The exact `RegisterProvider()` API signature is a design proposal, not derived from code. The actual implementation may need adjustment for backward compatibility during migration.

## Knowledge Gaps

- **SLO Reports adapter status**: Reports have no ResourceAdapter and no init() registration. Whether this is intentional or an oversight is unclear — reports may need adapter registration in the future.
- **`sync.Once` failure behavior**: If a factory fails, the adapter is permanently broken for the router's lifetime (no retry). This is a potential operational concern not addressed by the convergence.
- **Init ordering between sibling providers**: Registration order in the global slices is non-deterministic across Go builds. No code currently depends on this, but a unified registry should document this constraint.
- **Env var parsing duplication**: Three identical copies of `GRAFANA_PROVIDER_*` parsing exist. This is a pre-existing code smell orthogonal to convergence but worth addressing alongside.

## Synthesis Notes

- **Domains covered**: 6 (adapter registration, CLI wiring, config/validation, factory pattern, init patterns, provider interface)
- **Total source files analyzed**: 40+ across all domains
- **Contradictions found**: 1 minor (see contradictions.md)
- **Confidence rationale**: All findings grounded in direct code reading with file:line citations. High agreement across domains. Minor gaps in KG command enumeration and fleet tenant body.
- **Limitations**: Analysis is static (code reading only). No runtime testing or profiling was performed. Some large files (kg/commands.go, k6/commands.go) were not read exhaustively.

---

## References

[1] Provider Interface Analysis. `internal/providers/provider.go:16-40`. Analysis of all 8 provider implementations covering Name, ShortDesc, Commands, Validate, ConfigKeys, ResourceAdapters methods.

[2] Provider Interface Analysis. Complete inventory of non-CRUD commands across SLO, OnCall, K6, KG, Incidents, Fleet, and Alert providers from files: `internal/providers/slo/definitions/commands.go`, `internal/providers/oncall/commands.go`, `internal/providers/k6/commands.go`, `internal/providers/kg/commands.go`, `internal/providers/incidents/commands.go`, `internal/providers/fleet/provider.go`, `internal/providers/alert/rules_commands.go`.

[3] Provider Interface Analysis. Synth provider commands analysis. `internal/providers/synth/provider.go:93-113`. Commands structure for checks and probes CRUD operations.

[4] Provider Config/Validation Flow — Research Findings. ConfigKeys() consumers and runtime usage. `cmd/grafanactl/config/command.go:251` and `internal/providers/redact.go:16-40` for secret redaction implementation.

[5] Provider Config/Validation Flow — Research Findings. Validate() method analysis showing dead code at runtime. Analysis of config check path in `cmd/grafanactl/config/command.go:342-382` and synth's private validation in `internal/providers/synth/provider.go:162-214`.

[6] Adapter Factory Pattern — Research Findings. Factory pattern implementation showing lazy initialization. `internal/resources/adapter/adapter.go:47-50` and `internal/resources/adapter/register.go:41-43` for registration flow.

[7] Adapter Factory Pattern — Research Findings. ResourceClientRouter lazy initialization via sync.Once. `internal/resources/adapter/router.go:62-88` showing getAdapter() invocation pattern.

[8] Adapter Factory Pattern — Research Findings. Three config loading patterns: Grafana REST (SLO, OnCall), Provider-specific (Synth), Cloud GCOM (K6, Fleet, KG, Incidents). Analyzed in `internal/providers/slo/definitions/resource_adapter.go:85-97`, `internal/providers/synth/provider.go:161-215`, `internal/providers/k6/resource_adapter.go:91-115`.

[9] CLI Wiring: Provider Commands into Command Tree. `cmd/grafanactl/providers/command.go:22-73` showing providers list command implementation.

[10] Adapter Registration System Analysis. ProviderMeta registry concept to replace dual registry pattern. Cross-references both `providers.registry` and `adapter.registrations` from `internal/providers/registry.go:1-19` and `internal/resources/adapter/register.go:23-32`.

[11] init() Registration Pattern Analysis. Complete inventory of 13 init() functions across providers. Detailed catalog at lines 8-27 with locations: `alert/provider.go:11`, `alert/resource_adapter.go:50`, `incidents/provider.go:11`, `incidents/resource_adapter.go:34`, `k6/provider.go:11`, `k6/resource_adapter.go:61`, `kg/provider.go:11`, `kg/resource_adapter.go:41`, `fleet/provider.go:90`, `oncall/provider.go:17`, `synth/provider.go:21`, `slo/provider.go:11`, `slo/definitions/resource_adapter.go:33`.

[12] Adapter Registration System Analysis. Registration struct and AllRegistrations() usage. `internal/resources/adapter/register.go:25-32` and `internal/resources/adapter/register.go:53-61` for adapter registration and discovery flow.

---

*Research conducted: March 25, 2026*
*Sources: 6 research domain files analyzing 40+ code locations across grafanactl codebase*
*Citation count: 32 inline markers across 12 unique sources*
