# First-class typed datasources: transport unification via extended TypedCRUD

**Created**: 2026-07-07
**Status**: proposed
**Supersedes**: none

## Context

Datasources are the one resource type in gcx whose surfaces disagree on transport.
Two mechanisms and three independent fallback decisions coexist:

| Surface | Path to data | REST fallback? |
|---|---|---|
| `gcx datasources *` | `dsclient.NewTransport` → `dualTransport.k8sThenREST[T]` | yes, per call |
| `resources push/delete datasources` | `NormalizeGVK` → router → `datasourceAdapter` → `dualTransport` | yes, inherits the adapter |
| `resources get/pull datasources` | discovery enumerates per-plugin GVKs → router misses → **generic dynamic client** | **no** |

The divergence is not in the transport internals; it is in **routing**.
`NormalizeGVK` — which collapses a per-plugin group
(`prometheus.datasource.grafana.app`) onto the canonical `datasource.grafana.app`
so the router can reach the adapter — is applied only on the write path
(`internal/resources/remote/pusher.go` and `deleter.go`). Reads never normalize,
so on a stack that serves per-plugin groups the router receives per-plugin GVKs,
finds no registered adapter (the adapter is registered only under the canonical
GVK, in `internal/providers/datasources/resource_adapter.go`), and falls through
to the generic dynamic client with no fallback (`internal/resources/adapter/router.go`,
`Get`/`GetMultiple`).

Two verified facts constrain any fix:

1. **The app-platform serves datasources as per-plugin groups only.** There is no
   canonical `datasource.grafana.app/v0alpha1` collection on the Kubernetes API —
   `IsDatasourceGroup` (`internal/datasources/manifest.go`) returns `("", false)`
   for the base group, and `k8sTransport.listAll` (`internal/datasources/k8stransport.go`)
   enumerates each served `{plugin}.datasource.grafana.app` group and merges. The
   canonical GVK is a gcx-internal routing fiction. The only type-agnostic
   "all datasources" endpoint is the legacy REST `/api/datasources`.
2. **`TypedObject[T]` is spec-only** (`TypeMeta` + `ObjectMeta` + `Spec T`, in
   `internal/resources/adapter/typed.go`). Its round-trip reads only `spec` and
   emits only `apiVersion`/`kind`/`metadata`/`spec`, so the datasource top-level
   `secure` block (a sibling of `spec` on `DataSourceManifest`) cannot survive.
   This is precisely why datasources are backed by a bespoke `datasourceAdapter`
   instead of `TypedCRUD` today.

**Why now.** A prior spec (the PR-881 follow-ups remediation, superseded by this
ADR and never merged) was approved and then halted at build time: it assumed the
adapter already mediated reads and declared the routing and fallback layers out
of scope, so its output-parity invariants were unachievable and building it
would have shipped a broken result. Meanwhile the defects
are live: `resources get datasources` returns raw unstructured objects and errors;
`resources pull datasources` writes malformed `s.v0alpha1.*` directories and exits
non-zero; and the object set diverges by surface (a smaller REST set versus a
larger app-platform set) purely because one surface has the REST fallback and the
other does not.

The requirement is a single, runtime-detected, per-stack transport decision —
"does this stack serve the app-platform datasource API?" — honored uniformly by
every surface, the way `gcx dashboards` is uniformly Kubernetes and
`gcx irm oncall` is uniformly REST.

Related decisions: ADR-008 / ADR-010 (the `TypedCRUD` / `TypedResourceAdapter`
foundation this extends), ADR-016 (the dashboards direct-dynamic-client
exception), ADR-020 (the Synthetic Monitoring dual-mode transport precedent).

## Decision

Make datasources a **first-class typed resource** by **extending `TypedCRUD`**,
not by codifying a permanent exception. Retire the custom `datasourceAdapter` and
the hand-rolled `k8sTransport`. Route every surface through one shared dual-mode
transport built on the resources discovery registry and dynamic client, and make
reads reach it by collapsing per-plugin datasource groups at discovery ingestion.

