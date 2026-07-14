---
type: feature-spec
title: "First-class typed datasources: transport unification"
status: draft
created: 2026-07-08
---

# First-class typed datasources: transport unification

## Problem Statement

Datasources are the one gcx resource whose surfaces **disagree on transport**.
Every other resource is uniform: `gcx dashboards` is uniformly Kubernetes
(`/apis`), `gcx irm oncall` is uniformly REST. Datasources are not — reads
through `gcx resources` run *two* transports in parallel, with no shared
decision and no dedup, producing a mixed-shape result that cannot round-trip.

**Who is affected:** anyone (human or agent) running `gcx resources
get/pull/push/delete datasources`, and anyone consuming `gcx datasources list
-o json|yaml`. The `gcx datasources get/create/update/delete/health` commands
are correct today; the resources-pipeline read path is not.

**Current workaround:** use `gcx datasources list`/`get` (the REST-demoted path)
and avoid `gcx resources get/pull datasources` entirely. `pull` is unusable — it
exits non-zero and writes malformed directories.

### Current behavior (verified 2026-07-07, `dev` stack, anonymized `stacks-NNNNN`)

The canonical `datasource.grafana.app/v0alpha1 DataSource` descriptor is
**always** statically registered — `NewDefaultRegistry` runs `Discover` *then*
`adapter.RegisterAll` (registry.go:69,74). `LookupPreferredPerGroup` returns one
descriptor per group (registry_index.go:143-186; fetch.go:44-52), so a read
resolves a filter set of `{ canonical } ∪ { every served
<plugin>.datasource.grafana.app }`. Each filter routes independently; reads
never normalize GVKs (`NormalizeGVK` is write-only).

```
gcx resources get/pull datasources
        │
        ▼  LookupPreferredPerGroup (PreferredVersionOnly, fetch.go:44-52)
   filter set = { canonical datasource.grafana.app/v0alpha1 }
              ∪ { 39 served <plugin>.datasource.grafana.app/v0alpha1 }
        │
        ├───────────────────────────────┬──────────────────────────────────────┐
        ▼                                ▼
  canonical filter                 per-plugin filters (39 served)
  router EXACT-GVK HIT             router EXACT-GVK MISS (no read-path NormalizeGVK)
        ▼                                ▼
  datasourceAdapter                generic dynamic client (router.go:119-176)
  (registered canonical-only,       NO fallback — pure k8s /apis
   provider.go:37-49)                     │
        ▼                          ┌──────┴───────────────────────────┐
  dual-transport k8sThenREST       ▼                                  ▼
  → DEMOTES to REST          12 malformed no-suffix            7 app-platform-only
        ▼                    <type>/v0alpha1 groups            (full k8s metadata)
  50 objects                 (full k8s metadata)                     │
  ManifestFromDatasource     │                         grafana-bigquery-datasource
  shape (metadata.name+       │                         group HTTP 500 ("plugin not
  namespace only)             │                         found") → HARD EXIT 1 (no fallback)
        └───────────────┬─────┴─────────────────────────────┘
                        ▼
   NO cross-filter dedup (fetch.go:67-76) → merged into map[ResourceRef]
                        ▼
   69-item mongrel: 50 adapter-shaped + 19 dynamic (12 malformed + 7 app-only);
   8 duplicate name-pairs; two incompatible shapes; `get` exits 1
                        ▼
   pull additionally: empty-Kind → writer.go:40 (ToLower(Kind)+"s" = "s") →
   6 malformed s.v0alpha1.{clickhouse,pyroscope,mysql,prometheus,tempo,infinity}
   dirs; `pull` exits 4
```

The two paths make **different transport decisions on the same command**: the
canonical adapter demotes to REST (50), while the per-plugin dynamic fan-out
stays on k8s and hard-errors on the bigquery-500. This split is the defect.
Evidence (verified live, read-only): `api /apis` → 39 served groups, no bare
canonical; `datasources list` → 50; `resources get` → 69 exit 1; `resources
pull` → 6 malformed dirs exit 4.

