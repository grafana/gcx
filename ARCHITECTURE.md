# Architecture: gcx

## Vision

See [VISION.md](VISION.md) for goals, roadmap, and product surface.

**In brief:** A CLI for managing Grafana and Grafana Cloud. Supports dynamic Grafana API resources via a kubectl-like resources layer, and per-product features via the provider interface. Includes observability-as-code workflows (`gcx dev`), multi-stack configuration/contexts, and Grafana Assistant integration. Optimized for AI agents and human use.

## System Overview

### 1. Resources Pipeline

The core of gcx. Manages Grafana-native resources (dashboards, folders, alert rules, etc.) with Grafana's Kubernetes-compatible `/apis` endpoint (available in Grafana 12 or later).

```
User input                           gcx resources push ./dashboards/
    |
    v
Selector (partial)                   "dashboards/" or "dashboards/my-dash"
    |
    v
Discovery Registry                   API call to /apis → available GVKs
    |
    v
Filter (resolved)                    Full GVK: dashboard.grafana.app/v1alpha1
    |
    v
Processors                           Strip server fields (pull) / add namespace (push)
    |
    v
Dynamic Client (k8s.io/client-go)   Create-or-update via /apis endpoint
    |
    v
Grafana K8s API                      /apis/{group}/{version}/namespaces/{ns}/{plural}/{name}
```

**Operations:** `get`, `push` (create-or-update, idempotent), `pull` (export to local YAML/JSON), `delete`, `edit` (single resource, `$EDITOR`), `validate` (local linting via Rego), `list-types` (discover types), `list-examples` (show sample manifests).

**Key abstractions** ([resource-model.md](docs/architecture/resource-model.md)): `Resource` wraps `unstructured.Unstructured` — no pre-generated Go types. `Selector` → `Filter` two-stage resolution keeps CLI ignorant of API details. `Processor` pipeline composes transformations at defined pipeline points. `Discovery` registry resolves plural names and short names to full GVKs at runtime.

Selectors accept both `resource.version.group` and `resource.group`, including
dotted API groups. Discovery tries the versioned reading first, then the full
group name, for both native resources and provider adapters.

**Data flows** ([data-flows.md](docs/architecture/data-flows.md)): Push reads local files, resolves selectors, applies processors, pushes via dynamic client with folder-before-dashboard ordering and bounded concurrency (errgroup, default 10). Pull fetches from API, strips server-managed fields, writes to disk grouped by kind.

### 2. Provider System

Pluggable adapters for Grafana Cloud products. Each provider is a self-contained package under `internal/providers/` that contributes CLI commands and optionally bridges into the resources pipeline.

```
Provider (internal/providers/slo/)
    |
    +-- Commands()            Cobra commands: gcx slo definitions list
    |
    +-- TypedRegistrations()  Adapter registrations for resources pipeline
    |       |
    |       v
    |   adapter.Register()    Makes provider resources accessible via gcx resources get/push/pull
    |
    +-- ConfigKeys()          Declares provider-specific config keys (token, url, ...)
    |
    +-- Validate()            Validates config before API calls
```

**TypedCRUD\[T\]** bridges typed Go domain structs to K8s-style `unstructured.Unstructured` envelopes. Domain types implement `ResourceIdentity` (`GetResourceName`/`SetResourceName`). `TypedObject[T]` wraps them with `ObjectMeta` + `TypeMeta` for K8s compliance.

**ConfigLoader** (`providers.ConfigLoader`) handles `--config`/`--context` flag binding, YAML + env var precedence, and provider-specific config resolution (`GRAFANA_PROVIDER_{NAME}_{KEY}`). All providers must use it — no ad-hoc `os.Getenv`.

**Dual access paths** are permanent: provider commands (`gcx slo definitions list`) give ergonomic domain-specific tables; generic commands (`gcx resources get slos.v1alpha1.slo.ext.grafana.app`) serve the push/pull pipeline. JSON/YAML output is identical across both paths by construction (both use the same `ResourceAdapter`).