```
   gcx datasources *          resources get/pull/push/delete datasources
          │                            │
          │        discovery ingestion: per-plugin *.datasource.grafana.app groups
          │            ──collapse (predicate: IsDatasourceGroup)──▶ ONE canonical
          │            (also drops malformed empty-Kind groups)      DataSource descriptor
          └───────────┬────────────────┘
                       ▼
     TypedCRUD[DataSource]   ← first-class typed registration (providers.Register → adapter.Register)
     envelope: apiVersion / kind / metadata / spec / secure    ← via opt-in SecureCarrier interface
                       ▼
     one shared dual-mode transport
       discovery.NewDefaultRegistry  → served-ness probed once (~/.cache/gcx/discovery)
       served? ─yes─▶ dynamic client: enumerate per-plugin GVRs + merge (List); UID→plugin index (Get)
              └─no──▶ legacy REST /api/datasources
```

### Extend TypedCRUD with an opt-in `secure` block

Introduce a `SecureCarrier` interface, threaded through `TypedCRUD`'s
typed↔unstructured conversion via a runtime `*T` assertion — the same idiom
already used for `ResourceIdentity.SetResourceName` (the `restoreName` path in
`internal/resources/adapter/typed.go`). A resource that implements it round-trips
a top-level `secure` block (write-only values) and `secureFields` (read-back
names). `TypedObject[T]`'s struct is unchanged; resources that do not implement
the interface emit byte-identical output to today. This retires the premise — that
`TypedCRUD` cannot model a `secure` sibling of `spec` — on which the halted spec's
planned TypedCRUD-exception ADR rested.

### Rebuild the app-platform transport on the resources discovery registry + dynamic client

Replace `k8stransport.go` and `servedgroups.go` with a transport built on
`discovery.NewDefaultRegistry` (served-group detection, with the
`~/.cache/gcx/discovery` disk cache the hand-rolled cache deliberately lacked) and
`dynamic.NewDefaultNamespacedClient` (per-plugin GVR CRUD). List enumerates the
served per-plugin GVRs and merges; Get resolves UID→plugin via the discovery-backed
index. The legacy REST `Client` is retained as the fallback mode. Datasources thus
ride the same Kubernetes plumbing as dashboards and folders, and the duplicated
hand-rolled `/apis` CRUD is deleted. (This is the transport rebuild the halted spec
scoped as WS1/WS2; its feasibility was validated there. Here it serves unification
rather than being an end in itself.)

### Route reads via discovery-collapse

At discovery ingestion, fold per-plugin `*.datasource.grafana.app` groups into the
single canonical `datasource.grafana.app/v0alpha1 DataSource` descriptor, scoped
strictly by `IsDatasourceGroup`, so `LookupPreferredPerGroup` returns one canonical
descriptor instead of one per plugin. The router — which now holds a factory for
the canonical GVK because datasources are a first-class registration — routes reads
to the typed datasource adapter. The same ingestion predicate drops the malformed
empty-`Kind` `<type>/v0alpha1` groups that today produce `s.v0alpha1.*` pull
directories, so that bug is fixed as a consequence. Reads become symmetric with
writes without applying `NormalizeGVK` on the read path — which would be messier,
because the read path resolves multiple descriptors per group.

### One shared per-stack transport decision

Served-ness is probed once through the shared discovery registry and cached; both
the `gcx datasources` commands and the resources pipeline obtain the same transport
and branch on the same result. The unification guarantee is that both surfaces use
the same transport and therefore make the same decision for the same inputs.

### Output parity follows `gcx dashboards` (Pattern 13)

All surfaces fetch the canonical typed manifest; codecs control display, not data
acquisition. `-o json` and `-o yaml` are byte-identical across `datasources get`
and `resources get`/`pull`/`push`. `datasources list` keeps curated `table`/`wide`
codecs for the human view, but its `json`/`yaml` output now renders the canonical
manifest rather than today's flat summary.