Two facts constrain the fix (detail in ADR-021 §Context):

1. **The app-platform serves per-plugin groups only** — no bare
   `datasource.grafana.app/v0alpha1` collection; the only type-agnostic
   "all datasources" list is the legacy REST `/api/datasources`.
2. **`TypedObject[T]` is spec-only** (typed.go:47-52) — the top-level `secure`
   block cannot survive its round-trip, which is why datasources are backed by
   a bespoke `datasourceAdapter` today instead of `TypedCRUD`.

## Scope

### In Scope

- Extend `TypedCRUD[T]` with an opt-in `SecureCarrier` interface so a resource
  can round-trip a top-level `secure` block without changing `TypedObject[T]`.
- Register `DataSource` as a first-class typed resource; retire the bespoke
  `datasourceAdapter` (`internal/providers/datasources/`).
- Rebuild the app-platform transport on `discovery.NewDefaultRegistry` +
  `dynamic.NewDefaultNamespacedClient` (per-plugin enumerate+merge for List,
  UID→plugin index for Get), retire `k8stransport.go` and `servedgroups.go`,
  keep the legacy REST `Client` as the fallback mode.
- Collapse per-plugin `*.datasource.grafana.app` groups onto the single
  canonical descriptor at discovery ingestion, scoped strictly by
  `IsDatasourceGroup`; drop malformed empty-`Kind` groups as a consequence.
- One shared per-stack transport decision (served-ness probed once via the
  cached discovery registry), honored identically by `gcx datasources *` and the
  `gcx resources` pipeline.
- RBAC-partial **Option 2**: whole-stack REST demotion on any served-group
  403/404/5xx.
- Output parity (Pattern 13): all surfaces fetch the canonical typed manifest;
  `-o json|yaml` byte-identical across `datasources get` and `resources
  get/pull/push`. `datasources list -o json|yaml` renders the canonical manifest
  (table/wide unchanged).

### Out of Scope

- `gcx datasources` **per-type query subcommands** (`clickhouse query`,
  `cloudwatch query`, `influxdb query`, …). They use product query clients, not
  the datasource CRUD transport, and are unaffected. *Why:* orthogonal subsystem.
- **Non-datasource providers.** Discovery-collapse is scoped strictly to
  `IsDatasourceGroup`; dashboards, folders, and all other typed resources are
  untouched (and must stay byte-identical — see Behavioral Contract). *Why:*
  containment of blast radius.
- New datasource features, flags, or commands beyond the transport unification
  and the two documented behavior changes. *Why:* this is a unification, not a
  feature expansion.
- Generalizing `TypedObject[T]` to carry arbitrary extra top-level sections
  (Approach C, rejected in ADR-021 as speculative). *Why:* `secure` is the only
  known consumer; `SecureCarrier` is the minimal opt-in.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|---|---|---|---|
