---
type: feature-plan
title: "Setup Instrumentation Stage 2: Command grammar, init, and add"
status: draft
spec: docs/specs/feature-setup-instrumentation/spec.md
adr: docs/adrs/instrumentation/001-instrumentation-provider-design.md
beads_id: gcx-4r1g
created: 2026-03-31
---

# Setup Instrumentation Stage 2

Refines the command grammar based on Stage 1 smoke testing, adds the
imperative `add` verb, and introduces `init` for cluster bootstrapping.

## Revised Command Tree

```
gcx setup
├── status                                              # aggregated product status
└── instrumentation
    ├── init      <cluster>                             # bootstrap cluster into Grafana Cloud
    ├── status    [<cluster>]                           # per-cluster instrumentation state
    ├── discover  <cluster>                             # find instrumentable workloads
    ├── add       <cluster> --namespace=X --features=Y  # imperative edit (fetch-modify-apply)
    ├── get       <cluster> [-o yaml|json]              # export current config as manifest
    └── apply     -f <file> [--dry-run]                 # declarative import from manifest
```

### Workflow Model

```
init ──► discover ──► add ──► get ──► apply
 │         │           │       │        │
 │         │           │       │        └─ declarative: make it look like this file
 │         │           │       └────────── export: capture current state as manifest
 │         │           └────────────────── imperative: change one thing now
 │         └────────────────────────────── read: see what's running
 └──────────────────────────────────────── bootstrap: connect cluster to Grafana Cloud

Interactive workflow:  init → discover → add → add → ... → get (save for GitOps)
GitOps workflow:       get (from staging) → git commit → apply (to production)
```

---

## Part 0: Update ADR

Update ADR 014 (`docs/adrs/instrumentation/001-instrumentation-provider-design.md`)
to reflect the revised command grammar and Stage 2 scope.

### Changes

- **Command grammar**: `show` → `get` (kubectl convention for K8s-style manifests),
  `discover --cluster` → `discover <cluster>` (positional arg, consistent with
  `get`, `add`, `init`)
- **Stage 2 scope**: refined from "imperative `add` verb" to three parts:
  grammar fix (Part 1), `init` (Part 2), `add` (Part 3)
- **Command tree**: full tree with all 6 instrumentation subcommands documented
- **Workflow model**: `add` is the interactive tool, `get` + `apply` is the
  export/import loop for GitOps
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

## Part 2: `init` command — cluster bootstrapping

Bridges the gap between "I have a k8s cluster" and "gcx can manage its
instrumentation". Today this requires manually: creating an access policy,
minting a token, finding the Fleet Management URL, and installing a Helm
chart with the correct values.

### Command

```
gcx setup instrumentation init <cluster> [--apply] [--helm-namespace=grafana-cloud]
```

### Flow

```
1. Resolve stack info from cloud config (LoadCloudConfig → GCOM)
2. Create access policy via GCOM API:
   - Name: "<cluster>-instrumentation"
   - Scopes: fleet, metrics:write, logs:write, traces:write, profiles:write
3. Mint token for the access policy
4. Resolve backend URLs from stack info (same as apply)
5. Generate Helm values for grafana-cloud-onboarding chart:
   - cluster.name, fleet.url, fleet.token, auth.*
   - Mimir/Loki/Tempo/Pyroscope endpoints
6. Default: print helm install command with --set flags
7. With --apply: run helm install directly (requires helm in PATH)
```

### Design Questions (to resolve during implementation)

- **Access policy API**: Which GCOM endpoint? `POST /api/v1/accesspolicies`?
  Need to confirm the exact schema and required scopes.
- **Idempotency**: What if the access policy already exists? Reuse? Error?
  Suggest: check by name, reuse if exists, warn user.
- **Token rotation**: Short-lived or long-lived? The Helm chart needs a
  persistent token. Suggest: long-lived with clear warning.
- **Helm chart version pinning**: Hard-code chart version or use latest?
  Suggest: use `--version` flag with sensible default.
- **Scope**: Does `init` belong at `gcx setup instrumentation init` or
  `gcx setup init` (since connecting a cluster is cross-product)? Current
  decision: keep under `instrumentation` for Stage 2, extract to `setup init`
  if other products need the same bootstrap pattern.

### New Dependencies

- GCOM access policy API client (new code in `internal/cloud/`)
- Helm CLI detection (optional, only for `--apply`)

### Deliverables

- `cmd/gcx/setup/instrumentation/init.go` — init command
- `internal/cloud/accesspolicy.go` — GCOM access policy + token API client
- `cmd/gcx/setup/instrumentation/init_test.go` — unit tests
- Updated CLI reference docs

---

## Part 3: `add` command — imperative edit

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

- **Remove semantics**: How to remove a namespace or disable a feature?
  Options: `--features=-tracing` (toggle off), separate `remove` command,
  or rely on `apply` with edited manifest. Suggest: defer to `apply` for
  removal, keep `add` as additive-only for Stage 2.
- **Selection field**: Default `included` for new namespaces? Or require
  explicit `--selection=included|excluded`? Suggest: default `included`.
- **Idempotency**: Running `add` twice with same args should be a no-op
  (already enabled). Not an error.

### Deliverables

- `cmd/gcx/setup/instrumentation/add.go` — add command
- `cmd/gcx/setup/instrumentation/add_test.go` — unit tests
- Updated CLI reference docs

---

## Dependency Order

```
Part 0 (ADR update) ─── no code deps, do first
     │
Part 1 (show→get + discover positional) ─── mechanical refactor
     │
     ├── Part 2 (init) ─── new command, new GCOM client
     │
     └── Part 3 (add) ─── new command, uses existing instrumentation client
```

Parts 2 and 3 are independent of each other but both depend on Part 1
(the revised command grammar).
