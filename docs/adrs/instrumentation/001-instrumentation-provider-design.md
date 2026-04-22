# Declarative Instrumentation Setup under `gcx setup`

**Created**: 2026-03-30
**Status**: superseded by [002-top-level-provider.md](002-top-level-provider.md)
**Bead**: gcx-4r1g
**Supersedes**: none

## Context

Grafana Cloud's Instrumentation Hub (public preview Dec 2025) provides a
control plane for discovering Kubernetes services and applying observability
instrumentation at scale. The underlying API is the Fleet Management
gRPC/Connect service (`instrumentation.v1.InstrumentationService` and
`discovery.v1.DiscoveryService`).

`grafana-cloud-cli` exposes this imperatively: `gcx fleet instrumentation
{app|k8s} {get|set}` with flag-based and file-based mutation, plus
`fleet discovery {setup|run-app|run-k8s}`. Each command mutates server
state directly — there is no local representation, no portable config,
no drift detection.

**gcx has no instrumentation support today.** The fleet provider handles
only pipelines and collectors — low-level primitives that instrumentation
operates on top of. This is a migration gap that blocks users who manage
instrumentation programmatically or via CI/CD, and blocks AI agents that
need to discover and instrument services autonomously.

Key forces:

1. **Instrumentation is onboarding, not CRUD.** The user journey is
   discover → configure → verify, not create/update/delete on resources.
   This pattern recurs across other Grafana Cloud products (KG setup,
   integrations install, auth bootstrap). A wider `gcx setup` framework
   is planned (see `docs/research/2026-03-30-setup-framework.md`).

2. **Declarative > imperative for durability.** Instrumentation config
   should be a versionable, portable artifact — pulled from one stack,
   committed to git, applied to another stack via `--context`. The old
   CLI's imperative commands leave no such artifact.

3. **Same API server & auth as fleet.** Both hit the Fleet Management
   gRPC/Connect API with the same base URL and credentials. The shared
   client infrastructure should be extracted rather than duplicated.

4. **Agent-friendliness.** An AI agent should be able to chain
   `discover → show → apply → status` with structured JSON at every
   step. The declarative manifest is also the natural handoff artifact
   between agent and human ("here's what I configured — commit it").

5. **Fleet-managed pipeline protection.** `SetAppInstrumentation`
   creates Beyla pipelines (`beyla_k8s_appo11y_<cluster>`) under the
   hood. The fleet provider's `pipeline update/delete` commands must
   guard against editing these directly.

## Decision

### Instrumentation lives under `gcx setup`

Instrumentation is the first product in a new top-level `gcx setup`
area. The area follows `$AREA $NOUN $VERB` grammar where `setup` is
the area, the product name is the noun, and operations are verbs.

Command tree:

```
gcx setup instrumentation
├── status [--cluster <name>]         Show Alloy collector presence + Beyla health
├── discover --cluster <name>         Find workloads/namespaces via discovery API
├── show <cluster> [-o yaml|json]     Current config as portable manifest
└── apply -f <file> [--dry-run]       Apply InstrumentationConfig manifest
```

`gcx setup status` (without a product subcommand) will aggregate status
across all registered setup products. For now, instrumentation is the
only product; the framework is designed to accept more.

### Declarative manifest format

One manifest per cluster. Combined App O11y + K8s Monitoring in a single
`InstrumentationConfig` kind. Either section may be omitted — present
sections are applied, absent sections are untouched.

```yaml
apiVersion: setup.grafana.app/v1alpha1
kind: InstrumentationConfig
metadata:
  name: prod-1                              # cluster name = resource identity
spec:
  app:
    namespaces:
      - name: frontend
        selection: included
        tracing: true
        processmetrics: true
        extendedmetrics: true
        apps:                               # per-workload overrides (optional)
          - name: noisy-svc
            selection: excluded
      - name: data
        selection: included
        tracing: true
        logging: true
        profiling: true
      - name: monitoring
        selection: excluded
  k8s:
    costmetrics: true
    energymetrics: true
    clusterevents: true
    nodelogs: true
```

Key properties:

- **Environment-agnostic.** Datasource URLs (Mimir, Loki, Tempo,
  Pyroscope) are deliberately omitted from manifests — auto-populated
  from the target stack context on `apply`.
- **Cluster name is identity.** `metadata.name` is the K8s cluster name
  as reported by Alloy to Fleet Management.