| Model the `secure` sibling of `spec` | Opt-in `SecureCarrier` interface on `TypedCRUD`, detected via runtime `*T` assertion (like `restoreName`) | Retires the premise that `TypedCRUD` cannot carry `secure`; `TypedObject[T]` struct stays unchanged so non-opting resources are byte-identical | ADR-021 §Decision "Extend TypedCRUD with an opt-in secure block" |
| Datasource adapter backing | First-class typed registration via `providers.Register` → `adapter.RegisterAll`; retire bespoke `datasourceAdapter` | Datasources join dashboards/folders on the shared typed path; no permanent CONSTITUTION exception | ADR-021 §Decision, first-class framing |
| App-platform transport | Rebuild on `discovery.NewDefaultRegistry` (served detection + disk cache) + `dynamic.NewDefaultNamespacedClient`; retire `k8stransport.go`/`servedgroups.go` | Deletes duplicated hand-rolled `/apis` CRUD; gains the `~/.cache/gcx/discovery` disk cache; datasources ride the same plumbing as dashboards | ADR-021 §Decision "Rebuild the app-platform transport" |
| Read routing | Discovery-collapse: fold per-plugin groups → canonical at ingestion, scoped by `IsDatasourceGroup`; drops malformed empty-`Kind` groups | Stops the parallel per-plugin fan-out so only the canonical adapter runs; fixes the `s.v0alpha1.*` pull bug as a consequence; cleaner than normalize-on-read | ADR-021 §Decision "Route reads via discovery-collapse" |
| Transport decision granularity | One shared per-stack decision; served-ness probed once via the cached registry; both surfaces branch on the same result | The cross-surface parity requirement demands a single shared decision, not per-call independent fallback | ADR-021 §Decision "One shared per-stack transport decision" |
| Output model | Pattern 13: codecs control display, not acquisition; canonical manifest fetched by all surfaces; `-o json\|yaml` byte-identical | Matches `gcx dashboards`; makes cross-surface parity achievable | ADR-021 §Decision "Output parity follows gcx dashboards" |
| RBAC-partial semantics | **Option 2** (provisional): whole-stack REST demotion on any served-group 403/404/5xx | Preserves permissive "show me what I can see" and keeps both surfaces in agreement; flagged for internal review (5xx-masking) | ADR-021 §Decision "RBAC-partial demotion" + §Alternatives (Option 1) |

## Current Structure

Key files (verified file:line):

- Typed core — `internal/resources/adapter/typed.go`: `TypedObject[T]` (47-52),
  `restoreName` runtime `*T` assertion (108-112), `ToUnstructured` builds the
  `{apiVersion,kind,metadata,spec}` map (268-274), `fromUnstructured` reads only
  `spec` (281-306).
- Router — `internal/resources/adapter/router.go`: exact-GVK map lookup (63-88);
  Get/List fall through to the generic dynamic client (119-176).
- Discovery — `internal/resources/discovery/registry.go`: `NewDefaultRegistry`
  + disk cache (56-97), `Discover`-then-`RegisterAll` order (69,74);
  `registry_index.go`: `RegisterStatic` (80-118), `LookupPreferredPerGroup`
  (143-186).
- Datasources pkg (to retire/port) — `internal/datasources/`: `manifest.go`
  (`DataSourceManifest` 29-35, `IsDatasourceGroup` 93-98,
  `ManifestFromDatasource` 201-239, `DataSourceMetadata` 39-43),
  `k8stransport.go` (`listAll` 158-198, `errK8sNotServed` 23), `dualtransport.go`
  (`k8sThenREST` 22), `servedgroups.go` (16-25), `client.go` (`SecureJSONFields`
  50), `transport.go` (`Transport` iface + `NewTransport`).
- Providers/datasources (to retire bespoke adapter) —
  `internal/providers/datasources/`: `provider.go` (`TypedRegistrations` 37-49,
  canonical-only), `resource_adapter.go` (`StaticDescriptor` 52-62, GVK
  normalizer 26-47), `adapter.go` (`List`/`Get` 41-140; doc 19-25 explains why
  the custom adapter exists — the spec-only-envelope gap `SecureCarrier` closes).
- Commands — `cmd/gcx/datasources/list.go` (flat summary 103,112-120),
  `get.go` (manifest 81); `cmd/gcx/resources/fetch.go` (44-76 read pipeline).
- Malformed sink — `internal/resources/local/writer.go:40` (empty-`Kind` → "s");
  `PluralsFromFilters` (50-56).

```
gcx datasources *                       gcx resources get/pull/push/delete datasources
  dsclient.NewTransport(cfg)               (read) discovery per-plugin GVKs → router MISS → dynamic client
  → dualTransport.k8sThenREST              (write) NormalizeGVK → router HIT → datasourceAdapter → dualTransport
        │                                        │
        ▼                                        ▼
  dualTransport (per-call fallback)        [READS bypass the shared decision]
        │                                   canonical filter → adapter → REST (50)
   ┌────┴─────┐                             per-plugin filters → dynamic client, NO fallback (19)
   ▼          ▼                             → 69 mongrel, 8 dup pairs, exit 1; pull → 6 s.v0alpha1.* dirs, exit 4
 k8sTransport   REST Client
 (hand-rolled   (/api/datasources)
  /apis CRUD)
 servedgroups.go (process-scoped served cache — no disk cache)
```

