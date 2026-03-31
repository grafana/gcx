---
type: feature-plan
title: "Setup Instrumentation Stage 2: Command grammar, check, init, and add"
status: draft
spec: docs/specs/feature-setup-instrumentation/spec.md
adr: docs/adrs/instrumentation/001-instrumentation-provider-design.md
beads_id: gcx-4r1g
created: 2026-03-31
---

# Setup Instrumentation Stage 2

Refines the command grammar based on Stage 1 smoke testing, adds preflight
`check` at both levels, cluster bootstrapping via `init`, and the imperative
`add` verb.

## Revised Command Tree

```
gcx setup
├── init                                                # stack bootstrap (gcli init migration, bead gcx-b22fd37d)
├── check                                               # shared preflight: config → GCOM → fleet URL → backend URLs
├── status                                              # aggregated product status
└── instrumentation
    ├── init      <cluster>                             # cluster bootstrap: Helm chart + fleet connect
    ├── check     <cluster>                             # product preflight: prom headers, cluster exists, API responds
    ├── status    [<cluster>]                           # per-cluster instrumentation state
    ├── discover  <cluster>                             # find instrumentable workloads
    ├── add       <cluster> --namespace=X --features=Y  # imperative edit (fetch-modify-apply)
    ├── get       <cluster> [-o yaml|json]              # export current config as manifest
    └── apply     -f <file> [--dry-run]                 # declarative import from manifest
```

### Setup Area Pattern

Products with a **discover → configure → verify** workflow live under `setup`.
Each product can contribute its own `init` and `check`. Pure CRUD products
(dashboards, SLOs) stay as providers.

```
gcx setup init              # stack-level: credentials, context
gcx setup check             # stack-level: validate shared config chain
gcx setup status            # aggregated: all products
gcx setup instrumentation   # product: discover → configure → verify
gcx setup alloy             # future: prepare → verify
gcx setup integrations      # future: install → configure
gcx setup kg                # future: enable → configure → verify
```

### Workflow Model

```
setup init ──► instrumentation init ──► discover ──► add ──► get ──► apply
    │               │                     │           │       │        │
    │               │                     │           │       │        └─ declarative
    │               │                     │           │       └────────── export
    │               │                     │           └────────────────── imperative
    │               │                     └────────────────────────────── read
    │               └──────────────────────────────────────────────────── cluster bootstrap
    └──────────────────────────────────────────────────────────────────── stack bootstrap

setup check ──► instrumentation check   (diagnostic, run anytime)
```

Interactive workflow:  `setup init` → `instrumentation init` → `discover` → `add` → `get`
GitOps workflow:       `get` (from staging) → git commit → `apply` (to production)
Debugging workflow:    `setup check` → `instrumentation check <cluster>`

---

## Part 0: Update ADR

Update ADR 014 (`docs/adrs/instrumentation/001-instrumentation-provider-design.md`)
to reflect the revised command grammar and Stage 2 scope.

### Changes

- **Command grammar**: `show` → `get` (kubectl convention for K8s-style manifests),
  `discover --cluster` → `discover <cluster>` (positional arg, consistent with
  `get`, `add`, `init`)
- **Stage 2 scope**: refined to four parts: grammar fix, check, init, add
- **Command tree**: full tree with `setup`-level commands (`init`, `check`,
  `status`) and all instrumentation subcommands
- **Setup area pattern**: document that `init` and `check` exist at both the
  `setup` level (shared) and per-product level (product-specific)
- **Workflow model**: `add` is the interactive tool, `get` + `apply` is the
  export/import loop for GitOps, `check` is the diagnostic tool
- **`--features` grammar**: comma-separated signal list replacing individual
  boolean flags. Valid values: `tracing`, `logging`, `profiling`,
  `processmetrics`, `extendedmetrics` (app-level); `costmetrics`,
  `energymetrics`, `clusterevents`, `nodelogs` (k8s-level)
- **`--apps` grammar**: comma-separated workload names for per-workload pinning
  (no per-app signal toggles per FR-027)

### Deliverables

- Updated ADR with revised Decision section and command tree
- Updated spec.md FRs that reference `show` → `get`

---

## Part 1: Rename `show` → `get` + `discover` positional arg

Mechanical refactor — no new features, no API changes.

### Changes

1. `cmd/gcx/setup/instrumentation/show.go` → `get.go`
   - `showOpts` → `getOpts`
   - `runShow` → `runGet`
   - `newShowCommand` → `newGetCommand`
   - Cobra `Use: "show <cluster>"` → `Use: "get <cluster>"`