**Declarative registration front door**: `adapter.Resource[T]` + `adapter.NewProvider` (command factories attach via `WithCommands`) let a provider declare a resource type once, by implementing plain capability interfaces (`Lister[T]`, `Getter[T]`, `Creator[T]`, `Updater[T]`, `Deleter[T]`, `Validator[T]`) on its client, instead of hand-building a `Registration`. Capability detection is confined to a single audited `any`-assertion seam in `internal/resources/adapter/capability.go` — see RFC 026 and patterns.md's "Sanctioned Exception — Single-Seam Capability Assertion". Provider commands and registration share `Resource.TypedCRUD`; Grafana-backed command groups use `providers.BindGrafanaResource` for lazy binding; each leaf calls `Load` to resolve a fresh config snapshot and construct the declared client.

**Deep-dive:** [resource and provider patterns](docs/architecture/patterns.md). Implementation guide: [provider-guide.md](docs/reference/provider-guide.md).

### 3. Signal Providers

Top-level commands for querying observability datasources: `metrics`, `logs`, `traces`, `profiles`. These bypass the K8s dynamic client and call datasource HTTP APIs directly.

```
gcx metrics query -d prom-001 'rate(http_requests_total[5m])' --since 1h
    |
    v
SharedOpts                   Shared flags: -d/--datasource, --from, --to, --since, --step
    |
    v
Datasource Resolution        Resolves -d flag to datasource UID (by name, UID, or config default)
    |
    v
Query Client                 internal/query/{kind}/ (direct HTTP; unified-query clients reuse internal/query/grafanaquery + internal/query/dataframe)
    |
    v
Codec Pipeline               table (default) | graph (terminal chart) | json | yaml
```

**Standardized verbs**: `query` (execute queries), `labels` (list label names/values), `series`/`metrics` (list series or compute metric queries), `metadata` (metric metadata). All four signal providers share these verbs with identical flag semantics.

**Dual command mounting**: Datasource commands are accessible via two paths — top-level signal commands (`gcx metrics query`) and the `datasources` subgroup (`gcx datasources prometheus query`). Both paths call the same exported command constructors in `internal/datasources/{kind}/`. Signal command shape (shared config flags, root hook chaining, command metadata, adaptive subtree mounting) and signal-backed datasource provider wiring are centralized in `internal/signals/`. Top-level signal providers use `signals.Command(...)`; built-in datasource providers use `signals.DatasourceProvider(...)` and self-register via `init()` in `internal/datasources/providers/` (blank-imported from `cmd/gcx/root/command.go`).

**Adaptive telemetry** nests under each signal provider (`metrics adaptive`, `logs adaptive`, `traces adaptive`) with its own CRUD resources (rules, policies, exemptions, segments) and operational views (recommendations, patterns). Uses `internal/auth/adaptive/` for shared, GCOM-resolved Basic auth.

**Graph rendering:** `internal/graph/` converts query responses to terminal charts via ntcharts + lipgloss. Available as `-o graph` on all query commands and SLO/synth timeline commands.

### 4. Developer Tooling (`gcx dev`)