**Pain points / technical debt:**

- Two transport mechanisms and three independent fallback decisions coexist;
  reads have no shared decision and no dedup.
- `servedgroups.go` hand-GETs `/apis` and parses `groups[].name`, duplicating the
  cached discovery registry and lacking its disk cache.
- Per-op CRUD is raw `net/http`, duplicating `dynamic.NamespacedClient` (the
  client dashboards ride).
- The bespoke `datasourceAdapter` exists only because `TypedObject[T]` cannot
  carry the `secure` sibling of `spec`.

## Target Structure

```
gcx datasources *          gcx resources get/pull/push/delete datasources
        │                          │
        └────────────┬─────────────┘
                     ▼
  discovery ingestion (RegistryIndex.Update / FilterDiscoveryResults):
    per-plugin *.datasource.grafana.app groups ─COLLAPSE─┐  scoped strictly by
    malformed empty-Kind <type>/v0alpha1 groups ─DROP────┤  IsDatasourceGroup
                                                          ▼
    ONE canonical descriptor: datasource.grafana.app/v0alpha1 DataSource
                     ▼
  LookupPreferredPerGroup → ONE descriptor (per-plugin fan-out ELIMINATED)
                     ▼
  router EXACT-GVK HIT → TypedCRUD[DataSource] + SecureCarrier
    envelope: apiVersion / kind / metadata / secure / spec   (secure before spec)
                     ▼
  ONE shared dual-mode transport (obtained identically by both surfaces)
    served-ness probed once via discovery.NewDefaultRegistry
    (~/.cache/gcx/discovery, 10-min TTL — no new invalidation logic)
        │
        ├─ served? ─yes─▶ dynamic client (dynamic.NewDefaultNamespacedClient):
        │                   List  = enumerate served per-plugin GVRs + merge
        │                   Get   = resolve UID→plugin via discovery index
        │                   Update= fetch resourceVersion, apply, surface 409
        │                   Health= datasource health subresource (ported glue)
        │                   errors: ParseStatusError → normalize to datasources.APIError
        │
        └─ not served / Option-2 demote (any served-group 403/404/5xx)
                        ─▶ legacy REST /api/datasources (Client, retained)
```

**Improvements:** one transport, one decision, uniform behavior across every
surface; the read defects (raw unstructured output, 69-item mongrel, malformed
`s.v0alpha1.*` dirs, spurious exit 1/4) are resolved; duplicated hand-rolled
`/apis` CRUD is deleted; discovery gains a disk cache; `TypedCRUD` gains reusable
`secure`-block support for future resources.

## Functional Requirements

- **FR-001** — The system MUST define a `SecureCarrier` interface that
  `TypedCRUD[T]` detects at runtime via an `any(item).(SecureCarrier)`-style
  assertion, mirroring `restoreName` (typed.go:108-112). It MUST NOT alter the
  `TypedObject[T]` struct (typed.go:47-52).
- **FR-002** — WHEN a resource implementing `SecureCarrier` is serialized via
  `ToUnstructured` (typed.go:235-278), the system SHALL emit a top-level `secure`
  block as a sibling of `spec`, marshaled **before** `spec` (envelope order:
  `apiVersion, kind, metadata, secure, spec`), derived from the wire secure
  fields as `{ fieldName: { name: fieldName } }` — names ONLY.
- **FR-003** — WHEN a resource implementing `SecureCarrier` is deserialized via
  `fromUnstructured` (typed.go:281-306), the system SHALL consume the top-level
  `secure` values (`{create|fromEnv|fromFile}`) into the wire secure data.
- **FR-004** — The datasource `secure` round-trip MUST reproduce today's
  `ManifestFromDatasource` output byte-for-byte (manifest.go:29-35, 201-239),
  including struct order and `omitempty` behavior.
