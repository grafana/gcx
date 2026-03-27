# Extending gcx to Support the Entirety of Grafana Cloud

*Generated: 2026-03-02 | Sources: 7 | Citations: 43 | Confidence: 88% (High)*

---

## 1. Executive Summary

gcx today is a kubectl-style CLI that manages Grafana resources through Grafana 12's Kubernetes-compatible `/apis` endpoint[1]. It handles dashboards, folders, alerting rules, and other app-platform resources using `k8s.io/client-go` directly[1]. This path works well and should remain the primary mechanism for app-platform resources.

The problem is that this covers roughly 12% of the Grafana Cloud API surface[2]. The Terraform provider for Grafana manages approximately 97 resources across 13 product areas[2]. Of those, only 12 (the "appplatform" resources) use the k8s API that gcx can speak[2]. The remaining 85 resources -- spanning SLO, Synthetic Monitoring, OnCall, k6 Cloud, Machine Learning, Fleet Management, Cloud Provider integrations, Connections, Frontend Observability, and Asserts -- use product-specific REST APIs, OpenAPI-generated clients, dedicated SDKs, and in one case ConnectRPC[2]. None of these APIs use the Kubernetes envelope format (`apiVersion`/`kind`/`metadata`/`spec`). They return flat JSON with product-specific schemas[7].

**The recommendation:**

- **Adopt a Provider interface in the monorepo** that lets each Grafana Cloud product register its own command group (`gcx slo`, `gcx k6`, `gcx synth`) at compile time[5]. Each provider owns its own HTTP client, command tree, and flat-JSON-to-resource translation adapter. This ships product CLIs immediately with zero risk to the existing `resources` command.

- **Add a `ConfigKeys()` method to the Provider interface** so providers can declare their extra credentials (SM token, k6 token + stack ID) without modifying the core config struct[5]. Use env var overrides (`GRAFANA_SM_TOKEN`, `GRAFANA_K6_TOKEN`) and context-level provider config blocks.

- **Plan for Approach C convergence**: after 3-5 providers are stable, add `RegistryIndex.Populate([]Descriptor)` (~20 LOC) and modify `fetch.go`/`push.go` injection points (~50 LOC) so that `gcx resources push slo/my-rule` works as a unified alias[6]. The product-specific command groups remain as convenient shortcuts.

---

## 2. Grafana Cloud API Surface Map

**Confidence: HIGH (94%) -- sourced from exhaustive Terraform provider catalog with line-level verification[2]**

| Product Area | API Type | Base URL Pattern | Auth Mechanism | Resource Count | Difficulty | Priority |
|---|---|---|---|---|---|---|
| **App Platform (k8s)** | k8s `/apis` | `{grafana_url}/apis` | Grafana SA token (Bearer) | 12 | N/A (done) | N/A |
| **SLO** | REST (OpenAPI) | `{grafana_url}/api/plugins/grafana-slo-app/resources/v1/slo` | Grafana SA token (same) | 1 | **Low** | **High** |
| **Synthetic Monitoring** | REST (SDK) | `https://synthetic-monitoring-api.grafana.net/api/v1` | Separate SM access token | 3 | **Medium** | **High** |
| **OnCall** | REST (SDK) | `https://oncall-prod-*.grafana.net/oncall` | OnCall token OR Grafana SA token | 8 | **Medium** | **High** |
| **Machine Learning** | REST (SDK) | `{grafana_url}/api/plugins/grafana-ml-app/resources` | Grafana SA token (plugin proxy) | 4 | **Medium** | **Medium** |
| **k6 Cloud** | REST (OpenAPI) | `https://api.k6.io/cloud/v6` | k6 token + `X-Stack-Id` header | 5 | **High** | **Medium** |
| **Grafana Cloud** | REST (gcom) | `https://grafana.com` | Cloud Access Policy token | 13 | **Medium** | **Medium** |
| **Cloud Provider** | REST (custom) | Configurable | Cloud Provider token (Bearer) | 4 | **Medium** | **Low** |
| **Connections** | REST (custom) | `https://connections-api.grafana.net` | Connections token (Bearer) | 1 | **Low** | **Low** |
| **Fleet Management** | ConnectRPC | Configurable | Basic auth (user:pass) | 2 | **High** | **Low** |
| **Frontend O11y** | REST (custom) | Configurable | FO11y token OR Cloud token fallback | 1 | **Low** | **Low** |
| **Asserts** | REST (OpenAPI) | `{grafana_url}/api/plugins/grafana-asserts-app/resources/...` | Grafana SA token (plugin proxy) | 9 | **Medium** | **Low** |
| **Grafana OSS REST** | REST (OpenAPI) | `{grafana_url}/api/*` | Grafana SA token or user:pass | 34 | **Medium** | **Low** |

**Totals**: ~97 resources across 13 product areas[2]. gcx currently covers 12 resources (the App Platform row)[2]. Coverage gap: ~88%.

**Key observations about auth[7]**:

```
Credential tier 1 (shared Grafana token):
  SLO, Machine Learning, Asserts, Grafana OSS REST, App Platform
  -> Zero config changes needed. Same server + same token.

Credential tier 2 (separate token, same auth scheme):
  Synthetic Monitoring, OnCall, k6 Cloud, Cloud Provider,
  Connections, Frontend O11y, Grafana Cloud
  -> Each needs its own token. Different base URLs.

Credential tier 3 (different auth scheme):
  Fleet Management (basic auth, not Bearer)
  -> Needs basic auth support in provider config.
```