2. `cmd/gcx/setup/instrumentation/show_test.go` → `get_test.go`
   - Update all type/function references
3. `cmd/gcx/setup/instrumentation/export_test.go`
   - `ShowOpts` → `GetOpts`, `RunShow` → `RunGet`, `NewShowCommand` → `NewGetCommand`
4. `cmd/gcx/setup/instrumentation/command.go`
   - `newShowCommand` → `newGetCommand`
5. `cmd/gcx/setup/instrumentation/discover.go`
   - Remove `--cluster` flag from `discoverOpts`
   - Add `Args: cobra.ExactArgs(1)`, read cluster from `args[0]`
   - Remove `Validate()` cluster check (cobra handles it)
6. `cmd/gcx/setup/instrumentation/discover_test.go`
   - Pass cluster as positional arg instead of `--cluster` flag
7. Regenerate CLI reference docs (`make docs`)

### Acceptance Criteria

- `gcx setup instrumentation get <cluster>` produces same output as old `show`
- `gcx setup instrumentation show` returns "unknown command" error
- `gcx setup instrumentation discover <cluster>` works without `--cluster` flag
- `gcx setup instrumentation discover` (no arg) fails with cobra args error
- `GCX_AGENT_MODE=false make all` passes

---

## Part 2: `check` command — preflight validation

Two layers: shared `gcx setup check` validates the config chain that all
setup products depend on; `gcx setup instrumentation check <cluster>` validates
product-specific requirements for a given cluster.

### `gcx setup check`

Validates the shared infrastructure chain. Each step reports PASS/FAIL with
actionable error on failure.

```
$ gcx setup check
STEP                        STATUS  DETAILS
cloud.token configured      PASS    glc_***...***abc
cloud.stack resolved        PASS    collector (from grafana.server hostname)
GCOM API reachable          PASS    grafana-dev.com
Stack info retrieved        PASS    id=1119524 region=dev-us-central-0
Fleet Management URL        PASS    https://fleet-management-dev-...
Fleet Management Instance   PASS    id=5001
Backend URLs resolved       PASS    mimir ✓ loki ✓ tempo ✓ pyroscope ✓
Prom headers available      PASS    cluster_id=42 instance_id=5678
```

Failure example:
```
$ gcx setup check
STEP                        STATUS  DETAILS
cloud.token configured      PASS    glc_***...***abc
cloud.stack resolved        FAIL    cloud.stack not set; grafana.server not configured
                                    → Set cloud.stack: gcx config set cloud.stack <SLUG>
```

Stops at first FAIL with actionable suggestion.

#### Command

```
gcx setup check [--context=<name>] [-o json|table]
```

#### Flow

```
1. Load config (respects --context)
2. Check cloud.token present
3. Resolve stack slug (cloud.stack or grafana.server hostname)
4. Call GCOM API to fetch stack info
5. Check AgentManagementInstanceURL non-empty
6. Check AgentManagementInstanceID non-zero
7. Check backend URLs derivable (HMInstancePromURL, HLInstanceURL, etc.)
8. Check HMInstancePromID and HMInstancePromClusterID non-zero
9. (Optional) Ping fleet endpoint with auth to verify connectivity
```

#### Deliverables

- `cmd/gcx/setup/check.go` — check command
- `cmd/gcx/setup/check_test.go` — unit tests
- Wire into `cmd/gcx/setup/command.go`

### `gcx setup instrumentation check <cluster>`

Validates instrumentation-specific requirements for a cluster, building on
top of the shared check.

```
$ gcx setup instrumentation check k3d-shopk8s
STEP                           STATUS  DETAILS
Shared config (setup check)    PASS    8/8 checks passed
Fleet API authentication       PASS    Basic auth accepted
RunK8sMonitoring               PASS    API responds
Cluster visible                PASS    k3d-shopk8s found in monitoring response
Discovery endpoints            PASS    SetupK8sDiscovery + RunK8sDiscovery respond
GetAppInstrumentation          PASS    API responds for cluster
GetK8SInstrumentation          PASS    API responds for cluster
```

Failure example:
```
$ gcx setup instrumentation check k3d-shopk8s
STEP                           STATUS  DETAILS
Shared config (setup check)    PASS    8/8 checks passed
Fleet API authentication       PASS    Basic auth accepted
RunK8sMonitoring               PASS    API responds
Cluster visible                FAIL    k3d-shopk8s not found in monitoring response
                                       → Cluster may not have Alloy reporting yet
                                       → Run: gcx setup instrumentation init k3d-shopk8s
```

#### Command

```
gcx setup instrumentation check <cluster> [--context=<name>] [-o json|table]
```

#### Flow