- **FR-005** — The system MUST register `DataSource` as a first-class typed
  resource under the canonical GVK factory (`providers.Register` →
  `adapter.RegisterAll`) and MUST retire the bespoke `datasourceAdapter`.
- **FR-006** — The rebuilt app-platform transport `List` MUST enumerate the
  served per-plugin GVRs and merge them into one collection, built on
  `discovery.NewDefaultRegistry` + `dynamic.NewDefaultNamespacedClient`.
- **FR-007** — The rebuilt app-platform transport `Get` MUST resolve UID→plugin
  via the discovery-backed index (no per-plugin brute-force).
- **FR-008** — Errors from the dynamic-client path (k8s `StatusError` via
  `ParseStatusError`, dynamic/errors.go:34) MUST be normalized to the package
  typed error (`datasources.APIError`) so the `Transport` error contract is
  transport-agnostic.
- **FR-009** — `Update` MUST fetch the current `resourceVersion`, apply it, and
  surface a 409 as a typed error (no silent re-fetch-retry).
- **FR-010** — The datasource **health subresource** MUST be ported onto the new
  path (as thin glue, or via a `NamespacedClient` subresource variant) before
  `k8sTransport` is deleted.
- **FR-011** — At discovery ingestion, the system MUST fold per-plugin
  `*.datasource.grafana.app` groups onto the single canonical
  `datasource.grafana.app/v0alpha1 DataSource` descriptor, scoped **strictly** by
  `IsDatasourceGroup` (manifest.go:93-98), so `LookupPreferredPerGroup` returns
  exactly one datasource descriptor.
- **FR-012** — The same ingestion predicate MUST drop malformed empty-`Kind`
  `<type>/v0alpha1` datasource groups, so no `s.v0alpha1.*` directories are
  produced by `resources pull datasources`.
- **FR-013** — Served-ness MUST be probed once via the shared cached discovery
  registry; `gcx datasources *` and the `gcx resources` pipeline MUST obtain the
  same transport and branch on the same result. The system MUST reuse the
  existing `~/.cache/gcx/discovery` disk cache (10-min TTL) and MUST NOT add new
  cache-invalidation logic or flags.
- **FR-014** — IF any served datasource group returns 403, 404, or 5xx during
  enumeration THEN the whole listing SHALL demote to the legacy REST
  `/api/datasources`, applied identically to every surface (Option 2).
- **FR-015** — All datasource surfaces MUST fetch the canonical typed manifest;
  `-o json` and `-o yaml` output MUST be byte-identical across `datasources get`
  and `resources get/pull/push` for the same object (Pattern 13).
- **FR-016** — `datasources list -o json|yaml` MUST render the canonical
  manifest. `datasources list` `table`/`wide` output MUST keep the curated
  summary (`access,default,name,readOnly,type,uid,url`).

## Behavioral Contract

### Invariants (MUST hold — no behavior change)

- **INV-1 — Non-opting typed resources byte-identical.** Dashboards, folders,
  and every other `TypedCRUD`-backed resource that does NOT implement
  `SecureCarrier` MUST emit output byte-identical to today. Locked by golden
  output tests.
- **INV-2 — `TypedObject[T]` struct unchanged.** The struct (typed.go:47-52)
  MUST NOT gain, lose, or reorder fields; `secure` support rides the interface,
  not the struct.
- **INV-3 — Secret round-trip byte-identical; values never leaked.** The
  datasource `secure` block MUST round-trip byte-identically to today's
  `ManifestFromDatasource`, and read output MUST contain secret **names only**
  (`{name: ...}`), never values.
- **INV-4 — `datasources get`/`create`/`update`/`delete` output unchanged.**
  These surfaces are correct today; their rendered output and exit codes MUST NOT
  change (only their transport internals may).
- **INV-5 — `k8sTransport` coverage preserved before deletion.** Per-plugin
  enumerate+merge, error mapping, optimistic-concurrency on update, and the
  health subresource MUST be ported to the new path and tested before
  `k8stransport.go`/`servedgroups.go` are removed.