Observability-as-code workflows for managing Grafana resources as typed Go code via [grafana-foundation-sdk](https://github.com/grafana/grafana-foundation-sdk). The `gcx dev` commands produce and validate resources that feed into the standard `gcx resources` pipeline.

**End-to-end workflow:** `scaffold` → `import`/`add` → edit Go code → `serve`/`lint` → build to manifests → `resources push`

- **`scaffold`** — Generate a new project (Go module + foundation-sdk + folder structure)
- **`import`** — Import existing dashboards/alerts from Grafana as Go builder code
- **`serve`** — Live-reload dev server (Chi router, reverse proxy, WebSocket reload) — edit code, preview in browser
- **`lint`** — Lint resources with built-in and custom Rego rules (OPA engine in `internal/linter/`), including PromQL/LogQL expression validators
- **`generate`** — Code generation utilities

The linter engine is also used by `gcx resources validate` for pre-push validation. See [VISION.md § Observability as Code](VISION.md#observability-as-code) for the full workflow vision.

### 5. Setup (`gcx setup`)

Cross-product onboarding helpers. Not a provider — standalone command area.

- **`setup status`** — Aggregated connection, auth, and product-availability snapshot

The instrumentation onboarding wizard lives under the Instrumentation Hub provider (`gcx instrumentation setup`), not under `gcx setup`. See RFC 019 for the rationale.

### 6. Instrumentation Hub (`gcx instrumentation`)

Action-verb command tree for Grafana Cloud's Instrumentation Hub. Backed by fleet-management `Set/Get` + observed-state RPCs; registers no GVK and is not addressable through `gcx resources push/pull`.

- **`instrumentation setup <cluster>`** — End-to-end onboarding wizard; calls `SetupK8sDiscovery`, applies declared K8s monitoring config, prints a parameterized helm command
- **`instrumentation status`** — Cross-cutting observed view across cluster → namespace → service hierarchy
- **`instrumentation check [components]`** — Local OTel-instrumentation validation via the [otel-checker](https://github.com/grafana/otel-checker) library; runs entirely against the workstation's env vars, package manifests, and collector/Beyla/Alloy config. No Grafana stack calls unless `--fix-plan=assistant` is set. Two disjoint fix-plan modes: `--fix-plan=local` deterministically aggregates each finding's explanation doc (offline, no billing, OSS/Enterprise-friendly); `--fix-plan=assistant` hands the findings and docs to Grafana Assistant to synthesize one prioritized plan (billable, requires a Grafana Cloud context — errors out otherwise, with no silent fallback)
- **`instrumentation explain <id>`** and **`instrumentation list-explanations`** — Lookup surface for the explanation docs bundled by otel-checker; IDs come from the `explain_id` field emitted by `check`
- **`instrumentation clusters [list|get|configure|remove|wait]`** + nested **`apps`** — Declared-state read/write with tri-state flag semantics on `configure` and a per-namespace optimistic-lock guard
- **`instrumentation services [list|get|include|exclude|clear]`** — Observed-state fleet sweep via `RunK8sDiscovery` with DWIM single-workload mutation

Uses `internal/providers/instrumentation/` (provider, types, output codecs, RMW helper, helm formatter, enumeration helper) and `internal/fleet/` (shared base HTTP client, also used by the fleet provider; it reaches Fleet Management through the `grafana-collector-app` plugin proxy on the stack — see RFC 024). `check` and `explain` are thin cmd/-only wrappers around the upstream `github.com/grafana/otel-checker/checks` and `.../checks/explain` packages — no provider glue. `check --fix-plan=assistant` embeds Grafana Assistant via `internal/providers/assistant.ResolveClientOptions` + `RequireGrafanaCloud` (reused from the same auth resolution used by `gcx assistant prompt`), then calls `ChatWithApproval` directly; the two-mode dispatch (local vs. assistant, no cross-mode fallback) lives in `cmd/gcx/instrumentation/check/fixplan/`. See RFC 019 for the design.

### 7. Configuration

kubectl-inspired, versioned multi-environment configuration. Named stacks bind
Grafana destinations to Grafana auth and provider settings; named Cloud entries
bind GCOM endpoints to Cloud auth; thin contexts select one of each.

```yaml
version: 1
current-context: prod
stacks:
  prod:
    slug: my-stack
    grafana: { server: https://my-stack.grafana.net, token: gf_... }
    providers:
      slo: { token: glsa_... }
cloud:
  grafana-com:
    token: glc_...
    api-url: https://grafana.com
contexts:
  prod: { stack: prod, cloud: grafana-com }
```

**Loading chain:** Discover system → user → local source snapshots (or one
explicit `--config` file), reject unsupported declared versions (preflight every
layered source), resolve the trusted keychain policy from those bytes before
legacy migration, full decode, or opening a credential store, then merge
credential-bearing stack/Cloud entries atomically, select `--context` or
`current-context`, then apply environment overrides (`GRAFANA_SERVER`,
`GRAFANA_TOKEN`, `GRAFANA_PROVIDER_{NAME}_{KEY}`). Overrides take runtime
precedence but remain ephemeral.

**Namespace resolution:** `org-id` (on-prem, maps to K8s namespace) or `stack-id` (Cloud, discovered via GCOM). Providers use `ConfigLoader` which resolves these uniformly.

**Secret handling:** Config keys marked `Secret: true` in provider `ConfigKeys()`
are redacted in `gcx config view`. Undeclared keys and unknown providers are
redacted by default. Keychain references are bound to their canonical source
file, exact owner/field, and destination so another layer cannot redirect or
overwrite a stored credential. `credentials.keychain` defaults to `on`; an
explicit trusted `off` (or `GCX_KEYCHAIN=off`) is the only deliberate plaintext
mode, and an auto-discovered local file cannot set that policy. An unavailable
native keychain fails closed, same as a reachable but locked or
interaction-disabled keychain (`Keychain locked`).

**Deep-dive:** [config-system.md](docs/architecture/config-system.md).

### 8. Authentication

Multiple auth mechanisms for different tiers.

| Mechanism | Used for | Implementation |
|-----------|---------|----------------|
| **Service account token** | Grafana K8s API (`/apis`), plugin APIs | Bearer token in `rest.Config` |
| **Cloud Access Policy token** | GCOM stack discovery, Cloud product APIs | `internal/cloud/` GCOM client |
| **OAuth PKCE** | Browser-based login (`gcx login`) | `internal/auth/` — token refresh transport persists to config |
| **Basic auth** | Self-hosted Grafana, including server administration | `gcx login --basic-auth`; username/password in `rest.Config`, verified via `/api/user` before saving |
| **Adaptive auth** | Signal provider adaptive telemetry APIs | `internal/auth/adaptive/` — GCOM-resolved Basic auth shared across signal providers; stale provider cache fields are not credential destinations |

**Runtime selection:** an explicit `grafana.auth-method` (`oauth`, `token`,
`basic`, or `mtls`) is authoritative, and stale fields for other methods are
not attached to the request. Legacy entries without `auth-method` infer OAuth
proxy > service-account token > user/password > mTLS/anonymous; partial or
rejected higher-priority credentials fail before network use instead of falling
through. Explicit flags override env vars override config file while credentials
are being resolved. `httputils.NewDefaultClient(ctx)` must be used for APIs outside
the Grafana server unless the provider uses the trust-checked
`CloudRESTConfig.HTTPClient`/direct-provider snapshot path; a raw k8s transport
can otherwise inject Grafana auth into the wrong request.

**Deep-dive:** [client-api-layer.md](docs/architecture/client-api-layer.md), [config-system.md](docs/architecture/config-system.md).

### Portable Agent Skills (`gcx agent skills`)

`claude-plugin/skills-catalog.yaml` records active, deprecated, and retired skills
independently of bundled content. `internal/skills` reconciles that release catalog
with the selected local installation for list/install/update/uninstall; retired
entries remain addressable without automatic deletion or replacement installation.
See [CLI layer: Portable Skill Lifecycle](docs/architecture/cli-layer.md#portable-skill-lifecycle).

## Design rationale

Engineering proposals and historical decisions live in [RFCs](docs/rfcs/). Each record carries its own status; a proposal is not evidence of implementation.

## Architecture Docs

Use the [architecture guide](docs/architecture/README.md) to find the relevant subsystem. [Design rules](docs/design/) cover CLI behavior; the [provider guide](docs/reference/provider-guide.md) explains extension points.

## Taste Rules

Enforced — see [CONSTITUTION.md § Taste Rules](CONSTITUTION.md#taste-rules) for the authoritative list.

- **Options pattern** for every command: `opts` struct → `setup(flags)` → `Validate()` → constructor
- **Error messages**: lowercase, no trailing punctuation
- **Table-driven tests**: all Go tests follow [Go wiki conventions](https://go.dev/wiki/TableDrivenTests)
- **errgroup concurrency**: bounded parallelism (default 10) for all batch I/O operations
- **Commit format**: Title (one-liner) / What (description) / Why (rationale)

## Related

- [VISION.md](VISION.md) — goals, product surface, roadmap themes
- [CONSTITUTION.md](CONSTITUTION.md) — architecture invariants and dependency rules
- [DESIGN.md](DESIGN.md) — CLI UX design, command grammar, output model
- [docs/reference/provider-guide.md](docs/reference/provider-guide.md) — how to add a new provider
