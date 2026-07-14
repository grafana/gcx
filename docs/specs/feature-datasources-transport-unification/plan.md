---
type: feature-plan
title: "First-class typed datasources: transport unification"
status: draft
spec: spec.md
created: 2026-07-08
---

# Plan: First-class typed datasources: transport unification

## Pipeline Architecture

The target folds every datasource surface onto one path: discovery ingestion
collapses per-plugin groups to a single canonical descriptor, the router resolves
it to a first-class `TypedCRUD[DataSource]` adapter (with an opt-in
`SecureCarrier` block), and that adapter rides one shared dual-mode transport
whose served-vs-REST decision is made once per stack.

Full target diagram: spec.md §Target Structure. Stage → requirement tracing:
discovery-collapse + malformed-group drop (FR-011, FR-012), typed adapter with
`SecureCarrier` round-trip (FR-001..005), dynamic-client List/Get/Update/Health
+ error normalization (FR-006..010), shared served-ness decision (FR-013),
Option-2 demotion (FR-014).

**Transport decision (uniform):** served-ness is one shared per-stack fact read
from the cached discovery registry. On the served path, any group returning
403/404/5xx demotes the WHOLE listing to REST (Option 2). There is no per-call,
per-surface independent fallback and no parallel per-plugin dynamic fan-out.

## Design Decisions