- **INV-6 — Typed-error contract transport-agnostic.** A k8s-path 403/404/409
  MUST render identically to the REST path: `StatusError` is normalized to
  `datasources.APIError` so `IsNotFound`, 409-surfacing, `health` row messages,
  and `cmd/gcx/fail/convert.go` classification behave identically on both paths.
- **INV-7 — Exit-code discipline unchanged.** Usage → 2, auth/permission → 3,
  partial failure (delete/health) → 4. A *correct* `resources get/pull
  datasources` MUST exit 0.
- **INV-8 — Discovery-collapse scoped strictly to `IsDatasourceGroup`.** No
  non-datasource group is collapsed, reordered, or dropped; `resources
  get/pull` output for dashboards, folders, and all other types is unchanged.

### Intentional Changes (deliberate — each justified)

1. **IC-1 — `datasources list -o json|yaml`: flat summary → canonical manifest.**
   *Justification:* Pattern 13 parity requires all surfaces to acquire the
   canonical manifest; the flat summary survives only as `table`/`wide`. This is
   a documented, breaking output change for scripts parsing `list -o json`.
2. **IC-2 — Unified fan-out elimination.** `resources get/pull datasources`
   returns ONE canonical object set (was the 69-item mongrel = 50 adapter-shaped
   + 19 dynamic). No per-plugin dynamic objects, no duplicate name-pairs, one
   consistent manifest shape. *Justification:* the parallel per-plugin fan-out
   with no dedup is the root defect.
3. **IC-3 — Option 2 whole-stack REST demotion (incl. 5xx-masking).** Any
   served-group 403/404/5xx demotes the whole listing to REST, on every surface.
   *Justification:* preserves the permissive "show me what I can see" behavior and
   forces both surfaces to agree. **FLAG:** this MASKS genuine app-platform
   breakage — the live `grafana-bigquery-datasource` 500 (today a hard exit 1)
   becomes silent, and app-platform-only objects vanish on both surfaces when
   demotion fires. Option 1 (surface, do not demote) is recorded under Open
   Questions and Risks; the 5xx-masking is DEFERRED for internal discussion.
4. **IC-4 — `resources get/pull datasources` shape and exit-code fix.** Reads no
   longer emit raw unstructured objects; `pull` no longer writes `s.v0alpha1.*`
   directories; a correct run exits 0 (was `get` exit 1, `pull` exit 4).
   *Justification:* consequence of IC-2 + FR-012; the prior output cannot
   round-trip on push.

## Acceptance Criteria

### Group A — SecureCarrier secret round-trip (FR-001..004, INV-3)

- GIVEN a datasource whose wire response carries `SecureJSONFields`
  `{basicAuthPassword, httpHeaderName1}`
  WHEN it is read via any surface with `-o yaml`
  THEN the manifest emits a top-level `secure: { basicAuthPassword: {name:
  basicAuthPassword}, httpHeaderName1: {name: httpHeaderName1} }` block, ordered
  BEFORE `spec`, containing NO secret values, byte-identical to today's
  `ManifestFromDatasource`.
- GIVEN a manifest carrying `secure: { basicAuthPassword: {create: "s3cr3t"} }`
  (or `{fromEnv|fromFile}`)
  WHEN it is applied via `push`/`create`/`update`
  THEN the value is consumed into the wire secure data and sent, and a
  subsequent read returns only `{name: basicAuthPassword}`.

### Group B — Non-opting typed resources unchanged (INV-1, INV-2, FR-001)

- GIVEN dashboards and folders (which do NOT implement `SecureCarrier`)
  WHEN they are rendered via `resources get -o json|yaml` after the change
  THEN the output is byte-identical to a captured pre-change golden baseline.
- The `TypedObject[T]` struct (typed.go:47-52) MUST retain exactly its current
  fields and order — verified by the type definition remaining unchanged.

### Group C — First-class registration + discovery-collapse (FR-005, FR-011, INV-8, IC-2)