---

## 3. Current Architecture Analysis

**Confidence: HIGH (96%) -- direct codebase analysis with line-number precision[1]**

### What gcx Does Well

The existing architecture is clean and well-layered for its current scope[1]:

```
CLI Layer (cmd/gcx/)          <- Cobra commands, zero business logic
    |
Business Logic (internal/resources/) <- Resource model, selectors, filters, processors
    |
Client Layer (internal/resources/dynamic/) <- k8s dynamic client wrapper
    |
Grafana REST API (/apis endpoint)    <- K8s-compatible API (Grafana 12+)
```

Strengths[1]:
- **Clean Options pattern**: Every command uses `opts struct` + `setup(flags)` + `Validate()` + constructor injection[1]. This is the exact pattern new providers should follow.
- **Composable Processor pipeline**: `Processor.Process(*Resource) error` is a clean interface that separates transformation concerns from I/O[1]. The interface itself is backend-agnostic even though current implementations are k8s-specific.
- **Bounded concurrency**: `errgroup` with configurable parallelism (default 10) for batch operations[1]. Grizzly lacks this entirely (sequential iteration)[3].
- **Config system**: `GrafanaConfig` (server, token, TLS, org/stack ID) is completely k8s-agnostic[1]. Only `NamespacedRESTConfig` (which embeds `rest.Config`) adds k8s coupling.

### The K8s Coupling Map

Every public interface in gcx was classified by k8s coupling level[1]. The full analysis covers 7 interfaces and 5 core types. Here is the summary:

```
TIGHTLY K8S-COUPLED (must be replaced or bypassed for non-k8s backends):
  discovery.Client.ServerGroupsAndResources()
    -> Returns []*metav1.APIGroup, []*metav1.APIResourceList
    -> This IS the k8s API discovery protocol. No equivalent in REST APIs.

  RegistryIndex.Update()
    -> Takes []*metav1.APIGroup, []*metav1.APIResourceList directly
    -> Populates the descriptor registry from k8s discovery response

  PushClient / PullClient interfaces
    -> Use *unstructured.Unstructured as the universal payload type
    -> Use metav1.CreateOptions/UpdateOptions/GetOptions/ListOptions
    -> PushClient.Get() -> existing.GetResourceVersion() in upsert logic
       (k8s optimistic concurrency concept; no REST API equivalent)

MODERATELY COUPLED (wrappable or bypassable):
  resources.Resource
    -> Wraps unstructured.Unstructured + GrafanaMetaAccessor
    -> Any map[string]any CAN be wrapped in Unstructured
    -> But GrafanaMetaAccessor expects apiVersion/kind/metadata/spec shape

  resources.Descriptor
    -> Contains schema.GroupVersion (literally just {Group, Version string})
    -> GroupVersionResource() builds k8s URL paths (/apis/{g}/{v}/namespaces/...)
    -> Replaceable with local two-string struct at low cost

  NamespacedRESTConfig
    -> Embeds rest.Config with APIPath="/apis", QPS/Burst k8s rate limiting
    -> Auth fields (BearerToken, User/Pass, TLS) are fully generic

CLEAN (no k8s coupling, fully reusable):
  Selector, PartialGVK, Filter
    -> Plain strings, no k8s type imports
    -> Semantics are k8s-shaped but types are portable

  Processor interface
    -> Takes *resources.Resource; no k8s types on boundary
    -> Implementations are k8s-specific but interface is clean

  PushRegistry / PullRegistry
    -> Returns resources.Descriptors (gcx type)

  GrafanaConfig
    -> Most portable type in the codebase
```

### The One Strategic Seam: `RegistryIndex.Populate([]Descriptor)`

**Confidence: HIGH (3 sources agree)[1][2][3]**

The `RegistryIndex` is the gateway between API discovery and the selector/filter pipeline[1]. Today it is populated exclusively by `RegistryIndex.Update()` which takes k8s discovery protocol types. But everything ABOVE this seam -- `Registry`, `MakeFilters`, `Selectors`, `Filters`, `Descriptors` -- is backend-agnostic.

If a `Populate([]Descriptor)` method were added (~20 LOC)[6], any backend that can produce a `[]Descriptor` manifest would integrate with the existing selector/filter pipeline without any other changes[6]. The discovery protocol becomes a pluggable detail rather than a hardwired assumption.

This is the eventual Approach C convergence point[6]. It does not need to ship with the initial provider model.

### The Two Clean Extension Points

The Pusher and Puller constructors already accept injected interfaces[1]:

```go
// remote/pusher.go -- NewPusher accepts ANY PushClient implementation
func NewPusher(client PushClient, registry PushRegistry, ...) *Pusher

// remote/puller.go -- NewPuller accepts ANY PullClient implementation
func NewPuller(client PullClient, registry PullRegistry, ...) *Puller
```

The problem is that the callers (in `fetch.go` and `push.go`) hardcode the k8s implementations[1]:

```go
// fetch.go line 53 -- always creates k8s client
pull, err := remote.NewDefaultPuller(ctx, opts.Config)

// push.go line 136 -- always creates k8s client
pusher, err := remote.NewDefaultPusher(ctx, cfg)
```