| Decision | Rationale (traces to) |
|---|---|
| Add an opt-in `SecureCarrier` interface detected via runtime `*T` assertion; leave `TypedObject[T]` unchanged | Mirrors the `restoreName` idiom (typed.go:108-112); non-opting resources stay byte-identical → FR-001, FR-002, FR-003, INV-1, INV-2 |
| Emit `secure` before `spec` and reproduce `ManifestFromDatasource` byte-for-byte | Secret hygiene (names only on read) + output parity depend on exact bytes → FR-004, INV-3 |
| Register `DataSource` as first-class typed via `providers.Register`→`adapter.RegisterAll`; retire the bespoke `datasourceAdapter` | Datasources join dashboards/folders on the shared typed path; no permanent CONSTITUTION exception → FR-005 |
| Rebuild the app-platform transport on `discovery.NewDefaultRegistry` + `dynamic.NewDefaultNamespacedClient`; retain the REST `Client` as the fallback mode | Deletes duplicated hand-rolled `/apis` CRUD; gains the disk cache the hand-rolled version lacked → FR-006, FR-007 |
| Normalize dynamic-client `StatusError` (`ParseStatusError`) → `datasources.APIError` | Keeps the `Transport` error contract transport-agnostic so 403/404/409 render identically on k8s and REST → FR-008, INV-6 |
| `Update` fetches `resourceVersion`, applies it, surfaces 409 as a typed error | Preserves optimistic-concurrency; no silent retry → FR-009 |
| Keep `Health` as thin datasource-specific glue, OR extend `NamespacedClient.Get` with a subresource variant (implementer's choice, documented) | `NamespacedClient` has no subresource wrapper today; naming this keeps the transport rebuild executable → FR-010 |
| Collapse per-plugin groups → canonical at discovery ingestion, scoped strictly by `IsDatasourceGroup`; drop malformed empty-`Kind` groups | Stops the parallel per-plugin fan-out so only the typed adapter runs; fixes the `s.v0alpha1.*` pull bug as a consequence → FR-011, FR-012, INV-8 |
| One shared per-stack served-ness decision read from the cached registry (10-min TTL); no new invalidation logic | Cross-surface parity needs a single shared decision, not per-call fallback → FR-013 |
| Option 2: whole-stack REST demotion on any served-group 403/404/5xx | Permissive "show me what I can see" + forces both surfaces to agree; 5xx-masking flagged as DEFERRED → FR-014, IC-3 |
| Pattern 13: all surfaces acquire the canonical manifest; codecs control display; `-o json\|yaml` byte-identical | Matches `gcx dashboards`; makes cross-surface parity achievable → FR-015 |
| `datasources list -o json\|yaml` renders the canonical manifest; `table`/`wide` keep the curated summary | Pattern 13 while preserving the human view → FR-016, IC-1 |

## Migration / Workstream Sequence

Ordered so each workstream is independently verifiable and the destructive
deletions are gated behind proven parity (**port-before-delete**). Each SHOULD be
a separate commit. Dependencies noted per workstream.

### WS1 — `SecureCarrier` on `TypedCRUD` + golden baseline

*Depends on:* none.

- Capture golden `resources get -o json|yaml` baselines for dashboards and
  folders (non-opting typed resources) BEFORE any change.
- Define the `SecureCarrier` interface; detect it in `ToUnstructured`
  (typed.go:235-278) to emit a top-level `secure` block before `spec`, and in
  `fromUnstructured` (typed.go:281-306) to consume `{create|fromEnv|fromFile}`.
  Leave `TypedObject[T]` (typed.go:47-52) untouched.
- **Verify:** dashboards/folders golden output byte-identical (INV-1); a unit
  test proves a `SecureCarrier` round-trip is byte-identical to
  `ManifestFromDatasource` with secret names only, never values (FR-004, INV-3);
  `go test ./internal/resources/adapter/...` green.

### WS2 — First-class typed `DataSource` registration

*Depends on:* WS1.

- Register `DataSource` via the provider `TypedRegistrations()` shape
  (`adapter.TypedCRUD[DataSource]{ListFn,GetFn,CreateFn,UpdateFn,DeleteFn,
  Namespace,StripFields,Descriptor}.AsAdapter()`; exemplars:
  alert/resource_adapter.go:85-107, slo/provider.go:64-73) under the canonical
  GVK, implementing `SecureCarrier`. The functions initially delegate to the
  existing dsclient transport so behavior is unchanged.
- **Verify:** the router resolves the canonical `datasource.grafana.app/v0alpha1
  DataSource` GVK to the typed adapter (FR-005); `datasources get/create/update/
  delete` output unchanged (INV-4); the bespoke `datasourceAdapter` still
  present (removed at the FINAL GATE), tests green.

### WS3 — Rebuild the app-platform transport with FULL parity

*Depends on:* WS2. *Gates the FINAL GATE deletion.*

- Rebuild `List/Get/Create/Update/Delete` on `discovery.NewDefaultRegistry`
  (served detection + disk cache) + `dynamic.NewDefaultNamespacedClient`:
  List enumerates served per-plugin GVRs and merges (FR-006); Get resolves
  UID→plugin via the discovery index (FR-007); Update fetches `resourceVersion`
  and surfaces 409 (FR-009); Health ported as thin glue or a `NamespacedClient`
  subresource variant (FR-010). Normalize `StatusError` → `datasources.APIError`
  (FR-008). Keep the REST `Client` as the fallback mode. Build this behind the
  WS2 typed adapter functions; do NOT delete the old transport yet.
- **Verify:** a k8s-path 403/404/409 renders identically to the REST path
  (INV-6); a stale-`resourceVersion` update surfaces 409 (FR-009);
  `datasources health <uid>` works on the new path (FR-010); enumerate+merge
  returns the same object set the hand-rolled `listAll` did on a served stack
  (INV-5); transport unit tests green.

### WS4 — Discovery-collapse (scoped to `IsDatasourceGroup`) + malformed-group drop

*Depends on:* WS2.

- At discovery ingestion, fold per-plugin `*.datasource.grafana.app` groups onto
  the single canonical descriptor, scoped strictly by `IsDatasourceGroup`
  (manifest.go:93-98); the same predicate drops malformed empty-`Kind`
  `<type>/v0alpha1` groups (writer.go:40 sink).
- **Verify:** `LookupPreferredPerGroup` returns exactly ONE datasource descriptor
  (no per-plugin fan-out) (FR-011); `resources get datasources` returns one
  canonical set (no 19 dynamic objects, no dup pairs) and exits 0 (IC-2, IC-4);
  `resources pull datasources` writes no `s.v0alpha1.*` dirs and exits 0
  (FR-012); dashboards/folders `get/pull` output byte-identical to the WS1
  baseline (INV-8).

### WS5 — One shared per-stack decision + Option 2 fallback

*Depends on:* WS3, WS4.

- Make served-ness a single per-stack fact read from the cached discovery
  registry (10-min TTL, no new invalidation); both `gcx datasources *` and the
  resources pipeline obtain the same transport (FR-013). Implement Option 2:
  any served-group 403/404/5xx demotes the whole listing to REST on every
  surface (FR-014).
- **Verify:** `datasources list` and `resources get datasources` make the same
  served-vs-REST decision on one stack (FR-013); a served-group 403/404/5xx
  demotes both surfaces to REST and exits 0 — the `grafana-bigquery-datasource`
  500 is masked (documented, IC-3); no hard exit 1 on the read path.

### WS6 — Output parity + `datasources list` codec change

*Depends on:* WS5.

- Ensure all surfaces acquire the canonical manifest (Pattern 13); switch
  `datasources list -o json|yaml` (list.go:103,112-120) to render the canonical
  manifest while `table`/`wide` keep the curated summary (FR-016).
- **Verify:** `datasources get <uid>` and `resources get datasources` json/yaml
  are byte-identical for the same object (FR-015); `datasources list -o
  json|yaml` renders the manifest; `datasources list` default/`-o wide` renders
  the unchanged curated summary (IC-1); reference docs regenerated
  (`GCX_AGENT_MODE=false mise run reference`).

### FINAL GATE — Retire old code + reconcile governance docs

*Depends on:* WS3 (parity proven), WS4, WS5, WS6.

- Delete the bespoke `datasourceAdapter` (`internal/providers/datasources/
  adapter.go`, `resource_adapter.go`), `internal/datasources/k8stransport.go`,
  and `servedgroups.go`. Only proceed once WS3 parity is verified (INV-5).
- Reconcile governance docs with the shipped first-class-typed end state:
  correct ADR-021's Context to the verified dual-read-path model and move it to
  `accepted`; ensure `CONSTITUTION.md` and the `ARCHITECTURE.md` ADR table
  describe datasources as first-class typed (not a TypedCRUD exception).
- **Verify:** `grep` confirms the deleted files/symbols are gone with no dangling
  references; `GCX_AGENT_MODE=false mise run all` passes; the datasources test
  suites (`internal/datasources`, `cmd/gcx/datasources`,
  `internal/providers/datasources`) stay green.

## Compatibility

**Continues unchanged:**
- `TypedObject[T]` struct and every non-opting `TypedCRUD`-backed resource's
  output (dashboards, folders, …).
- `gcx datasources get/create/update/delete` rendered output and exit codes.
- `gcx datasources list` `table`/`wide` curated summary.
- `resources get/pull` output for dashboards, folders, and all non-datasource
  types (discovery-collapse scoped strictly to `IsDatasourceGroup`).
- Legacy REST `Client` (`/api/datasources`) and its typed `APIError`; secret
  hygiene (names only on read); exit-code discipline; the 10 MB response cap.

**Deprecated / removed:**
- The bespoke `datasourceAdapter`, `k8stransport.go`, and `servedgroups.go`.
- The parallel per-plugin dynamic-client fan-out on datasource reads.
- The flat-summary `json|yaml` output of `datasources list` (→ canonical
  manifest; `table`/`wide` retained).

**Newly available:**
- `resources get/pull datasources` works: one canonical object set, one manifest
  shape, no malformed `s.v0alpha1.*` directories, exit 0.
- Byte-identical `-o json|yaml` across `datasources get` and `resources
  get/pull/push` (Pattern 13).
- One shared, disk-cached, per-stack transport decision honored by every surface.
- `SecureCarrier` — reusable `secure`-block support on `TypedCRUD` for future
  resources that outgrow a spec-only envelope.