- GIVEN a stack serving per-plugin datasource groups
  WHEN a datasource read resolves descriptors via `LookupPreferredPerGroup`
  THEN exactly ONE datasource descriptor (the canonical
  `datasource.grafana.app/v0alpha1 DataSource`) is returned — no per-plugin
  descriptor and no parallel dynamic-client fan-out occurs.
- GIVEN the canonical descriptor
  WHEN the router resolves it
  THEN it routes to the first-class typed `DataSource` adapter (the bespoke
  `datasourceAdapter` no longer exists).
- GIVEN a stack also serving dashboards, folders, and other non-datasource typed
  groups
  WHEN discovery ingestion runs the datasource collapse
  THEN no non-datasource group is collapsed, reordered, or dropped, and
  `resources get/pull` output for dashboards and folders is byte-identical to a
  pre-change baseline (INV-8).

### Group D — Malformed pull directories gone (FR-012, IC-4)

- GIVEN a stack serving loki/prometheus/tempo/mysql/clickhouse/pyroscope/infinity
  WHEN `gcx resources pull datasources` runs into a fresh directory
  THEN NO `s.v0alpha1.*` directory is produced, each type is written once, the
  manifests round-trip on `push`, and the command exits 0.

### Group E — Shared per-stack decision (FR-013)

- GIVEN one stack
  WHEN `gcx datasources list` and `gcx resources get datasources` both run
  THEN both obtain the same transport from the shared cached discovery registry
  and make the SAME served-vs-REST decision for the same inputs.

### Group F — Output parity byte-identical (FR-015, Pattern 13)

- GIVEN the same datasource object
  WHEN captured via `gcx datasources get <uid> -o json|yaml` and via `gcx
  resources get datasources -o json|yaml`
  THEN the two manifests are byte-identical (`table`/`wide`/`text` MAY differ).

### Group G — Option 2 demotion on 403/404/5xx (FR-014, IC-3)

- GIVEN a stack where a served group returns 403, 404, or 5xx (the live
  `grafana-bigquery-datasource` 500 is the concrete 5xx case)
  WHEN `gcx resources get datasources` or `gcx datasources list` runs
  THEN the whole listing demotes to the legacy REST `/api/datasources` on BOTH
  surfaces, the command exits 0 (the 500 is masked — a DEFERRED concern), and no
  per-plugin dynamic fan-out or hard exit 1 occurs.

### Group H — Transport parity: list/get, errors, concurrency, health (FR-006, FR-007, FR-008..010, INV-5, INV-6)

- GIVEN a stack serving multiple per-plugin datasource groups
  WHEN the rebuilt transport `List` runs in served mode
  THEN it enumerates every served per-plugin GVR and merges them into one
  collection matching the set the retired `k8sTransport.listAll` returned
  (per-plugin enumerate+merge parity — FR-006, INV-5 enumerate clause).
- GIVEN a valid datasource UID on a served stack
  WHEN the rebuilt transport `Get` runs
  THEN it resolves the UID to the correct plugin via the discovery-backed index
  (no per-plugin brute-force) and returns that datasource's manifest (FR-007).
- GIVEN the app-platform path returns 403/404/409
  WHEN the operation completes
  THEN the surfaced error is a `datasources.APIError` with the matching status,
  rendering identically to the REST path (`IsNotFound`, 409-surfacing, and
  `fail/convert.go` classification behave the same on both paths).
- GIVEN two concurrent updates to one datasource
  WHEN the second update is applied with a stale `resourceVersion`
  THEN a 409 is surfaced as a typed error (no silent retry).
- GIVEN a datasource UID
  WHEN `gcx datasources health <uid>` runs on the rebuilt transport
  THEN the health subresource returns the same result shape as today.

### Group I — `datasources list` codec change (FR-016, IC-1)

- GIVEN a stack with datasources
  WHEN `gcx datasources list -o json|yaml` runs
  THEN it renders the canonical manifest (not the flat summary).
