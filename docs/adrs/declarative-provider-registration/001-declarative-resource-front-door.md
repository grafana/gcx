# Declarative `adapter.Resource[T]` + `adapter.NewProvider` registration front door

**Created**: 2026-07-07
**Status**: accepted
**Supersedes**: none

## Context

ADR-008 (`docs/adrs/typed-resource-adapter-compliance/001-typed-resource-adapter-foundation.md`,
now `accepted`) gave every provider a shared `TypedCRUD[T]` service layer and a
single `Provider.TypedRegistrations()` method that `providers.Register()`
auto-consumes to populate both the CLI registry and the `resources` adapter
registry atomically. It did not, however, prescribe how a provider *builds*
the `adapter.Registration` values that method returns — and three divergent
idioms grew up around that gap, verified against the tree just before this
change:

1. **SLO** hand-built an erased `adapter.Registration{}` literal plus a
   **duplicate** `TypedCRUD[Slo]` literal across factory functions in
   `internal/providers/slo/definitions/resource_adapter.go` (182 lines, two
   `//nolint:dupl` directives).
2. **OnCall** re-invented a **private** generic helper —
   `buildRegistration[T]` + `oncallMeta()` + `withCreate`/`withUpdate`/`withDelete`
   over `crudOption[T]` (`internal/providers/irm/oncall_adapter.go`) — to
   register its 17 types (see ADR-010, "Table-driven TypedCRUD[T] for OnCall
   Adapter").
3. **A generic front door already existed and nobody used it.** The dead
   singular `TypedRegistration[T]` struct and its `ToRegistration()` method
   were referenced only by `typed_test.go` and stale doc comments — no
   provider called them.

In every hand-built registration, `Schema` (always `SchemaFromType[T](desc)`)
and `GVK` (always `desc.GroupVersionKind()`) were relayed by the provider
rather than derived, and `TypedCRUD.AsAdapter()` deliberately returned `nil`
from `Schema()`/`Example()` as a consequence — a latent footgun for any
caller expecting a complete adapter. The provider surface was still growing
(SLO, synth, IRM, k6, fleet, faro, appo11y, aio11y, kg, dashboards,
instrumentation); every new resource type paid the duplication, relay, and
footgun cost, and the `/add-provider` skill plus `docs/reference/provider-guide.md`
taught the boilerplate as required.

This ADR extends ADR-008: it keeps `TypedCRUD[T]`, `ResourceIdentity`, and
the live `TypedRegistrations()` wiring unchanged, and adds a declarative
layer on top of them. Full analysis: `docs/specs/feature-provider-registration-simplification/spec.md`.

## Decision

We consolidated registration in two sequenced steps, then migrated SLO
(definitions) onto the result as the reference implementation.

### Step 1 — pure consolidation (`internal/resources/adapter/builder.go`)

OnCall's private `buildRegistration[T]` was lifted into the `adapter`
package as `BuildRegistration[T, C]`, with `RegistrationMeta`,
`CRUDOption[T, C]`, and `WithCreate`/`WithUpdate`/`WithDelete`, constrained
by `adapter.ResourceNamer`. OnCall now routes its 17 registrations through
this lifted, public builder; its private copy is deleted. The dead singular
`TypedRegistration[T]`/`ToRegistration()` and the stale doc-comment
references to it are deleted. SLO's duplicated `TypedCRUD[Slo]` literal is
collapsed to one construction path, removing both `//nolint:dupl`
directives. `BuildRegistration` auto-derives `Schema` and `GVK` from the
type parameter and `Descriptor`, so no caller hand-threads them.

This step alone is a near-pure refactor and was proven independently
shippable (`GCX_AGENT_MODE=false mise run all` green with no
`adapter.Resource[T]` yet in the tree).

### Step 2 — declarative model (`internal/resources/adapter/resource.go`, `capability.go`, `provider.go`)

For providers whose types share one client-construction shape (the common
case), a second, more declarative front door sits on top of Step 1:

```go
type Resource[T ResourceNamer] struct {
    Group, Version, Kind string
    Singular, Plural      string  // override hooks; Plural covers irregular plurals
    Namespace             string  // override hook; defaults to ClientDeps.Namespace
    StripFields           []string
    NaturalKey            string          // folds in RegisterNaturalKey — no separate init()
    URLTemplate           string          // folds in deeplink registration — no separate init()
    Example               T               // typed value; wrapped + marshaled automatically
    NewClient             func(ctx context.Context, deps ClientDeps) (any, error)
}
```

- `ClientDeps{HTTP, BaseURL, Namespace}` carries a pre-built, fully-configured
  `*http.Client` (logging, retry, `--insecure-log-http-payload`, timeouts,
  auth mode); `NewClient` must reuse it and must never construct competing
  transport.
- `DepsLoader func(ctx) (ClientDeps, error)` is a caller-supplied closure
  passed into `NewProvider` — `adapter` cannot import `internal/providers`
  (the reverse import already exists), so every provider supplies its own
  loader (mirroring `BuildRegistration`'s `loadClient` parameter) instead of
  `adapter` loading config itself.
- Six capability interfaces — `Lister[T]`, `Getter[T]`, `Creator[T]`,
  `Updater[T]`, `Deleter[T]`, `Validator[T]` — express which CRUD verbs a
  client supports. `NewClient`'s return value is type-asserted against them
  at **exactly one** audited seam, `newCapabilityCRUD[T]` in
  `internal/resources/adapter/capability.go`: an unimplemented capability
  leaves its `TypedCRUD[T]` `Fn` field nil, which already resolves to
  `errors.ErrUnsupported` (or, for `Validator[T]`, a no-op dry-run) — no nil-Fn
  plumbing and no flags in provider code.
- `Resource[T].registration(loadDeps)` derives `Schema` (`SchemaFromType[T]`),
  `GVK`, `Singular`/`Plural` (naive English pluralization of a lower-cased
  `Kind`, overridable), and `Namespace` (from `ClientDeps`, overridable).
  Declaring `NaturalKey` folds in `RegisterNaturalKey`; declaring
  `URLTemplate` folds into the `Registration` that `providers.Register()`
  already threads through `deeplink.RegisterPattern` — neither needs a
  separate `init()`.
- `NewProvider(name, shortDesc string, loadDeps DepsLoader, resources ...Declaration) *Provider`
  builds a concrete `*adapter.Provider` from one or more `Resource[T]`
  values (`Declaration` is the type-erased façade `Resource[T]` satisfies,
  letting one variadic call mix different `T`s). The returned `*Provider`
  structurally satisfies `providers.Provider` — `Name`/`ShortDesc` from the
  constructor args, `ConfigKeys()` nil and `Validate()` a no-op by default,
  `TypedRegistrations()` built from the declared resources, and
  `Commands()` from `WithCommands(existingCmds...)`. `NewProvider` does
  **not** auto-generate standard CRUD command verbs — providers keep their
  hand-written command tree and attach it via `WithCommands`; auto-building
  `list`/`get`/`push`/`pull`/`delete` verbs is a named follow-up spec, out of
  scope here.

### SLO reference migration

`internal/providers/slo/definitions/resource_adapter.go` (182 lines) is
replaced by `SloResource() adapter.Resource[Slo]` — one declarative value
naming `Group`/`Version`/`Kind`, `NaturalKey: "name"`,
`URLTemplate: "/a/grafana-slo-app/slo/{name}"`, a typed `Example: Slo{...}`,
and `NewClient: newAdapterClient`. `internal/providers/slo/provider.go`
builds the provider with
`adapter.NewProvider("slo", shortDesc, loadSLODeps, definitions.SloResource()).WithCommands(sloCmd)`.
SLO's bespoke create-then-refetch behavior (the create endpoint returns only
a UUID) moved verbatim from the old registration-side closure into
`Client.Create`/`Client.Update` in `client.go` — it is now the `Creator[Slo]`/
`Updater[Slo]` implementation, not registration plumbing.
`definitions/commands.go` (449 lines, the hand-written `gcx slo definitions`
command tree) is untouched and attached via the same `WithCommands` hook, so
both `gcx slo definitions …` and `gcx resources … slos` front doors resolve
through the identical `Client` methods.

### Type-erasure containment

Detecting which capability interfaces a `NewClient` result satisfies
intrinsically requires a type assertion on the `any` it returns — there is
no generics-only way to do this within the `TypedCRUD[T]` model (maintainer
ruling, 2026-07-07, recorded in the spec's Open Questions). This is a
**documented, audited exception** to `docs/architecture/patterns.md` §16's
"no `any` type erasure" rule, confined to the single seam described above:
grep for `.(Lister[`, `.(Getter[`, `.(Creator[`, `.(Updater[`, `.(Deleter[`,
`.(Validator[` and all six occur only in `capability.go`. No provider
package performs this assertion; no second seam is introduced anywhere
else. See patterns.md §16, "Sanctioned Exception — Single-Seam Capability
Assertion", for the full ruling and grep-verifiable boundary.

## Consequences

### Positive

- SLO's registration collapsed from a 182-line hand-built `Registration{}`
  (with two `//nolint:dupl` literals) to a single ~50-line declarative
  `Resource[Slo]` value with no `//nolint` directives.
- Schema, GVK, plural, and namespace are derived, not hand-threaded — the
  relay smell and the `AsAdapter()` nil-schema footgun are both eliminated
  for types registered through `Resource[T]`.
- Read-only and singleton types need no nil-`Fn` plumbing or provider-side
  flags: a client that implements only `Lister[T]`/`Getter[T]` gets
  `Create`/`Update`/`Delete` as `errors.ErrUnsupported` for free.
- `NaturalKey` and `URLTemplate` fold in registration that previously needed
  a separate `init()`.
- Dead code (`TypedRegistration[T]`, `ToRegistration()`) is removed; OnCall's
  private builder duplicate is removed in favor of the shared, public
  `BuildRegistration`.
- The one necessary `any`-assertion seam is centralized, grep-verifiable,
  and documented — not spread across provider packages.

### Negative / follow-up work

- **Command auto-build is deferred.** `Resource[T]` only reduces the
  registration side (SLO's `resource_adapter.go`, 182 → ~50 lines); the
  hand-written `commands.go` (449 lines) is untouched. The full ~540 → ~50
  line reduction this design eventually targets requires a standard-verb
  command factory, which is a separate, named follow-up spec.
- **Only SLO (definitions) is migrated.** OnCall, synth, IRM, k6, fleet,
  faro, appo11y, aio11y, kg, dashboards, and instrumentation keep their
  existing registration idiom (mostly `BuildRegistration` after Step 1, or
  hand-built literals) and converge onto `Resource[T]` opportunistically, in
  out-of-scope work. Until that convergence, the provider surface carries
  two live front doors — `BuildRegistration` for multi-type/heterogeneous
  providers, `Resource[T]` for the common single-client-shape case — which
  is an intentional, not accidental, divergence (see Alternatives below).
- **Naive plural derivation.** `derivePlural` handles common English suffix
  rules (`y`→`ies`, `s`/`x`/`z`/`ch`/`sh`→`es`, else `+s`) but mishandles true
  irregulars; those types must set `Resource.Plural` explicitly.
- **Numeric-ID identity is not covered yet.** An embeddable slug-id identity
  helper (composing a name via `internal/resources/adapter/slug.go`) is
  deferred until a numeric-ID provider (Fleet/Synth-shaped) actually
  migrates and defines the requirement.

## Alternatives considered

- **Reflective / struct-tag GVK model (Option C).** Rejected: trades
  accidental complexity for essential complexity and would introduce
  reflection-based dispatch and runtime panics, violating §16 more broadly
  than the single sanctioned seam this ADR ratifies.
- **One fat `CRUDClient[T]` interface requiring every verb.** Rejected in
  favor of six narrow capability interfaces (`Lister`/`Getter`/`Creator`/
  `Updater`/`Deleter`/`Validator`) — a fat interface would force read-only
  and singleton types to write stub methods or thread extra flags, exactly
  the boilerplate this design removes.
- **Auto-building standard CRUD commands as part of this change.** Deferred
  to a separate follow-up spec to keep this change registration-scoped and
  lower-risk; `NewProvider` explicitly documents that it does not generate
  command verbs.
- **Big-bang migration of every provider onto `Resource[T]`.** Rejected:
  SLO alone proves the model without a large blast radius; other providers
  (especially multi-type ones like OnCall, already well-served by
  `BuildRegistration`) converge opportunistically.
