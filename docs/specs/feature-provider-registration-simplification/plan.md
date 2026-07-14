---
type: feature-plan
title: "Provider Registration Simplification — Declarative adapter.Resource[T] Front Door"
status: draft
spec: docs/specs/feature-provider-registration-simplification/spec.md
created: 2026-07-07
---

# Plan — Provider Registration Simplification

## Pipeline Architecture

How a declared resource flows from one `adapter.Resource[T]` value to the dual-access surface. The single type-erasure seam is called out explicitly.

```
adapter.Resource[Foo]  (one declarative value — the whole provider-type surface)
  │   Group / Version / Kind, StripFields, NaturalKey,
  │   URLTemplate, Example, Columns (adapter.Cols[Foo]), NewClient
  ▼
adapter.NewProvider("foo", "Manage Foo…", FooResource, …)  ── WithCommands(existingCmds) [FR-018]
  │
  ├─ DERIVE ─▶ Schema     = SchemaFromType[Foo]()               [FR-013]
  │            GVK        = Group / Version / Kind              [FR-013]
  │            Singular/Plural = strcase(Kind)  (override-able) [FR-013]
  │            Namespace  = loaded config                       [FR-013]
  │            NaturalKey ⇒ RegisterNaturalKey(...)             [FR-015]
  │            URLTemplate ⇒ deeplink registration             [FR-015]
  │
  ├─ NewClient(ctx, ClientDeps{ HTTP, BaseURL, Namespace }) ─▶ any   [FR-007]
  │        HTTP = PRE-BUILT client (logging / retry / payload-dump / auth mode)
  │        provider never hand-rolls transport                       [NC-007]
  │
  ▼
┌──────────────────────────────────────────────────────────────────┐
│  SINGLE AUDITED ASSERTION SEAM  — one helper in internal/resources/ │  [FR-014]
│  adapter/ ; the ONLY `any`→interface assertion in the codebase.     │  [§16 exception, FR-024]
│    v.(Lister[Foo])    → ListFn(ctx, ListOptions.Limit)  else ErrUnsupported
│    v.(Getter[Foo])    → GetFn                           else ErrUnsupported
│    v.(Creator[Foo])   → CreateFn                        else ErrUnsupported
│    v.(Updater[Foo])   → UpdateFn                        else ErrUnsupported
│    v.(Deleter[Foo])   → DeleteFn                        else ErrUnsupported
│    v.(Validator[Foo]) → ValidateFn (dry-run path)       else no-op        [FR-010]
└──────────────────────────────────────────────────────────────────┘
  │
  ▼
TypedCRUD[Foo] fields  ─▶  adapter.Registration
  │
  ▼
Provider.TypedRegistrations() []Registration   ── LIVE method, preserved  [FR-017, NC-006]
  │
  ▼
providers.Register()  →  registry.go wiring   (UNCHANGED)               [FR-017, NC-005]
  │
  ├─────────────▶  gcx <provider> …   commands  (hand-written, attached via WithCommands)
  └─────────────▶  gcx resources …    pipeline  (get / schemas / examples / push / pull / delete)
        DUAL-ACCESS INVARIANT — one ResourceAdapter, two front doors    [AC-001]
```

## Design Decisions

| Decision | Rationale | Traces to |
|----------|-----------|-----------|
| Consolidated builder lives in the `adapter` package, lifted from OnCall | Single front door; OnCall already proves the shape; removes the relay and the `AsAdapter()` nil-schema footgun | FR-001, FR-002, FR-005, FR-016 |
| CRUD wired by capability interfaces, not one fat `CRUDClient[T]` | Maps TypedCRUD's "nil Fn ⇒ unsupported" onto interface satisfaction; read-only and singleton types need no stubs or flags | FR-010, AC-003 |
| Schema / GVK / plural / namespace derived, with override hooks | They are pure functions of the type/descriptor; hand-threading is the relay smell; the `Plural` override covers irregular plurals | FR-013, AC-011 |
| Type-erasure confined to one audited seam in `adapter` | §16 forbids spreading dispatch/erasure; one seam is reviewable and can carry a documented, ratified exception | FR-014, FR-024, AC-005, AC-018 |
| Sequenced A → B rollout | Step 1 (consolidation) is a near-pure refactor with an intermediate shippable safe point; Step 2 layers the declarative model on top | FR-001–FR-005 then FR-006–FR-018 |
| `ClientDeps.HTTP` carries the pre-built transport | Structurally prevents providers from hand-rolling transport or forgetting the logging/retry/auth middleware stack | FR-007, NC-007, AC-010 |
| Embeddable `adapter.Named` / `adapter.IDNamed` | Removes the repeated `GetResourceName`/`SetResourceName` pair; numeric path reuses existing `slug.go` | FR-011 |
| Commands stay hand-written; auto-build deferred | User scoping — this spec is registration-side only; SLO's `commands.go` is untouched and attached via `WithCommands` | FR-018, FR-021, NC-003, NC-004 |
| SLO (definitions) as the sole reference migration | Prove the model on one provider without a big-bang migration; other providers converge opportunistically out of scope | FR-019, FR-020, NC-008 |
| Live `TypedRegistrations()` + `registry.go` wiring preserved verbatim | Dual-access invariant and the auto-consumed front door must not regress; the naming collision with the dead singular `TypedRegistration[T]` must not cause the live plural method to be touched | FR-017, NC-006 |