- GIVEN the same stack
  WHEN `gcx datasources list` (default) or `-o wide` runs
  THEN it renders the curated table summary
  (`access,default,name,readOnly,type,uid,url`), unchanged.

### Group J — Datasource commands unchanged (INV-4)

- GIVEN the `gcx datasources get <uid>`, `create`, `update`, and `delete`
  commands
  WHEN they run on the rebuilt transport
  THEN their rendered output and exit codes are byte-identical to a captured
  pre-change baseline (only transport internals changed).

## Negative Constraints

- **NEVER** mutate the `TypedObject[T]` struct (typed.go:47-52) — `secure`
  support MUST ride the `SecureCarrier` interface only.
- **NEVER** apply discovery-collapse to any group for which `IsDatasourceGroup`
  returns `false`.
- **NEVER** delete `k8stransport.go` or `servedgroups.go` before full parity
  (per-plugin enumerate+merge, error mapping, optimistic-concurrency, health) is
  ported AND covered by tests.
- **NEVER** return datasource secret values on read — emit secret names only.
- **NEVER** regress non-opting typed resources (dashboards, folders, …) output.
- **NEVER** let a raw k8s `StatusError` escape the datasource `Transport`
  boundary — it MUST be normalized to `datasources.APIError`.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Threading `secure` touches `TypedCRUD` machinery shared by all typed resources | Silent output drift on dashboards/folders/etc. | `SecureCarrier` is opt-in; `TypedObject[T]` struct unchanged (INV-1, INV-2); golden output tests for non-opting resources (AC Group B) |
| Retiring `k8sTransport` removes a well-tested component | Lost coverage: enumerate+merge, error mapping, 409, health | Port-before-delete gate (INV-5, plan FINAL GATE); parity + typed-error tests (AC Group H) before deletion |
| Discovery-collapse changes behavior at a site shared by all resources | Non-datasource descriptors dropped/reordered | Scope strictly to `IsDatasourceGroup` (INV-8, FR-011); test that dashboards/folders `get/pull` output is unchanged |
| `datasources list -o json\|yaml` shape change | Scripts/agents parsing the flat summary break | Documented break (IC-1); `table`/`wide` unchanged; announce in CHANGELOG |
| Option 2 masks 5xx and drops app-platform-only objects | Genuine app-platform breakage becomes invisible; per-token RBAC gap flips a per-stack decision | FLAG prominently (IC-3); DEFERRED open question; Option 1 recorded as the alternative; the bigquery-500 is the tracked concrete case |
| Typed-error normalization gap on the k8s path only | 403/404/409 UX + create-vs-update routing regress on k8s path, uncaught by REST-path tests | INV-6 + AC Group H: test that a k8s-path 403/404/409 renders identically to REST |

## Open Questions

- **[DEFERRED]** RBAC-partial Option 2 5xx-masking. Option 2 is implemented now
  (FR-014, IC-3) but masks genuine app-platform 5xx (the live
  `grafana-bigquery-datasource` 500) and hides app-platform-only objects when
  demotion fires. Option 1 (surface the partial-authorization error, keep the
  app-platform view for readable groups, reserve REST fallback for "not served at
  all") is the recorded alternative. To be resolved in internal discussion.
- **[RESOLVED]** Current-state model. Verified empirically 2026-07-07: reads run
  a **dual read-path fan-out** (canonical→adapter→REST = 50 objects ‖ per-plugin
  → generic dynamic client = 19 objects, no dedup → 69-item mongrel, `pull` → 6
  malformed dirs). This is NOT an "adapter is unreachable / dead code" model —
  the canonical adapter IS reached and produces the REST-demoted set; the
  per-plugin dynamic fan-out runs in parallel with no fallback (see the
  verified current-behavior diagram in the Problem Statement).
- **[DEFERRED]** Correct ADR-021's Context to match the verified model (its
  "reads bypass the adapter, no fallback, generic dynamic client" phrasing reads
  as adapter-unreachable). ADR-021 is still `proposed`/uncommitted; the
  correction can land with this feature.