### RBAC-partial demotion: uniform whole-stack REST fallback (provisional — to be revisited)

When app-platform enumeration is incomplete — any served group returns 403, 404,
or 5xx — the whole listing demotes to the legacy REST `/api/datasources`, applied
identically to every surface. This preserves today's permissive "show me what I can
see" behavior and keeps both surfaces in agreement.

This is provisional and flagged for internal discussion, because:

- it lets a per-*token* RBAC gap flip what is meant to be a per-*stack* decision;
- app-platform-only datasources (visible on the Kubernetes surface but not via REST)
  disappear on both surfaces when demotion triggers;
- it retains 5xx-masking rather than surfacing 5xx as a real error while only
  404 / absent group / zero-served trigger fallback.

The alternative — surface a partial-authorization error and keep the app-platform
view for readable groups, consistent with the rest of the resources pipeline — is
recorded under Alternatives.

## Consequences

- One transport, uniform behavior across every datasource surface; the read defects
  (raw unstructured output, malformed `s.v0alpha1.*` directories, spurious exit-4)
  are resolved.
- `TypedCRUD` gains reusable support for a top-level `secure`/`secureFields` block,
  available to future resources that outgrow a spec-only envelope.
- Less duplicated transport code: the hand-rolled `/apis` CRUD and served-group
  cache are deleted in favor of shared discovery + dynamic-client plumbing.
- **Risk — TypedCRUD core.** Threading `secure` touches machinery shared by all
  typed resources. Non-opting resources must remain byte-identical; lock this with
  golden output tests for dashboards, folders, and other typed resources.
- **Risk — retiring `k8sTransport`.** A well-tested component is removed; its
  coverage (per-plugin enumeration and merge, error mapping, optimistic-concurrency
  on update, the health subresource) must be ported onto the new path before
  deletion.
- **Risk — shared discovery ingestion.** Discovery-collapse changes behavior at a
  site shared by all resources; it must be scoped strictly to `IsDatasourceGroup`
  and covered by tests asserting non-datasource groups are unaffected.
- **User-visible change.** `datasources list -o json|yaml` changes from the flat
  summary to the canonical manifest — acceptable under the parity rule, but a
  documented behavior change.
- **Follow-up:** resolve the RBAC-partial semantics internally (the flagged Option 2
  above).

## Alternatives considered

- **Typed veneer over the existing dual transport (Approach A).** Keep the
  hand-rolled `k8sTransport`; wrap it in the typed registration; fix routing via
  discovery-collapse. Lower risk, but retains the duplicated transport code the
  reuse mandate discourages. Rejected in favor of full pipeline integration.
- **Generalized typed core + normalize-on-read (Approach C).** Generalize
  `TypedObject` to carry arbitrary extra top-level sections, and fix routing
  symmetrically with the write path. Rejected: the core generalization is
  speculative (no second consumer today), and normalize-on-read is messier than
  discovery-collapse (the read path resolves several descriptors per group) and
  does not fix the malformed-group bug.
- **Rewrite `k8sTransport` internals only, per the halted spec.** Keep the adapter,
  routing, and per-call fallback fixed and rebuild only the Kubernetes half.
  Rejected: built on an incorrect model — reads bypass the adapter — so its
  output-parity invariants are unachievable.
- **RBAC-partial Option 1: surface, do not demote.** Treat a forbidden group as a
  partial-authorization condition (report it, keep the app-platform view for
  readable groups), consistent with the rest of the pipeline, and reserve REST
  fallback for "not served at all." This makes the per-stack decision a genuine
  per-stack fact and fixes 5xx-masking. Deferred: Option 2 chosen for now and
  flagged for internal discussion.
- **Per-request fallback with no upfront probe (as in ADR-020).** ADR-020 rejected
  an upfront served-ness probe as racy and an extra round-trip. Not adopted here
  because the cross-surface parity requirement calls for a single shared decision,
  and the `/apis` discovery this design relies on is already probed and cached, so
  the probe is amortized rather than added per command.