Changing these two call sites to accept injected client implementations (~50 LOC)[1] is all that is needed for Approach C. But this is deferred -- Approach A (parallel command groups via Provider interface) ships first[6].

---

## 4. The Extension Architecture Decision

### Why NOT a Plugin Ecosystem (gh/kubectl Style)

**Confidence: HIGH (3 sources analyzed: gh extension model, kubectl plugin model, Grizzly architecture)[4][5]**

A plugin ecosystem where providers are separate binaries was thoroughly evaluated[5]. It fails on four axes:

**1. Auth Problem**: gcx's config loading, TLS resolution, and stack-ID auto-discovery live in `internal/` packages[5]. Go's `internal/` visibility rule means external plugin binaries cannot import these packages[5]. Each plugin must re-implement config loading (~200 LOC), or gcx must make `internal/config` a public package (a non-trivial API commitment)[5]. The kubectl model "solves" this by inheriting `KUBECONFIG` via environment, but gcx's config is more complex (stack-id discovery, org-id resolution, TLS with custom CA)[5].

**2. UX Divergence Problem**: Without shared access to `format.Codec`, `io.Options`, `fail.DetailedError`, and the Options pattern, plugins diverge from gcx's output format, error presentation, and flag naming immediately[5]. The gh extension ecosystem exhibits this: extensions have inconsistent `--format`, `--json`, and `--jq` support despite gh's own rich output system[5].

**3. Agent Context Fragmentation Problem**: When an agentic coder (Claude Code, Copilot Workspace) implements a new product provider, it needs the full context: agent-docs, CLAUDE.md, existing patterns, the Provider interface spec[5]. With a plugin model, the agent works in a separate repo with none of this context[5]. This is the difference between "read agent-docs, follow the template, submit a PR" and "understand two codebases, set up CI/CD, publish a binary."

**4. Timeline Problem**: A plugin ecosystem requires 2-4 weeks of infrastructure before the first product ships: plugin protocol spec, config env-var injection in root command, plugin development documentation, possibly making `internal/` packages public[5]. The Provider interface in monorepo lets the first product ship within days[5].

### Why NOT Approach B Immediately (Unified `resources` with Pluggable Backends)

**Confidence: HIGH (confirmed by API shapes finding that directly invalidated a key assumption)[6][7]**

Approach B -- making `gcx resources push slo/my-rule` route to an SLO REST adapter -- was evaluated and rejected for immediate implementation[6]. Three reasons:

**1. Interface Semantic Mismatch**: `PushClient` and `PullClient` use `*unstructured.Unstructured` and `metav1.*Options` on their signatures[4]. While `unstructured.Unstructured` is technically just `map[string]any`, the upsert logic in `pusher.go:259` calls `existing.GetResourceVersion()` -- a k8s optimistic concurrency primitive[1]. Non-k8s REST APIs use different concurrency controls (ETags, timestamps, or none at all)[6]. An adapter that returns empty `resourceVersion` would cause the upsert to always use Create instead of Update, breaking the idempotency contract[6].

**2. Discovery Seam Is a Hard Block**: `discovery.NewDefaultRegistry()` makes an HTTP call to `/apis` on construction[1]. There is no static-population path[6]. Adding `RegistryIndex.Populate()` is only ~20 LOC, but it means touching the discovery pipeline -- and every read command (`get`, `list`, `pull`, `edit`) flows through `fetch.go` which calls `NewDefaultRegistry()`[4]. Any regression breaks four commands simultaneously.

**3. API Shape Assumption Was Wrong**: The extension-patterns analysis initially assumed SLO and k6 APIs use the k8s envelope format (`apiVersion`/`kind`/`metadata`/`spec`)[4]. The follow-up API shapes research definitively disproved this[7]. ALL three APIs (SLO, Synthetic Monitoring, k6) use flat JSON with product-specific schemas[7]. This means the translation layer design is load-bearing -- it was premature to design the unified interface before understanding what needs translating.

### The Recommended Architecture: Provider Interface in Monorepo