```
1. Run shared check (setup check logic) — fail fast if shared infra broken
2. Authenticate to Fleet API (DoRequest to a known endpoint)
3. Call RunK8sMonitoring with prom headers — verify response
4. Check if <cluster> appears in monitoring response
5. Call SetupK8sDiscovery + RunK8sDiscovery with prom headers — verify response
6. Call GetAppInstrumentation for <cluster> — verify response
7. Call GetK8SInstrumentation for <cluster> — verify response
```

#### Deliverables

- `cmd/gcx/setup/instrumentation/check.go` — check command
- `cmd/gcx/setup/instrumentation/check_test.go` — unit tests
- Wire into `cmd/gcx/setup/instrumentation/command.go`

---

## Part 3: `init` command — bootstrapping

Two layers: `gcx setup init` bootstraps stack credentials (migrates `gcli init`);
`gcx setup instrumentation init <cluster>` bootstraps a cluster for
instrumentation.

### `gcx setup init`

Migrates `gcli init` to gcx. Creates a scoped access policy and token from a
minimal bootstrap token, saves config context.

**Bead**: gcx-b22fd37d (Phase 5: Init/Onboarding)

This is a separate workstream from instrumentation. The plan here documents
the interface contract so `setup instrumentation init` can depend on it, but
implementation follows the gcx-b22fd37d bead.

#### Command

```
gcx setup init [--stack=<slug>] [--tier=readonly|telemetry|cloud-admin] [--ttl=2160h] [--force]
```

#### Flow (migrated from gcli init)

```
1. Resolve bootstrap token (flag → env → interactive prompt)
2. Discover stacks via GCOM API
3. Select stack (--stack flag or interactive prompt)
4. Create access policy with scoped permissions
5. Mint token for the access policy
6. Save config context to ~/.config/gcx/config.yaml
7. Auto-discover datasource UIDs
```

### `gcx setup instrumentation init <cluster>`

Bootstraps a cluster for instrumentation: creates a cluster-scoped access
policy, generates or applies Helm values for the grafana-cloud-onboarding
chart to install Alloy + fleet agent.

#### Command

```
gcx setup instrumentation init <cluster> [--apply] [--helm-namespace=grafana-cloud]
```

#### Flow

```
1. Resolve stack info from cloud config (LoadCloudConfig → GCOM)
2. Create cluster-scoped access policy via GCOM API:
   - Name: "<cluster>-instrumentation"
   - Scopes: fleet, metrics:write, logs:write, traces:write, profiles:write
3. Mint token for the access policy
4. Resolve backend URLs from stack info
5. Generate Helm values for grafana-cloud-onboarding chart:
   - cluster.name, fleet.url, fleet.token, auth.*
   - Mimir/Loki/Tempo/Pyroscope endpoints
6. Default: print helm install command with --set flags
7. With --apply: run helm install directly (requires helm in PATH)
```

#### Design Questions (to resolve during implementation)

- **Access policy API**: Which GCOM endpoint? Confirm schema and required scopes.
- **Idempotency**: Check by name, reuse if exists, warn user.
- **Token rotation**: Long-lived with clear warning (Helm chart needs persistent token).
- **Helm chart version pinning**: `--version` flag with sensible default.
- **Helm chart**: `grafana-cloud-onboarding` vs `grafana-cloud` — tester noted
  the correct chart is `grafana-cloud`, not `k8s-monitoring`.

#### New Dependencies

- GCOM access policy API client (new code in `internal/cloud/`)
- Helm CLI detection (optional, only for `--apply`)

#### Deliverables

- `cmd/gcx/setup/instrumentation/init.go` — init command
- `internal/cloud/accesspolicy.go` — GCOM access policy + token API client
- `cmd/gcx/setup/instrumentation/init_test.go` — unit tests
- Updated CLI reference docs

---

## Part 4: `add` command — imperative edit

Quick imperative fetch-modify-apply for interactive use. Eliminates the need
to manually edit YAML for common operations.

### Command

```
gcx setup instrumentation add <cluster> --namespace=<name> --features=<list> [--apps=<list>]
```

### Examples

```bash
# Enable tracing + logging for namespace "default"
gcx setup instrumentation add k3d-shopk8s \
  --namespace=default --features=tracing,logging

# Same, but pin specific workloads
gcx setup instrumentation add k3d-shopk8s \
  --namespace=default --features=tracing,logging --apps=cartservice,frontend

# K8s monitoring features (no --namespace needed)
gcx setup instrumentation add k3d-shopk8s \
  --features=costmetrics,clusterevents
```

### Flow

