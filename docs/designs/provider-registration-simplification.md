---
type: design
title: "Provider Registration — Declarative Resource[T] Scaffolding Reduction"
created: 2026-06-30
status: proposed
bead: none
---

# Provider Registration — Declarative Resource[T] Scaffolding Reduction

**Created**: 2026-06-30
**Status**: proposed
**Supersedes**: none

> **Implementation note (PR #931):** `Columns`/`adapter.Cols[T]` and the
> embeddable `adapter.Named`/`IDNamed` identity helpers were deferred at
> implementation time — nothing consumed them yet (table rendering is a
> follow-up spec, and no numeric-ID provider has migrated). Examples below
> that show them predate that deferral; domain types hand-write
> `GetResourceName`/`SetResourceName` for now.

## Context

Adding a new provider resource type to gcx requires a large amount of
mechanical scaffolding that is mostly derivable or duplicated. A maintainer
asked whether this accidental complexity can be removed. Investigation of the
live code (SLO, OnCall, IRM, App O11y) found three distinct problems:

**1. Three different ways to register one type — and the cleanest is dead code.**

```
1. SLO    → hand-builds an erased adapter.Registration{} literal + NewLazyFactory()
            AND a DUPLICATE TypedCRUD literal in NewFactoryFromConfig()   (//nolint:dupl ×2)
2. OnCall → a PRIVATE generic buildRegistration[T] + oncallMeta()
            + withCreate/withUpdate/withDelete functional options          (//nolint:dupl,maintidx)
3. adapter.TypedRegistration[T].ToRegistration()
          → the generic front door that ALREADY EXISTS in typed.go...
            ...and is used by NO provider (grep: only defined in the adapter package, never called)
```

OnCall re-invented, privately, the generic helper the codebase already ships and
nobody uses. SLO hand-rolls it and pays the duplication tax (the file carries two
`//nolint:dupl` directives on the repeated `TypedCRUD[Slo]` literal). This is the
core smell: one job, three implementations.

**2. The relay and the nil-schema footgun.** A provider's `TypedRegistrations()`
re-threads values the type package already knows — `Descriptor`, `GVK`
(literally `desc.GroupVersionKind()`), `Schema` (always
`adapter.SchemaFromType[T](desc)`), `Example`. `adapter.TypedCRUD.AsAdapter()`
deliberately returns `nil` from `Schema()`/`Example()` (typed.go) *because*
schema/example are carried by the hand-built `Registration` instead of derived —
a latent footgun for anyone who calls `AsAdapter()` expecting a complete adapter.

**3. Per-type CRUD command shells are hand-written re-implementations of the
pipeline.** SLO's `definitions/commands.go` is ~450 lines; ~280 of them
(`list`/`get`/`push`/`pull`/`delete`) duplicate verbs the generic `resources`
pipeline already runs through the *same* adapter. The only parts that earn their
place are the custom table codec (column choice is taste, not derivable) and the
extension verbs `status`/`timeline` (Prometheus-backed, no pipeline equivalent).
OnCall is evidence this boilerplate is optional: it registers 17 types as
adapters (each gets the full `resources` pipeline for free) and layers only a
*curated* command set on top.

**Why now.** The provider surface is growing (SLO, synth, IRM, k6, fleet, faro,
appo11y, aio11y, kg, dashboards, instrumentation, …). Every new type pays this
tax, and `/add-provider` + `provider-guide.md` teach the boilerplate as
required. Reducing it compounds across every future provider.

**Framing constraints (established facts, not options):**
- The dual-access invariant is permanent: one `ResourceAdapter` powers both
  `gcx <provider> …` commands and the generic `gcx resources …` pipeline.
  (`ARCHITECTURE.md` § Provider System; `patterns.md` §16 "Output Consistency".)
- `patterns.md` §16 forbids "new serialization bridges, dispatch patterns, or
  type-erasure mechanisms" and treats the provider pattern as semi-invariant.
  Any change must be **consolidation within TypedCRUD**, not a new bridge.
- The documented long-term trajectory is typed resources that satisfy K8s
  interfaces directly, *eliminating* the TypedCRUD bridge. This design is a
  stepping stone toward that, not a competing direction.
- **Prior art:** ADR-008 ("TypedResourceAdapter[T] with ResourceIdentity and
  Provider Command Migration"). Its status field read `proposed`, but the
  foundation it specifies is **already implemented** in the codebase: the
  `ResourceIdentity` interface (`internal/resources/adapter/identity.go`), the
  `TypedObject[T]` envelope and typed `TypedCRUD[T]` methods (`typed.go`), and
  unified registration where `providers.Register()` auto-wires
  `TypedRegistrations()` (`registry.go`) — ADR-008's `Registrations()` decision,
  shipped under a slightly different name (`TypedRegistrations`) and constraint
  (`ResourceNamer`, the value-safe subset of `ResourceIdentity`). That `proposed`
  status was stale drift; it is corrected to `accepted` as part of this work.
  This design **builds on that implemented foundation** and carries forward
  ADR-008's own explicitly-deferred follow-ups — registration ergonomics and
  "deprecate hand-written provider CRUD commands in favour of the pipeline."

## Goals

1. Reduce per-type registration + CRUD-command scaffolding from ~540 lines
   (SLO baseline) to the irreducible floor (~50), leaving only: the domain
   struct, the REST client, one example manifest, and optional table columns.
2. Collapse the three registration paths into **one** front door.
3. Auto-derive everything derivable: `Schema` from the Go type, `GVK` from the
   descriptor, `Singular`/`Plural` from `Kind`, namespace from the loaded config.
4. Express CRUD capability by **interface satisfaction**, so read-only and
   singleton resources need no nil-plumbing or flags.
5. Make the standard `list`/`get`/`push`/`pull`/`delete` command tree
   auto-buildable from a resource declaration, while staying opt-in so curated
   providers (OnCall) keep their bespoke surface.
6. Eliminate the `AsAdapter()` nil-schema footgun.
7. Stay strictly inside the TypedCRUD model — no new serialization bridge.

## Non-Goals

- Not in scope: retiring TypedCRUD in favour of K8s-native typed resources
  (the documented long arc — this design is a stepping stone, not that work).
- Not in scope: changing the `resources` discovery/push/pull pipeline itself.
- Not in scope: forcing command auto-generation onto providers that deliberately
  curate their command surface (OnCall, IRM incidents).
- Not in scope: a reflective / struct-tag GVK model (Option C — rejected below;
  it trades accidental complexity for essential complexity and violates §16).
- Not in scope: migrating the datasource/signal-provider tier (`metrics`,
  `logs`, `traces`, `profiles`) — they are not adapter-backed CRUD resources.

## Decisions

| Decision | Chosen | Rationale | Alternatives Considered |
|----------|--------|-----------|------------------------|
| Registration model | **Option B — declarative `adapter.Resource[T]` + `adapter.NewProvider(...)`** | One front door; derives schema/GVK/plural/namespace; smallest provider surface; stays in TypedCRUD | A (consolidated builder — keeps explicit closures); C (reflective/struct-tag — rejected) |
| CRUD wiring | **Capability interfaces** (`Lister[T]`/`Getter[T]`/`Creator[T]`/`Updater[T]`/`Deleter[T]`), type-asserted at registration | Maps TypedCRUD's existing "nil Fn ⇒ unsupported" semantics onto interface satisfaction; read-only/singleton fall out for free | One fat `CRUDClient[T]` interface (forces stubs); explicit closures (today's boilerplate) |
| Schema / GVK / plural | **Derived** (`SchemaFromType[T]`, `Descriptor.GroupVersionKind()`, `strcase` from `Kind`) with override hooks | These are pure functions of the type/descriptor; threading them by hand is the relay smell | Hand-threaded (status quo) |
| Commands | **Auto-built standard verbs + opt-in extras** via `adapter.NewProvider(..., WithCommands(...))` | Reclaims SLO's ~280 mechanical lines; opt-in preserves OnCall's curated surface | Mandatory auto-gen (breaks curated providers); always hand-written (status quo) |
| Identity boilerplate | **Embeddable `adapter.Named` (string) / `adapter.IDNamed` (numeric slug)** | Removes the repeated `GetResourceName`/`SetResourceName` pair for the common cases; numeric path reuses existing `slug.go` | Hand-written identity methods per type |
| Type-erasure containment | **Single audited assertion seam** inside the `adapter` helper | §16 forbids spreading dispatch/erasure; one centralized seam is a known, reviewable exception | Per-provider assertions (spreads erasure — rejected) |
| Rollout | **A first (pure consolidation), then B** | A is a near-pure refactor proven by OnCall; B layers declarative model + capability interfaces + command auto-build on the consolidated base | Big-bang B (higher risk, no intermediate safe point) |

## Design

### The provider author's end-state (Option B)

Everything about one type lives in one file. Foo, fully:

```go
package myprovider

// 1. Domain type. Embed adapter.Named → ResourceIdentity for free (string-name case).
type Foo struct {
	adapter.Named        // Name string + Get/SetResourceName; serialized as spec.name
	Description string `json:"description"`
	Threshold  int    `json:"threshold"`
	Status     string `json:"status,omitempty"` // server-managed; stripped on output
}

// 2. REST client. Implements ONLY the verbs the API supports.
//    Create/Update return the PERSISTED object — the refetch-after-write that
//    SLO hand-wires in a closure lives HERE, where it belongs.
type fooClient struct{ http *http.Client; base string }

func (c *fooClient) List(ctx context.Context, opts adapter.ListOptions) ([]Foo, error) { /* ... */ }
func (c *fooClient) Get(ctx context.Context, name string) (*Foo, error)       { /* ... */ }
func (c *fooClient) Create(ctx context.Context, f *Foo) (*Foo, error)         { /* ... */ }
func (c *fooClient) Update(ctx context.Context, n string, f *Foo) (*Foo, error) { /* ... */ }
func (c *fooClient) Delete(ctx context.Context, name string) error            { /* ... */ }
// Optional: implement Validate(ctx, []*Foo) to get real server-side dry-run.

// 3. Declarative registration. No GVK, no schema, no factory boilerplate,
//    no natural-key init(), no deeplink init() — all derived or folded in.
var FooResource = adapter.Resource[Foo]{
	Group:       "myprovider.ext.grafana.app",
	Version:     "v1alpha1",
	Kind:        "Foo",            // Singular "foo" / Plural "foos" derived via strcase
	StripFields: []string{"status"},
	NaturalKey:  "name",           // folds RegisterNaturalKey(...) in
	URLTemplate: "/a/myprovider-app/foos/{name}", // folds the deeplink init() in
	Example:     Foo{Named: adapter.Named{Name: "my-foo"}, Threshold: 90},
	Columns: adapter.Cols[Foo]{   // optional; omit → generic name/namespace/age table
		{"NAME", func(f Foo) string { return f.Name }},
		{"THRESHOLD", func(f Foo) string { return strconv.Itoa(f.Threshold) }},
	},
	// deps.HTTP is a PRE-CONFIGURED *http.Client — LoggingRoundTripper, retry,
	// --insecure-log-http-payload, timeouts, and the correct auth mode already
	// wired by the framework (httputils.NewDefaultClient / cloudCfg.HTTPClient,
	// per provider-guide.md Step 4b). Providers never hand-roll transport and
	// cannot forget the middleware stack.
	NewClient: func(ctx context.Context, deps adapter.ClientDeps) (any, error) {
		return &fooClient{http: deps.HTTP, base: deps.BaseURL}, nil
	},
}
```

The entire provider shell:

```go
package myprovider

func init() { providers.Register(myprovider()) } //nolint:gochecknoinits

func myprovider() providers.Provider {
	return adapter.NewProvider("myprovider", "Manage Foo and Bar resources.",
		FooResource, BarResource)
}
```

No `resource_adapter.go`, no `commands.go`, no hand-built `Registration`, no
`AsAdapter()` call.

### What the framework derives

```
adapter.Resource[Foo] ──▶ Schema  = SchemaFromType[Foo]()              (auto)
                          GVK     = Group/Version/Kind                 (auto)
                          Singular/Plural from Kind via strcase        (auto, override-able)
                          Namespace threaded from the loaded config    (auto)
                          NewClient invoked with adapter.ClientDeps{    (auto)
                              HTTP:      pre-built *http.Client (logging/retry/auth mode)
                              BaseURL, Namespace }
                          TypedCRUD wired by type-asserting NewClient's return:
                              v.(Lister[Foo])    → ListFn(ctx, limit) else nil ⇒ ErrUnsupported
                              v.(Getter[Foo])    → GetFn     else nil ⇒ ErrUnsupported
                              v.(Creator[Foo])   → CreateFn  else nil ⇒ ErrUnsupported
                              v.(Updater[Foo])   → UpdateFn ...
                              v.(Deleter[Foo])   → DeleteFn ...
                              v.(Validator[Foo]) → ValidateFn (dry-run path) else no-op
adapter.NewProvider(..) ─▶ Name / ShortDesc; TypedRegistrations() from the resources;
                           ConfigKeys()/Validate() defaulting to none;
                           auto-built commands: gcx myprovider foos list|get|push|pull|delete
                             with --limit (list) and --dry-run (push/delete) wired in
                           extension verbs via NewProvider(..., adapter.WithCommands(statusCmd))
```

`adapter.ClientDeps` (the argument to every `NewClient`) is how the reviewer's
"reuse the pre-configured client" requirement is enforced structurally:

```go
type ClientDeps struct {
	HTTP      *http.Client // pre-built: logging, retry, payload-dump, timeouts, auth mode
	BaseURL   string
	Namespace string
}
```

A **read-only** type implements only `List`/`Get`; a **singleton**
(appo11y-style) implements only `Get`/`Update`. The missing interfaces *are* the
"unsupported" signal — no flags, no nil-plumbing.

### Capability interfaces

```go
type Lister[T any]    interface { List(ctx context.Context, opts adapter.ListOptions) ([]T, error) }
type Getter[T any]    interface { Get(ctx context.Context, name string) (*T, error) }
type Creator[T any]   interface { Create(ctx context.Context, item *T) (*T, error) }
type Updater[T any]   interface { Update(ctx context.Context, name string, item *T) (*T, error) }
type Deleter[T any]   interface { Delete(ctx context.Context, name string) error }

// Optional. When the client implements it, the pipeline calls Validate for
// `push --dry-run` and `resources validate` instead of Create/Update — mirroring
// TypedCRUD.ValidateFn + the adapter's metav1.DryRunAll handling in typed.go.
type Validator[T any] interface { Validate(ctx context.Context, items []*T) error }
```

`adapter.Resource[T].register()` type-asserts the `NewClient` return value
against each, building the `TypedCRUD[T]` fields accordingly. **This is the one
place that performs a type assertion** — the §16-sensitive seam is contained,
audited, and not duplicated across providers.

**Generic operation options.** Cross-cutting knobs are carried by option structs,
not baked into per-verb signatures, so they extend without breaking clients:

- **List:** `adapter.ListOptions{ Limit int64 /* room for label/field filters */ }`
  — maps onto the existing `TypedCRUD.ListFn(ctx, limit)` and
  `metav1.ListOptions.Limit`, and onto the auto-built command's `--limit` flag.
- **Dry-run (mutating verbs):** owned by the pipeline, not the client. The
  auto-built `push`/`delete` commands and generic `resources push --dry-run`
  set `metav1.DryRunAll`; the adapter routes that to `Validator[T]` (if present)
  and skips the real mutation — so `--dry-run` works for every resource without
  per-provider code, and providers add real *server-side* validation only by
  implementing `Validator[T]`.

### Alternatives considered (the in-session brainstorm set)

- **Option A — consolidated builder.** Promote OnCall's private
  `buildRegistration[T]` + `oncallMeta()` + `withCreate/withUpdate/withDelete`
  into the `adapter` package; auto-derive schema/GVK. Author still writes a
  factory and explicit CRUD closures, but duplication, relay, and the nil-schema
  footgun vanish. Lowest risk, near-pure refactor (OnCall already proves the
  shape). **Chosen as rollout step 1.**
- **Option C — fully reflective / single-call.** GVK on a struct tag, reflect
  the client, `adapter.Scan("myprovider", &fooClient{}, &barClient{})`. Shortest
  on paper but trades accidental complexity for essential complexity: stringly
  GVK loses compile-time checks, reflection-based dispatch is exactly the
  "new dispatch / type-erasure" §16 forbids, and wiring failures become runtime
  panics. **Rejected** — included to mark where the line is.

### Rollout

1. **Step 1 (Option A — pure consolidation).** Lift OnCall's builder into
   `adapter`; route SLO + new providers through it; delete SLO's duplicated
   `NewTypedCRUD`/`NewFactoryFromConfig` literals and the unused
   `TypedRegistration[T]` (or fold it into the new builder). Shippable alone.
2. **Step 2 (Option B).** Add `adapter.Resource[T]`, `adapter.ClientDeps`,
   `adapter.ListOptions`, `adapter.Cols[T]`, the capability interfaces
   (`Lister`/`Getter`/`Creator`/`Updater`/`Deleter`/`Validator`),
   `adapter.Named`/`IDNamed`, and `adapter.NewProvider` (with `WithCommands`).
   Migrate one provider (SLO) as the reference, then others opportunistically.
   New providers start here.
3. Update `/add-provider`, `provider-guide.md`, and `provider-checklist.md` to
   teach the declarative model.

## Consequences

**Easier:**
- A new provider type collapses from ~540 lines of scaffolding to ~50; the rest
  is the irreducible REST client + domain struct + example.
- One registration path; the dead `TypedRegistration[T]` and the duplicated
  literals disappear.
- Read-only and singleton resources need no special handling.
- The `AsAdapter()` nil-schema footgun is removed (schema is derived, not relayed).
- `gcx resources schemas`/`examples` coverage is automatic for every new type.

**Harder / risks:**
- The `NewClient → any → capability-assertion` seam reintroduces a small,
  contained amount of type-erasure. Must live in exactly one audited helper or it
  becomes the §16 anti-pattern it's meant to avoid.
- Auto-derived `Plural` via `strcase` will mis-handle irregular plurals; the
  override hook must be ergonomic and documented.
- Migrating existing providers is incremental but non-trivial (each has bespoke
  closures, e.g. SLO's create-then-refetch — which moves into the client method).
- Changes a documented pattern (`patterns.md` §16) and the `/add-provider` skill —
  requires an ADR and doc updates, not just code.

**Follow-up work:**
- Reconcile with ADR-008 (extend vs supersede).
- Decide migration scope (all providers vs new-only).

## Open Questions

- [ ] Does the contained `any`-assertion seam satisfy `patterns.md` §16's
  "no type-erasure" rule, or does it need an explicit documented exception?
  — maintainer ruling, before Step 2.
- [ ] ADR relationship: ADR-008's foundation is implemented (status corrected
  `proposed` → `accepted` in this work), so this design **extends** it rather than
  reopening it — confirm that framing when promoting this design to its own ADR,
  and decide whether the ADR-008 follow-up list should be closed out or migrated
  here.
- [ ] Numeric-ID types: is `adapter.IDNamed` (wrapping `slug.go`) sufficient, or do
  slug-id providers (Fleet, Synth) need a richer identity hook? — confirm against
  Fleet/Synth before migrating them.
- [ ] Command auto-gen vs file semantics: can a generic `push`/`pull` factory
  faithfully reproduce per-provider upsert nuances (e.g. SLO's "empty UUID ⇒
  create")? — spike during Step 2.
- [ ] Migration scope: migrate all existing providers, or only require the new
  model for new providers and let existing ones converge opportunistically?