**Confidence: HIGH (validated against 3 target products and Grizzly's production-proven model)[3][5]**

```go
// internal/providers/provider.go

type Provider interface {
    // Identity
    Name()      string   // "slo", "k6", "synth"
    ShortDesc() string   // "Manage Grafana Cloud SLOs"

    // Commands returns the Cobra commands this provider contributes.
    // Called once during CLI initialization. Each command receives
    // the shared config options for --config/--context resolution.
    Commands(cfg *config.Options) []*cobra.Command

    // Validate checks that the provider's required credentials are
    // present and valid (offline check -- no network calls).
    Validate(ctx context.Context, cfg config.GrafanaConfig) error

    // ConfigKeys declares extra config fields this provider needs
    // beyond the standard Grafana server+token.
    ConfigKeys() []ConfigKey
}

type ConfigKey struct {
    Name        string   // "sm_token", "k6_token", "stack_id"
    EnvVar      string   // "GRAFANA_SM_TOKEN", "GRAFANA_K6_TOKEN"
    Description string   // "Synthetic Monitoring access token"
    Sensitive   bool     // true for tokens (masked in config view)
    Required    bool     // true if provider cannot function without it
}
```

**How providers register at compile-time in `cmd/gcx/root/command.go`[5]:**

```go
import (
    "github.com/grafana/gcx/internal/providers"
    "github.com/grafana/gcx/internal/providers/slo"
    "github.com/grafana/gcx/internal/providers/synth"
    "github.com/grafana/gcx/internal/providers/k6"
)

func Command(version string) *cobra.Command {
    // ... existing setup ...
    rootCmd.AddCommand(config.Command())
    rootCmd.AddCommand(resources.Command())

    // Provider registration -- each provider contributes its command group
    configOpts := &cmdconfig.Options{}
    for _, p := range providers.All() {
        for _, cmd := range p.Commands(configOpts) {
            rootCmd.AddCommand(cmd)
        }
    }
    return rootCmd
}
```

Where `providers.All()` is[5]:

```go
// internal/providers/registry.go
func All() []Provider {
    return []Provider{
        slo.New(),
        synth.New(),
        k6.New(),
    }
}
```

**How each product owns `internal/providers/{product}/`[5]:**

```
internal/providers/
  provider.go          <- Provider interface + ConfigKey type
  registry.go          <- All() function (compile-time list)
  slo/
    provider.go        <- SLOProvider struct implementing Provider
    client.go          <- Thin wrapper around slo-openapi-client
    adapter.go         <- Flat JSON <-> resources.Resource translation
    commands.go        <- list, get, push, pull Cobra commands
  synth/
    provider.go        <- SMProvider struct implementing Provider
    client.go          <- Wraps synthetic-monitoring-api-go-client
    adapter.go         <- Flat JSON <-> resources.Resource translation
    commands.go        <- list, get, push, pull + probes subcommands
    probes.go          <- Probe name -> ID resolution helper
  k6/
    provider.go        <- K6Provider struct implementing Provider
    client.go          <- Wraps k6-cloud-openapi-client-go
    adapter.go         <- Flat JSON <-> resources.Resource translation
    commands.go        <- list, get, push, pull + run subcommands
    run.go             <- k6-specific run/schedule lifecycle commands
```

**How the translation adapter pattern works (flat JSON -> k8s envelope -> back)[7]:**

Every non-k8s API returns flat JSON[7]. The adapter synthesizes a k8s-like envelope for gcx's internal `resources.Resource` type[7]. This envelope is gcx's canonical on-disk format and display format -- it does NOT need to match what the API returns.

```
API response (flat):                    gcx Resource (synthesized):
{                                       apiVersion: slo.grafana.com/v1alpha1
  "uuid": "c7f1e3a2-...",       ->     kind: SLO
  "name": "API Availability",          metadata:
  "description": "...",                   name: c7f1e3a2-...
  "objectives": [...]                     annotations:
}                                           slo.grafana.com/display-name: API Availability
                                        spec:
                                          name: API Availability
                                          description: ...
                                          objectives: [...]
```

The reverse direction strips the envelope and passes the flat spec to the API[7]:

```
gcx push slo/my-slo.yaml
  -> Read file (has apiVersion/kind/metadata/spec envelope)
  -> fromResource() extracts spec fields
  -> HTTP PUT /v1/slo/{uuid} with flat JSON body
```

**The `Prepare(existing, desired)` pattern from Grizzly for server-generated field round-tripping[3]:**

Non-k8s APIs inject server-generated fields on responses: `id`, `tenantId`, `created`, `modified`[3]. These must be handled correctly during push[3]:

- **`Unprepare(resource)`**: Strip server-generated fields before display/diff[3]. Prevents false diffs when comparing local files against remote state. (Analogous to gcx's existing `ServerFieldsStripper` processor, but for non-k8s fields.)[3]

- **`Prepare(existing, desired)`**: Before sending an update, inject server-generated fields from the existing remote resource into the desired resource[3]. This handles APIs that reject updates missing their own server-assigned IDs[3]. Grizzly's Synthetic Monitoring handler demonstrates this: it injects `tenantId` and `id` from the existing check before calling the update endpoint[3].

This pattern is implemented per-provider in the adapter layer, not in the core Provider interface.

### The Long-Term Evolution Path (Approach C)

**Confidence: MEDIUM (82%) -- design is sound but untested; depends on patterns stabilizing across providers[6]**

After 3-5 providers are implemented and their patterns are stable, the following evolution enables `gcx resources push slo/my-rule`[6]:

**Step 1: Add `RegistryIndex.Populate([]Descriptor)` (~20 LOC)[6]**

```go
// internal/resources/discovery/registry_index.go

// Populate adds statically-defined descriptors to the registry index.
// Used by non-k8s providers that cannot use k8s API discovery.
func (ri *RegistryIndex) Populate(descriptors []resources.Descriptor) {
    for _, d := range descriptors {
        ri.byKind[strings.ToLower(d.Kind)] = d
        ri.bySingular[strings.ToLower(d.Singular)] = d
        ri.byPlural[strings.ToLower(d.Plural)] = d
        ri.descriptors = append(ri.descriptors, d)
    }
}
```

This is non-breaking[6]. Existing k8s discovery path continues to work via `Update()`.

**Step 2: Modify `fetch.go` + `push.go` injection points (~50 LOC)[6]**

```go
// fetch.go -- before
pull, err := remote.NewDefaultPuller(ctx, opts.Config)

// fetch.go -- after
pull, err := opts.newPuller(ctx, opts.Config)  // injectable constructor
```

The injectable constructor defaults to the k8s path[6]. Providers can override it with their own client implementation.

**Step 3: `gcx resources push slo/...` starts working as an alias[6]**

The selector `slo/my-rule` resolves via `RegistryIndex.LookupPartialGVK()` against the statically-populated SLO descriptor, routes to the SLO provider's PushClient adapter, and executes[6].

**Step 4: All command groups remain as convenient shortcuts[6]**

`gcx slo push` continues to work as before[6]. The `resources push slo/...` path is an additional entry point, not a replacement[6]. Users who only work with SLOs never need to learn the unified syntax.

---

## 5. Implementation Roadmap (Before Summer)

**Confidence: MEDIUM (82%) -- product ordering well-supported; LOC and timeline estimates are approximate[6]**

### Wave 0: Infrastructure (Week 1)

**What**: Provider interface, registry, config extension, agent-docs[5]

**Why first**: All subsequent providers depend on this foundation[5]

**Deliverables[5]**:
- `internal/providers/provider.go` -- Provider interface + ConfigKey type (~50 LOC)
- `internal/providers/registry.go` -- All() function + registration (~30 LOC)
- Config system: add provider-specific config keys to Context loading (~100 LOC)
- Wire provider registration into `cmd/gcx/root/command.go` (~10 LOC)
- Agent-docs: "How to add a new product provider" guide (~2 pages)
- Template: skeleton provider package for agents to copy

**Estimated LOC**: ~250 infrastructure + ~500 documentation

### Wave 1: SLO (Weeks 2-3)

**Why first[5]**: Simplest possible provider. Same auth as Grafana (zero config changes). Clean CRUD API. One resource type. No product-specific quirks beyond the Asserts provenance header. This is the reference implementation that all subsequent providers will follow.

**Deliverables[5]**:
- `internal/providers/slo/` -- provider, client, adapter, commands (~600-700 LOC)
- `cmd/gcx/slo/` integration wiring (~20 LOC)
- Unit tests for adapter layer translation (~200 LOC)
- Integration test with Grafana Cloud SLO API (manual or docker)

**Product-specific notes[7]**:
- Auth: Reuses Grafana SA token -- `ConfigKeys()` returns empty slice
- Base URL: `{grafana_server}/api/plugins/grafana-slo-app/resources/v1/slo`
- Identity: `uuid` (server-assigned string UUID)
- Pagination: None (API returns all SLOs)
- Create: `POST /v1/slo` with JSON body
- Update: `PUT /v1/slo/{uuid}` (full replace)
- Delete: `DELETE /v1/slo/{uuid}`
- Edge case: Asserts-provisioned SLOs need `Grafana-Asserts-Request: true` header (detect via label)

### Wave 2: Synthetic Monitoring + OnCall (Weeks 4-6)

**Why second[5]**: High impact products. SM is well-understood from Grizzly's implementation (production-proven patterns to adopt). OnCall can reuse the Grafana SA token, reducing config complexity.

**Synthetic Monitoring deliverables[5]**:
- `internal/providers/synth/` (~800 LOC -- larger due to probe resolution)
- `ConfigKeys()`: `sm_url` (required), `sm_token` (required)
- Env vars: `GRAFANA_SM_URL`, `GRAFANA_SM_TOKEN`

**SM product-specific notes[3][7]**:
- Auth: Separate SM access token (NOT the Grafana SA token)[7]
- API quirks from Grizzly analysis[3]:
  - No GET-by-ID endpoint; must `ListChecks()` and filter (Grizzly workaround)
  - `Prepare()` must inject `tenantId` + `id` from existing remote resource before update
  - `Unprepare()` must strip `tenantId`, `id`, `modified`, `created` before diff/display
  - Probes specified by name in YAML but API expects int64 IDs -- needs name-to-ID translation
- Resource types: Check (3 subtypes: http, ping, dns, tcp, traceroute, multihttp, grpc, scripted, browser), Probe

**OnCall deliverables[5]**:
- `internal/providers/oncall/` (~700 LOC)
- `ConfigKeys()`: `oncall_token` (optional, falls back to Grafana SA token), `oncall_url` (optional)
- Env vars: `GRAFANA_ONCALL_TOKEN`, `GRAFANA_ONCALL_URL`
- Resource types: Integration, Route, Escalation Chain, Schedule, Shift, Webhook, Notification Rule (8 types)

### Wave 3: Machine Learning + k6 Cloud (Weeks 7-9)

**Why third[5]**: ML is simple (plugin proxy, same auth). k6 is the most architecturally divergent (multipart upload, separate script storage, run lifecycle) and benefits from having the pattern established by simpler providers first.

**Machine Learning deliverables[5]**:
- `internal/providers/ml/` (~600 LOC)
- Auth: Grafana SA token via plugin proxy -- `ConfigKeys()` returns empty slice
- Base URL: `{grafana_server}/api/plugins/grafana-ml-app/resources`
- Resource types: Job, Holiday, Outlier Detector, Alert (4 types)

**k6 Cloud deliverables[5]**:
- `internal/providers/k6/` (~1000 LOC -- larger due to script handling + run lifecycle)
- `ConfigKeys()`: `k6_token` (required), `stack_id` (required)
- Env vars: `GRAFANA_K6_TOKEN`, `GRAFANA_STACK_ID`

**k6 product-specific notes[7]**:
- Auth: k6 token (Bearer) + `X-Stack-Id` header on every request
- Create: `multipart/form-data` (script as binary) -- NOT standard JSON POST
- Read: TWO API calls required (metadata GET + script GET)
- Update: SPLIT operation (PATCH metadata + PUT script)
- Pagination: `$skip`/`$top` query params + `@nextLink` cursor
- Additional commands beyond push/pull:
  - `gcx k6 run <test-id>` -- trigger test run
  - `gcx k6 runs list <test-id>` -- list test runs
  - `gcx k6 schedule set <test-id>` -- set schedule
- This is the most divergent product and validates that the Provider interface can accommodate non-CRUD workflows

### Wave 4: Cloud Management + Remaining (Weeks 10-12)

**Products[5]**: Grafana Cloud (stacks, access policies), Cloud Provider, Connections, Frontend O11y, Asserts

**Why last[5]**: Lower priority, smaller user base. Some (Cloud, Cloud Provider) are control-plane operations that may be less CLI-driven.

**Estimated LOC per product[5]**: ~500-700

### Wave 5 (Post-Summer): Approach C Convergence

**What[6]**: `RegistryIndex.Populate()` + `fetch.go`/`push.go` injection points
**LOC[6]**: ~100 in core files
**Prerequisite[6]**: 3-5 stable providers with patterns validated

### Total Estimated LOC

```
Wave 0 (infrastructure):    ~250 code + ~500 docs
Wave 1 (SLO):               ~800 (including tests)
Wave 2 (SM + OnCall):       ~1,500
Wave 3 (ML + k6):           ~1,600
Wave 4 (remaining 5):       ~3,000
Wave 5 (Approach C):        ~100

Total new code:              ~7,250
Total existing code changed: ~20 lines (root/command.go wiring)
```

---

## 6. Config System Changes

**Confidence: HIGH (88%) -- auth requirements verified from Terraform provider source code[2]**

### Current State

```yaml
# ~/.config/gcx/config.yaml
contexts:
  production:
    grafana:
      server: https://my-org.grafana.net
      token: glsa_xxx
      stack-id: 12345
```

```go
type Context struct {
    Name    string
    Grafana *GrafanaConfig
}

type GrafanaConfig struct {
    Server   string
    User     string
    Password string
    APIToken string
    OrgID    int64
    StackID  int64
    TLS      *TLS
}
```

### Proposed Extension

The config file grows a `providers` map that holds per-provider credential blocks[5]:

```yaml
contexts:
  production:
    grafana:
      server: https://my-org.grafana.net
      token: glsa_xxx
      stack-id: 12345
    providers:
      synth:
        sm_url: https://synthetic-monitoring-api.grafana.net
        sm_token: eyJr...
      k6:
        k6_token: k6_xxx
        stack_id: 12345
```

```go
type Context struct {
    Name      string
    Grafana   *GrafanaConfig
    Providers map[string]map[string]string  // provider name -> key/value pairs
}
```

The `Providers` field is a generic `map[string]map[string]string` rather than typed structs[5]. This avoids the Grizzly anti-pattern where adding a provider requires modifying the Context struct[5]. The Provider interface's `ConfigKeys()` method provides schema validation at runtime.

### How Environment Variables Work

Each provider declares its env vars via `ConfigKeys()`[5]:

```go
// internal/providers/synth/provider.go
func (p *SMProvider) ConfigKeys() []ConfigKey {
    return []ConfigKey{
        {Name: "sm_url",   EnvVar: "GRAFANA_SM_URL",   Description: "SM API URL", Required: true},
        {Name: "sm_token", EnvVar: "GRAFANA_SM_TOKEN",  Description: "SM access token", Sensitive: true, Required: true},
    }
}
```

Resolution order (highest priority wins)[5]:
1. Environment variable (`GRAFANA_SM_TOKEN`)
2. Context provider config (`contexts.production.providers.synth.sm_token`)
3. Not set -- provider's `Validate()` returns error if `Required: true`

### How `gcx config set` Works

```bash
# Set a provider-specific key
gcx config set providers.synth.sm_token=eyJr...

# This writes to the current context's providers map:
# contexts.{current}.providers.synth.sm_token = "eyJr..."
```

The existing `config set` already uses dot-path notation for nested fields[5]. The `providers.{name}.{key}` path integrates naturally[5]. `config view` respects `Sensitive: true` from `ConfigKeys()` and redacts token values (using the existing `secrets.Redactor` pattern)[5].

### Provider Config Validation

```bash
$ gcx config check
Context: production
  grafana: OK (server reachable, token valid)
  slo: OK (shares Grafana credentials)
  synth: ERROR: sm_token not configured
    Set it with: gcx config set providers.synth.sm_token=<token>
    Or export:   GRAFANA_SM_TOKEN=<token>
  k6: OK (k6_token and stack_id configured)
```

This extends the existing `config check` command by iterating over `providers.All()` and calling `Validate()` on each[5].

---

## 7. Agent-Docs Updates Required

**Confidence: MEDIUM-HIGH (85%) -- gap analysis is comprehensive but template structure is untested[5]**

### Current Coverage Gaps

| Task | Current Coverage | Agent Success Rate |
|---|---|---|
| Add `resources` subcommand | 95% | HIGH -- cli-layer.md is excellent |
| Add new k8s resource type | 90% | HIGH -- resource-model.md covers discovery |
| Add new command group (slo, k6) | 30% | LOW -- no walkthrough exists |
| Add product with REST API | 10% | LOW -- no pattern or bridge documented |

### New Documentation Needed

**1. New file: `agent-docs/provider-guide.md` -- "How to Add a New Product Provider"[5]**

Contents:
- The Provider interface contract (with Go doc)
- Step-by-step walkthrough using SLO as the reference implementation
- Template directory structure
- Translation adapter pattern (flat JSON <-> k8s envelope)
- The `Prepare/Unprepare` pattern for server-generated fields
- How to declare extra config keys
- How to write provider-specific tests
- Checklist: what to verify before submitting

**2. New section in `cli-layer.md`: "Provider Command Groups"[5]**

Explain how provider commands differ from `resources` commands:
- Provider commands call `LoadConfig()` (not `LoadRESTConfig()`)
- Provider commands build their own HTTP clients
- Provider commands can have product-specific verbs (e.g., `k6 run`)
- Provider commands share `--config`, `--context`, `--output`, `--on-error` flags

**3. Template structure for a provider package[5]**

```
internal/providers/{product}/
  provider.go        <- Copy from SLO reference, change Name/ShortDesc/ConfigKeys
  client.go          <- Product-specific HTTP client or SDK wrapper
  adapter.go         <- toResource() + fromResource() translation
  commands.go        <- list, get, push, pull (standard CRUD)
  {extra}.go         <- Product-specific commands if needed
  provider_test.go   <- Validate() and ConfigKeys() tests
  adapter_test.go    <- Round-trip translation tests
```

**4. Decision tree: when to use `resources` command vs. a new provider[5]**

```
Does the product API use the Grafana k8s /apis endpoint?
  YES -> It is already handled by `gcx resources`. No provider needed.
  NO  -> Does the product API use a REST/gRPC endpoint?
    YES -> Implement a Provider.
    NO  -> Discuss with the team.
```

**5. The `/add-product` skill spec[5]**

What an agentic coder needs to implement a new provider:

```
Inputs:
  - Product name (e.g., "slo")
  - API documentation URL or OpenAPI spec
  - Auth mechanism (Grafana token, separate token, basic auth)
  - Go client library (if one exists)

Steps:
  1. Read agent-docs/provider-guide.md
  2. Copy template from internal/providers/slo/ (reference implementation)
  3. Replace client wrapper with product-specific client
  4. Implement toResource() / fromResource() adapter
  5. Implement Cobra commands (list, get, push, pull + product-specific)
  6. Add ConfigKeys() for any extra credentials
  7. Register provider in internal/providers/registry.go
  8. Add import + AddCommand in cmd/gcx/root/command.go
  9. Run make lint && make tests
  10. Submit PR

Outputs:
  - internal/providers/{product}/ directory
  - 2 lines changed in root/command.go
  - 1 line added in providers/registry.go
```

---

## 8. Risk Register

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| **API breaking changes during implementation** -- SLO, SM, k6 APIs may change endpoint shapes or auth before summer | Medium | High | Pin Go client library versions. Add API version checks in provider `Validate()`. Monitor Grafana Cloud changelog. |
| **Translation adapter drift** -- flat JSON schema evolves, adapter breaks silently | Medium | High | Round-trip property tests for each adapter (`toResource(fromResource(r)) == r`). CI integration tests against staging APIs if available. |
| **k6 multipart upload complexity** -- script binary upload, two-call reads, and run lifecycle are significantly more complex than the generic push/pull model | High | Medium | Start k6 in Wave 3 (not Wave 1). Let the pattern stabilize with simpler providers first. Accept that k6 provider will be larger (~1000 LOC) and may need product-specific commands that diverge from the push/pull model. |
| **SM token lifecycle** -- SM access tokens can expire; provider may fail silently after token rotation | Medium | Medium | Document token refresh procedure. Consider adding `gcx synth auth refresh` command. Check token validity in `Validate()` with a lightweight API call. |
| **Config complexity growth** -- 5+ providers each adding 1-3 config keys creates a confusing config file | Medium | Medium | `gcx config check` command validates all providers. Clear error messages with "set it with" instructions. `config view` groups by provider with redacted sensitive values. |
| **Summer timeline pressure** -- 13 product areas is ambitious for one season | High | High | Prioritize by impact. Waves 1-3 (SLO, SM, OnCall, ML, k6) cover the highest-value products. Remaining products can ship post-summer. The Provider interface de-risks parallelism: multiple contributors can work on different providers simultaneously. |
| **Approach C convergence never happens** -- provider command groups work well enough that nobody invests in the unified `resources push slo/...` path | Medium | Low | This is acceptable. The provider model is already a good UX. Approach C is a nice-to-have for power users who want unified workflows. If it never ships, the architecture is still sound. |

---

## 9. Open Questions

**1. Should the translation adapter produce the k8s envelope or should gcx define a lighter internal representation?[5]**

The current recommendation wraps flat JSON in a synthesized `apiVersion`/`kind`/`metadata`/`spec` envelope[5]. This reuses `resources.Resource` and the existing output codecs[5]. But it adds conceptual overhead: SLO files on disk will have a k8s-shaped envelope that the SLO API does not understand[5]. An alternative is a lighter `ProviderResource` type with just `{type, name, spec}` -- but this means two resource formats in the codebase.

**Status**: Needs a design prototype[5]. Recommend starting with the k8s envelope (it works with existing tooling) and evaluating whether the conceptual mismatch causes user confusion.

**2. Should providers share a common CRUD interface or is the Provider returning `[]*cobra.Command` sufficient?[5]**

The current design lets each provider define its own command tree via `Commands()`[5]. This maximizes flexibility (k6 can have `run` commands that SLO does not)[5]. But it means no shared `gcx push` across all providers[5]. If we later want `gcx push ./dir` to automatically dispatch to all configured providers, we would need a shared `PushProvider` sub-interface.

**Status**: Deferred[5]. Ship with per-provider commands first[5]. If user demand emerges for unified push, design the sub-interface based on real usage patterns[5].

**3. How should `gcx resources pull` interact with provider resources in a mixed environment?[5]**

Today, `gcx resources pull` pulls all app-platform resources[5]. When Approach C lands, should `gcx resources pull` also pull SLO/SM/k6 resources by default? Or should it require explicit `gcx resources pull --all-providers`?

**Status**: Needs user research[5]. The safe default is to NOT pull provider resources from the `resources` command unless explicitly requested, to avoid surprising users with new resource types appearing in their pull output[5].

**4. What is the right error contract when a provider's credentials are not configured?[5]**

If a user runs `gcx slo list` without configuring SLO credentials, should the error come from `Validate()` (before any command runs) or from the HTTP client (401 at runtime)? The Grizzly model uses two-level checks (`Active` vs `Online`)[3]. gcx should decide whether `Validate()` is mandatory before command execution or advisory.

**Status**: Recommend mandatory `Validate()` in each command's `RunE` before making API calls[5]. Clearer error messages, faster failure[5]. The `config check` command provides the advisory view[5].

**5. Should on-disk resource files for non-k8s providers use the same directory structure convention as app-platform resources?[5]**

App-platform resources use `{kind}/{name}.{json|yaml}` directory layout (controlled by `FSWriter`)[5]. Should SLO resources be written to `slo/c7f1e3a2.yaml`? Or should providers control their own file layout (e.g., `slo/api-availability.yaml` using display name instead of UUID)?

**Status**: Needs a decision[5]. Recommend using `{provider}/{display-name}.yaml` as the default, with the UUID/ID stored inside the file rather than as the filename[5]. This is more human-friendly for non-k8s resources where the identity is often a UUID rather than a meaningful name.

---

## Synthesis Notes

- **Findings analyzed**: 7 (4 codebase analyses + 3 follow-up research)
- **External codebases analyzed**: grafana/terraform-provider-grafana[2], grafana/grizzly[3], cli/cli (gh)[4], grafana/slo-openapi-client, grafana/k6-cloud-openapi-client-go, grafana/synthetic-monitoring-api-go-client
- **Contradictions found and resolved**: 5 (see synthesis/contradictions.md)
- **Confidence rationale**: High overall confidence (88%) driven by direct codebase analysis with line-level precision[1]. Lower confidence on implementation timeline (inherently uncertain) and Approach C convergence (untested design)[6]. Highest confidence on the API surface map (94%)[2] and current architecture analysis (96%)[1].
- **Key load-bearing finding**: The API shapes research discovering that NONE of SLO/k6/Synthetics use the k8s envelope format[7]. This single finding eliminated Approach B as an immediate option and confirmed the translation adapter as a core architectural requirement.
- **Limitations**: No prototype has been built. LOC estimates are based on similar implementations in Grizzly[3] and the Terraform provider[2]. Timeline estimates assume dedicated engineering capacity. The ConnectRPC product (Fleet Management) was not deeply analyzed for provider interface fit.

---

## References

[1] gcx Codebase Analysis: K8s Coupling Interface Boundaries
    `internal/resources/remote/pusher.go:25`, `internal/resources/remote/puller.go:19`,
    `internal/resources/discovery/registry.go:30`, `internal/resources/resources.go:28`,
    `internal/config/rest.go:12-19`
    Conducted 2026-03-02

[2] Terraform Provider for Grafana — Architecture Analysis
    `github.com/grafana/terraform-provider-grafana` (main branch)
    Conducted 2026-03-02

[3] Grizzly Architecture Analysis: Provider/Handler Abstraction
    `github.com/grafana/grizzly` (main branch)
    `pkg/grizzly/registry.go`, `pkg/grizzly/handler.go`, `pkg/syntheticmonitoring/`
    Conducted 2026-03-02

[4] gcx CLI Extension Patterns Analysis
    `cmd/gcx/root/command.go`, `cmd/gcx/resources/command.go`,
    `internal/config/command.go:21-31`, `cmd/gcx/fail/detailed.go`
    Conducted 2026-03-02

[5] Plugin/Extension Ecosystem Architecture Decision Analysis for gcx
    Monorepo contribution vs. plugin ecosystem evaluation
    gh extension model, kubectl plugin model analysis
    Conducted 2026-03-02

[6] Approach Comparison: Parallel Command Groups vs Unified Pluggable Backends
    Approach A, B, and C feasibility analysis
    Conducted 2026-03-02

[7] Follow-up: API Shapes for SLO, k6, and Synthetic Monitoring
    `grafana/terraform-provider-grafana` (provider implementation),
    `grafana/slo-openapi-client`, `grafana/k6-cloud-openapi-client-go`,
    `grafana/synthetic-monitoring-api-go-client`
    Conducted 2026-03-02

---

*Research conducted using Claude Code's multi-agent research system.*
*Session ID: research-2d6d0aee-20260302-120912 | Generated: 2026-03-02*