```
1. Parse flags: cluster (positional), --namespace, --features, --apps
2. Classify features:
   - App-level: tracing, logging, profiling, processmetrics, extendedmetrics
   - K8s-level: costmetrics, energymetrics, clusterevents, nodelogs
3. If app-level features present:
   a. Require --namespace
   b. GET current app config
   c. Find or create namespace entry, set feature booleans
   d. If --apps: add/update app entries in the namespace
   e. SET app config (with backend URLs)
4. If k8s-level features present:
   a. GET current k8s config
   b. Set feature booleans
   c. SET k8s config (with backend URLs)
5. Print summary of what was changed
```

### Feature Validation

Valid `--features` values:

| Feature | Level | Maps to |
|---------|-------|---------|
| `tracing` | app | `NamespaceConfig.Tracing` |
| `logging` | app | `NamespaceConfig.Logging` |
| `profiling` | app | `NamespaceConfig.Profiling` |
| `processmetrics` | app | `NamespaceConfig.ProcessMetrics` |
| `extendedmetrics` | app | `NamespaceConfig.ExtendedMetrics` |
| `costmetrics` | k8s | `K8sSpec.CostMetrics` |
| `energymetrics` | k8s | `K8sSpec.EnergyMetrics` |
| `clusterevents` | k8s | `K8sSpec.ClusterEvents` |
| `nodelogs` | k8s | `K8sSpec.NodeLogs` |

Invalid feature names → error listing valid options.
Mixed app + k8s features in one command → allowed (executes both SET calls).
App features without `--namespace` → error: `--namespace is required for app-level features`.

### Design Questions (to resolve during implementation)

- **Remove semantics**: Defer to `apply` for removal; keep `add` as
  additive-only for Stage 2.
- **Selection field**: Default `included` for new namespaces.
- **Idempotency**: Running `add` twice with same args should be a no-op
  (already enabled). Not an error.

### Deliverables

- `cmd/gcx/setup/instrumentation/add.go` — add command
- `cmd/gcx/setup/instrumentation/add_test.go` — unit tests
- Updated CLI reference docs

---

## Part 5: Fleet collector filtering and cleanup

Fleet provider UX gap surfaced during smoke testing: `gcx fleet collectors list`
returns all collectors across all clusters with no way to filter or prune stale
entries from previous test runs.

Not instrumentation-specific, but impacts the instrumentation workflow — users
see clutter from old clusters mixed with active ones.

### Changes to `gcx fleet collectors list`

Add `--cluster` filter flag:

```
gcx fleet collectors list                        # all collectors (existing behavior)
gcx fleet collectors list --cluster k3d-shopk8s  # filter by cluster name
```

Implementation: filter client-side on `Collector.RemoteAttributes["cluster"]`
or equivalent cluster identifier field after fetching all collectors.

### Stretch: `gcx fleet collectors prune`

New subcommand to clean up stale collectors:

```
gcx fleet collectors prune --inactive-for 7d     # delete collectors inactive > 7 days
gcx fleet collectors prune --cluster old-cluster  # delete all collectors for a cluster
gcx fleet collectors prune --dry-run              # preview what would be deleted
```

Implementation: filter by `MarkedInactiveAt` timestamp and/or cluster name,
then batch delete with confirmation prompt (or `--force` to skip).

### Design Questions

- **Cluster identification**: How is the cluster name stored on a collector?
  `RemoteAttributes`, `LocalAttributes`, or `Name` prefix? Need to check the
  actual API response shape.
- **Prune safety**: Require `--force` or interactive confirmation? Suggest:
  interactive confirmation by default, `--force` for CI.

### Deliverables

- `internal/providers/fleet/provider.go` — add `--cluster` flag to collector list
- `internal/providers/fleet/provider.go` — new `prune` subcommand (stretch)
- `internal/providers/fleet/provider_test.go` — unit tests
- Updated CLI reference docs

---

## Dependency Order

```
Part 0 (ADR update) ─── no code deps, do first
     │
Part 1 (show→get + discover positional) ─── mechanical refactor
     │
     ├── Part 2 (check) ─── setup check + instrumentation check
     │
     ├── Part 3 (init) ─── setup init (gcx-b22fd37d) + instrumentation init
     │
     ├── Part 4 (add) ─── new command, uses existing instrumentation client
     │
     └── Part 5 (fleet collectors) ─── independent, fleet provider enhancement

```

Parts 2, 3, 4, and 5 are independent of each other. Parts 2–4 depend on
Part 1 (the revised command grammar). Part 5 has no deps on Part 1.
Part 3's `setup init` is tracked under bead gcx-b22fd37d and may be
implemented in a separate workstream.