- **Portability axis is stack.** Same manifest applied to different
  Grafana Cloud stacks via `--context`. One stack can observe many
  clusters, so users maintain one manifest per cluster.
- **Per-workload granularity.** `apps` entries within a namespace allow
  include/exclude of individual workloads, matching the Instrumentation
  Hub UI's capabilities.

### Apply semantics: optimistic locking

`apply` does not blindly replace remote state. It:

1. Fetches current remote config (GET)
2. Compares with local manifest
3. If remote has state not present in the manifest (e.g., a namespace
   instrumented remotely but absent from the file) → **fail** with a
   clear error suggesting `show -o yaml` to reconcile
4. If manifest is a superset of or matches remote → apply (SET)

This prevents accidentally dropping configuration and forces explicit
reconciliation before destructive changes.

### Shared fleet client package

Extract shared gRPC/Connect client infrastructure from the existing
fleet provider into `internal/fleet/`:

```
internal/fleet/
├── client.go      Base HTTP client (doRequest, auth, Prom headers)
├── config.go      Shared config keys (fleet_url, fleet_token, instance_id)
└── errors.go      Common error types and translation

         ↑                    ↑
providers/fleet/              providers/instrumentation/
(pipelines, collectors)       (setup product: discover/show/apply/status)
```

Both providers import from `internal/fleet/`. Each adds domain-specific
methods as thin wrappers.

### Fleet-managed pipeline protection

The fleet provider's `pipeline update` and `pipeline delete` commands
will guard against editing pipelines whose name starts with
`beyla_k8s_appo11y_`. Error message directs users to
`gcx setup instrumentation apply` instead. A `--force` flag overrides.

### Two-stage delivery

**Stage 1 (this ADR): Declarative apply.**
`discover`, `show`, `apply -f`, `status`. Manifest-first workflow.
Shared fleet client extraction. Pipeline protection guard.

**Stage 2 (follow-up): Imperative add.**
`gcx setup instrumentation add --cluster X --namespace Y --tracing` —
flag-based fetch-modify-apply for quick agent/human edits. Imperative
sugar over the declarative core. Deferred to validate that the
declarative approach works well first.

### Rejected alternatives

**A. Extend fleet provider.** Would grow the fleet provider to 2500+
lines and mix CRUD resource management with onboarding configuration.
The command path `gcx fleet instrumentation app set` is deeper than
needed and doesn't fit the onboarding mental model.

**B. Standalone `gcx instrumentation` provider.** Originally proposed,
but the broader `gcx setup` framing emerged from recognizing that
instrumentation, KG setup, integrations install, and auth bootstrap all
follow the same discover → configure → verify pattern. Instrumentation
should be the first product in this framework, not an island.

**C. Imperative-first (mirror grafana-cloud-cli).** Flag-based `set`
commands are convenient for one-offs but produce no durable artifact.
Declarative manifests enable git versioning, code review, cross-stack
portability, and drift detection. Imperative convenience is deferred to
Stage 2 as `add`.

## Consequences

### Positive

- Instrumentation config becomes a versionable, portable artifact
- `show -o yaml` bridges imperative agent sessions to declarative git
  workflows — agent configures, human commits
- `gcx setup` framework is extensible to other onboarding products
- Shared `internal/fleet/` eliminates client duplication
- Optimistic locking on `apply` prevents accidental config loss
- Pipeline protection guard prevents low-level interference with
  fleet-managed Beyla pipelines
- Aligns with Instrumentation Hub UI's capabilities (per-namespace,
  per-workload, per-signal granularity)

### Negative

- Declarative-only in Stage 1 means more steps for quick edits
  (show → edit file → apply vs single flag command). Mitigated by
  Stage 2 `add` verb.
- New top-level `setup` area is a structural addition to gcx — needs
  CONSTITUTION.md and architecture docs updated.
- Migration friction vs grafana-cloud-cli namespace. Mitigate with
  migration docs and `gcx commands` discoverability.
- Refactoring cost to extract `internal/fleet/` shared client adds
  scope to the first PR.

### Follow-up

- `/plan-spec` for Stage 1 implementation
- Create `gcx setup` area scaffold (cmd/gcx/setup/, internal/setup/)
- Extract `internal/fleet/` shared client
- Add pipeline protection guard to fleet provider
- Update CONSTITUTION.md, architecture docs, migration-gap-analysis.md
- Stage 2: `add` verb for imperative flag-based configuration