## Compatibility

**Continues working unchanged:**

- Every existing provider (OnCall, synth, IRM, k6, fleet, faro, appo11y, aio11y, kg, dashboards, instrumentation) — registrations and commands.
- The `resources` discovery / push / pull pipeline.
- Every `gcx <provider>` command surface.
- SLO's `definitions/commands.go` (449 lines) and its command UX.
- The live `TypedRegistrations()` provider-interface method and `providers.Register()` / `registry.go` wiring.

**Deprecated / removed for new work:**

- The three divergent registration idioms: SLO's hand-built erased `Registration{}` literal, OnCall's private `buildRegistration[T]`, and the dead singular `TypedRegistration[T].ToRegistration()` (deleted or folded into the consolidated builder).
- `AsAdapter()`'s nil-schema behavior (footgun removed; schema/example derived).
- Hand-threading `Schema` / `GVK` / `Example` into `TypedRegistrations()`.

**Newly available:**

- `adapter.Resource[T]`, `adapter.NewProvider`, `adapter.ClientDeps`, `adapter.ListOptions`, `adapter.Cols[T]`.
- Capability interfaces `Lister[T]` / `Getter[T]` / `Creator[T]` / `Updater[T]` / `Deleter[T]` / `Validator[T]`.
- Embeddable identity `adapter.Named` / `adapter.IDNamed`.
- Auto-derived schema / GVK / plural / namespace with override hooks, plus folded-in `NaturalKey` and `URLTemplate` registration.

## Rollout / Sequencing

```
Step 1  (Option A — pure consolidation)  ── SHIPPABLE ALONE ──▶  Step 2 GATE ──▶  Step 2 (Option B — declarative)
```

- **Step 1 (shippable milestone):** lift OnCall's builder into the `adapter` package; route OnCall's registrations through it and remove OnCall's private copy; delete the dead singular `TypedRegistration[T]` / `ToRegistration()`; dedupe SLO's duplicated `TypedCRUD[Slo]` literals so both `//nolint:dupl` directives are gone; auto-derive schema/GVK in the builder. No `adapter.Resource[T]` yet. `GCX_AGENT_MODE=false mise run all` must pass (AC-012).
- **GATE (before any Step 2 code):** the §16 `any`-assertion-seam exception has been ruled acceptable (Open Question `[RESOLVED]` — no reasonable alternative within TypedCRUD). The remaining gate is documentation only: no `adapter.Resource[T]` or capability-assertion-seam code merges until `patterns.md` §16 is updated to record the sanctioned single-seam exception (AC-016, AC-018).
- **Step 2 (declarative model):** add `adapter.Resource[T]`, `ClientDeps`, `ListOptions`, `Cols[T]`, the capability interfaces, `Named`/`IDNamed`, and `NewProvider` (with the `WithCommands` attach hook); confine the type assertion to one audited seam; remove the `AsAdapter()` footgun; migrate SLO (registration only, `commands.go` untouched); promote the ADR, correct ADR-008 to `accepted`, document the §16 exception, and update `/add-provider` + `provider-guide.md` + `provider-checklist.md`.
- **Command auto-build** (auto standard-verb factory, `--limit`/`--dry-run` wiring) is a NAMED FOLLOW-UP SPEC, outside this sequence. The registration-side reduction (SLO `resource_adapter.go` 182 → ~50 lines) is realized here; the full ~540 → ~50 target lands only once command auto-build ships.
