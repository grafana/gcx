---
type: feature-spec
title: "Provider Registration Simplification — Declarative adapter.Resource[T] Front Door"
status: done
research: docs/designs/provider-registration-simplification.md
created: 2026-07-07
---

# Provider Registration Simplification — Declarative adapter.Resource[T] Front Door

## Problem Statement

Registering one provider resource type in gcx requires a large amount of mechanical scaffolding that is either derivable from the Go type/descriptor or duplicated across providers. Investigation of the live code found three concrete problems, verified against the current tree.

**1. Three divergent ways to register one type — and the cleanest is dead code.**

- **SLO** hand-builds an erased `adapter.Registration{}` literal plus a **duplicate** `TypedCRUD[Slo]` literal across factory functions. `internal/providers/slo/definitions/resource_adapter.go` is 182 lines and carries two `//nolint:dupl` directives (lines 85 and 146) on the repeated literal.
- **OnCall** re-invented a **private** generic helper — `buildRegistration[T adapter.ResourceNamer]` (`internal/providers/irm/oncall_adapter.go:60`), `oncallMeta()` (line 105), and `withCreate`/`withUpdate`/`withDelete` functional options over `crudOption[T]` — to register its many types.
- **The generic front door already exists and nobody uses it.** `TypedRegistration[T]` (singular struct, `internal/resources/adapter/typed.go:461`) and its method `ToRegistration()` (line 472) are referenced only by `typed_test.go` and doc comments in `typed.go:227` / `adapter.go:43` — no provider calls them. This is confirmed dead code.

**2. The relay and the nil-schema footgun.** A provider's `TypedRegistrations()` re-threads values the type package already knows — `Descriptor`, `GVK` (literally `desc.GroupVersionKind()`), `Schema` (always `adapter.SchemaFromType[T](desc)`), and `Example`. Because schema/example are relayed rather than derived, `adapter.TypedCRUD.AsAdapter()` deliberately returns `nil` from `Schema()`/`Example()` — a latent footgun for any caller expecting a complete adapter.

**3. The scaffolding tax compounds.** The provider surface is growing (SLO, synth, IRM, k6, fleet, faro, appo11y, aio11y, kg, dashboards, instrumentation). Every new type pays the duplication, relay, and footgun cost, and `/add-provider` plus `docs/reference/provider-guide.md` currently teach the boilerplate as required. Reducing it compounds across every future provider.

**Who is affected.** Provider authors (each new type), maintainers reviewing the duplicated `//nolint` literals, and the documentation that must teach the pattern. **Current workaround.** Copy an existing provider's `resource_adapter.go` and hand-thread the relay values — accepting the `//nolint:dupl` cost and the `AsAdapter()` footgun.

This spec builds on the already-implemented ADR-008 foundation (`ResourceIdentity` in `identity.go`, `TypedObject[T]`/`TypedCRUD[T]` in `typed.go`, unified `TypedRegistrations()` wiring in `registry.go`) and is a stepping stone toward K8s-native typed resources, not a competing direction.

## Scope

### In Scope

**Step 1 — pure consolidation (shippable milestone):**

- Lift OnCall's private `buildRegistration[T]` + `oncallMeta()` + `withCreate`/`withUpdate`/`withDelete` + `crudOption[T]` into the `adapter` package as the single consolidated builder, constrained by `adapter.ResourceNamer` (the value-safe subset of `ResourceIdentity`).
- Route OnCall's existing registrations through the lifted builder; remove OnCall's private copy.
- Delete the dead singular `TypedRegistration[T]` struct and its `ToRegistration()` method (or fold them into the consolidated builder), and clean up the stale doc-comment references.
- Dedupe SLO's duplicated `TypedCRUD[Slo]` literals so both `//nolint:dupl` directives are removed.
- Auto-derive `Schema` (from the Go type) and `GVK` (from the descriptor) inside the consolidated builder, removing the relay.

**Step 2 — declarative model (layered on Step 1):**

- Add `adapter.Resource[T]` with fields: `Group`/`Version`/`Kind`, `StripFields`, `NaturalKey`, `URLTemplate`, `Example`, and `NewClient`. (`Columns`/`adapter.Cols[T]` deferred to the standard-verb command auto-build follow-up — no consumer in this change.)
- Add `adapter.ClientDeps` carrying a pre-built `*http.Client` (`HTTP`), `BaseURL`, and `Namespace`.
- Add `adapter.ListOptions` (`Limit int64`, with room for label/field filters).
- ~~Add `adapter.Cols[T]` for optional table columns.~~ Deferred: no renderer consumes it in this change.
- Add the capability interfaces `Lister[T]`, `Getter[T]`, `Creator[T]`, `Updater[T]`, `Deleter[T]`, `Validator[T]`, with "unsatisfied interface ⇒ unsupported verb" semantics.
- ~~Add embeddable identity helpers `adapter.Named` (string name) and `adapter.IDNamed` (numeric slug reusing `slug.go`).~~ Deferred until a migration needs them: domain types keep hand-written `GetResourceName`/`SetResourceName`.
- Add `adapter.NewProvider(name, shortDesc, resources...)` returning a `providers.Provider`.
- Auto-derive `Schema`, `GVK`, `Singular`/`Plural` (via `strcase` from `Kind`), and `Namespace` (from loaded config), each with an override hook (notably `Plural` for irregular plurals).
- Wire CRUD by type-asserting the `NewClient` return value against the capability interfaces at exactly ONE audited seam inside the `adapter` package.
- Remove the `AsAdapter()` nil-schema footgun (schema/example derived, not relayed).

**SLO reference migration (registration only):**

- Migrate SLO (definitions) to declarative `adapter.Resource[Slo]` as the single reference implementation, replacing `resource_adapter.go`.
- Move SLO's bespoke create-then-refetch closure into an SLO client method (its `Creator`/`Updater` implementation).

**Docs + ADR deliverables:**

- Promote this design to a new ADR (new row in the `ARCHITECTURE.md` ADR table + a new file under `docs/adrs/`).
- Correct ADR-008 status `proposed` → `accepted` in both `ARCHITECTURE.md` (row 008) and `docs/adrs/typed-resource-adapter-compliance/001-typed-resource-adapter-foundation.md`.
- Document the §16 type-erasure exception in `docs/architecture/patterns.md` §16.
- Update the `/add-provider` skill (`.claude/skills/add-provider/SKILL.md`), `docs/reference/provider-guide.md`, and `docs/design/provider-checklist.md` to teach the declarative model.

### Out of Scope

- **Standard-verb command auto-build** — the auto `list`/`get`/`push`/`pull`/`delete` command factory, `--limit`/`--dry-run` wiring, and standard-verb `WithCommands` generation. Deferred entirely to a SEPARATE follow-up spec. Providers keep their hand-written `commands.go`. SLO's `definitions/commands.go` (449 lines) stays unchanged. The advertised ~540 → ~50 line reduction is the EVENTUAL target once command auto-build lands; THIS spec realizes only the registration-side reduction (SLO's `resource_adapter.go`, 182 lines, to a declarative `Resource[T]` declaration).
- **Migrating providers other than SLO.** OnCall, synth, IRM, k6, fleet, faro, appo11y, aio11y, kg, dashboards, and instrumentation converge opportunistically in later, out-of-scope work. (OnCall's builder is lifted into `adapter` in Step 1, but OnCall itself is not re-migrated onto `Resource[T]` beyond routing through the lifted builder.)
- **The datasource / signal-provider tier** (`metrics`, `logs`, `traces`, `profiles`) — these are not adapter-backed CRUD resources.
- **Retiring `TypedCRUD` in favour of K8s-native typed resources** (domain structs implementing the K8s `runtime.Object` / apimachinery interfaces directly, so the `unstructured` bridge is no longer needed) — the documented long arc. That is a much larger effort whose feasibility also depends on how each product's resources are represented and served upstream (today the Cloud tier adapts product-specific REST APIs into K8s-style envelopes); this spec is a stepping stone toward it, not that work.
- **The reflective / struct-tag GVK model (Option C)** — rejected: it trades accidental complexity for essential complexity and violates §16 with reflection-based dispatch and runtime panics.
- **Changing the `resources` discovery / push / pull pipeline itself.**

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Registration model | Option B — declarative `adapter.Resource[T]` + `adapter.NewProvider(...)` | One front door; derives schema/GVK/plural/namespace; smallest provider surface; stays inside TypedCRUD | design doc |
| CRUD wiring | Capability interfaces (`Lister`/`Getter`/`Creator`/`Updater`/`Deleter`/`Validator`), type-asserted at registration | Maps TypedCRUD's "nil Fn ⇒ unsupported" onto interface satisfaction; read-only/singleton fall out for free; avoids one fat `CRUDClient[T]` that forces stubs | design doc |
| Schema / GVK / plural / namespace | Derived (`SchemaFromType[T]`, `Descriptor.GroupVersionKind()`, `strcase` from `Kind`, config namespace) with override hooks | Pure functions of type/descriptor; hand-threading them is the relay smell | design doc |
| Identity boilerplate | Embeddable `adapter.Named` (string) / `adapter.IDNamed` (numeric slug) | Removes the repeated `GetResourceName`/`SetResourceName` pair; numeric path reuses `slug.go` | design doc |
| Type-erasure containment | Single audited assertion seam inside one `adapter` helper | §16 forbids spreading dispatch/erasure; one centralized seam is a known, reviewable, documentable exception | design doc |
| Rollout model | Option A first (pure consolidation, shippable), then Option B (declarative) | A is a near-pure refactor proven by OnCall; B layers the declarative model on the consolidated base with an intermediate safe point | design doc |
| Rollout scope | Full A+B, sequenced in this ONE spec | Both steps are needed to reach the declarative end-state; A alone leaves the relay and dead code partially addressed | user decision |
| Migration breadth | SLO (definitions) as the SOLE reference migration; delete dead code; others converge opportunistically | Prove the model on one provider without a big-bang migration; opportunistic convergence is out of scope | user decision |
| Command surface | Commands stay hand-written; standard-verb auto-build DEFERRED to a follow-up spec (overrides the design's "auto-built standard verbs" decision) | Keeps this spec registration-scoped and lower-risk; SLO's `commands.go` is untouched | user decision (supersedes design doc) |
| Docs + ADR | Included as first-class deliverables (new ADR, ADR-008 status fix, §16 exception doc, skill + guide + checklist updates) | This change alters a documented pattern and the `/add-provider` teaching path — it requires an ADR and doc updates, not just code | user decision |

## Functional Requirements

**Step 1 — consolidation**

- **FR-001**: The `adapter` package MUST expose a single consolidated registration builder lifted from OnCall's `buildRegistration[T]` + `oncallMeta()` + `withCreate`/`withUpdate`/`withDelete` + `crudOption[T]`, constrained by `adapter.ResourceNamer`.
- **FR-002**: OnCall's registrations MUST route through the consolidated builder in the `adapter` package, and OnCall's private `buildRegistration[T]` copy MUST be removed.
- **FR-003**: The dead singular `TypedRegistration[T]` struct and its `ToRegistration()` method MUST be deleted (or folded into the consolidated builder), and the stale doc-comment references (`typed.go:227`, `adapter.go:43`) MUST be removed or updated.
- **FR-004**: SLO's duplicated `TypedCRUD[Slo]` literals MUST be collapsed to a single construction path such that both `//nolint:dupl` directives (currently at `resource_adapter.go:85` and `:146`) are removed.
- **FR-005**: The consolidated builder MUST auto-derive `Schema` (via `SchemaFromType[T]`) and `GVK` (from the descriptor), so provider code MUST NOT hand-thread these values.

**Step 2 — declarative model**

- **FR-006**: The `adapter` package MUST provide a generic `adapter.Resource[T]` declaration type with fields `Group`, `Version`, `Kind`, `StripFields`, `NaturalKey`, `URLTemplate`, `Example`, `Columns` (typed `adapter.Cols[T]`), and `NewClient`.
- **FR-007**: The `adapter` package MUST provide `adapter.ClientDeps` carrying a pre-built `HTTP *http.Client` (logging, retry, `--insecure-log-http-payload`, timeouts, and auth mode already wired), `BaseURL string`, and `Namespace string`. `NewClient` MUST receive `ClientDeps`.
- **FR-008**: The `adapter` package MUST provide `adapter.ListOptions` with at minimum `Limit int64`, mapping onto the existing `TypedCRUD.ListFn(ctx, limit)` and `metav1.ListOptions.Limit`.
- **FR-009**: The `adapter` package MUST provide `adapter.Cols[T]` for declaring optional table columns; omitting it MUST yield the generic name/namespace/age table.
- **FR-010**: The `adapter` package MUST define capability interfaces `Lister[T]`, `Getter[T]`, `Creator[T]`, `Updater[T]`, `Deleter[T]`, and `Validator[T]`. A verb whose interface is unsatisfied MUST resolve to the "unsupported" path (`ErrUnsupported`); an unsatisfied `Validator[T]` MUST resolve to the no-op dry-run path, not an error.
- **FR-011**: The `adapter` package MUST provide embeddable identity helpers `adapter.Named` (string name) and `adapter.IDNamed` (numeric slug reusing `slug.go`), each satisfying `ResourceIdentity`.
- **FR-012**: The `adapter` package MUST provide `adapter.NewProvider(name, shortDesc, resources...)` returning a `providers.Provider` that implements `Name`, `ShortDesc`, `ConfigKeys` (defaulting to none), and the live `TypedRegistrations()` method built from the supplied resources.
- **FR-013**: Registering an `adapter.Resource[T]` MUST auto-derive `Schema` (`SchemaFromType[T]`), `GVK` (`Group`/`Version`/`Kind`), `Singular`/`Plural` (`strcase` from `Kind`), and `Namespace` (from the loaded config). Each derived value MUST be overridable via an ergonomic hook; the `Plural` override MUST handle irregular plurals.
- **FR-014**: The type assertion of the `NewClient` return value (`any`) against the capability interfaces MUST occur at EXACTLY ONE audited helper inside the `adapter` package, which builds the `TypedCRUD[T]` fields. No provider package MUST perform this assertion.
- **FR-015**: Declaring `NaturalKey` on `adapter.Resource[T]` MUST fold in `RegisterNaturalKey(...)`, and declaring `URLTemplate` MUST fold in the deeplink registration, so providers MUST NOT need a separate `init()` for either.
- **FR-016**: The `AsAdapter()` nil-schema footgun MUST be eliminated: the adapter path MUST expose the derived schema and example (either by returning them from `AsAdapter()` or by removing the method in favour of the derived path).
- **FR-017**: The live `TypedRegistrations()` provider-interface method and the `providers.Register()` / `registry.go` wiring MUST be preserved unchanged; `adapter.NewProvider` MUST implement `TypedRegistrations()`.
- **FR-018**: `adapter.NewProvider` MUST provide a mechanism (for example `adapter.WithCommands(...)`) for a provider to attach its EXISTING hand-written command tree. `adapter.NewProvider` MUST NOT auto-generate standard CRUD command verbs in this spec.

**SLO reference migration**

- **FR-019**: SLO (definitions) MUST be migrated to a declarative `adapter.Resource[Slo]` declaration, replacing `internal/providers/slo/definitions/resource_adapter.go` (182 lines) with the declarative form. This is a registration-only migration.
- **FR-020**: SLO's create-then-refetch behavior MUST move into an SLO client method (its `Creator`/`Updater` implementation) and MUST NOT remain a registration-side closure.
- **FR-021**: SLO's `internal/providers/slo/definitions/commands.go` (449 lines) MUST remain unchanged and MUST be attached to the provider via the command-attachment hook (FR-018).

**Docs + ADR**

- **FR-022**: A new ADR promoting this design MUST be added — a new file under `docs/adrs/` and a new row in the `ARCHITECTURE.md` ADR table (next free number, 021).
- **FR-023**: ADR-008's status MUST be corrected `proposed` → `accepted` in both `ARCHITECTURE.md` (row 008) and `docs/adrs/typed-resource-adapter-compliance/001-typed-resource-adapter-foundation.md`.
- **FR-024**: `docs/architecture/patterns.md` §16 MUST document the sanctioned single-seam `any`-assertion exception, and the statement "No `any` type erasure — all 17 types use concrete generics" (line ~608) MUST be qualified to reflect the audited exception.
- **FR-025**: The `/add-provider` skill (`.claude/skills/add-provider/SKILL.md`), `docs/reference/provider-guide.md`, and `docs/design/provider-checklist.md` MUST be updated to teach the declarative `adapter.Resource[T]` + `adapter.NewProvider` model.

## Acceptance Criteria

- **AC-001 (dual access preserved)**
  GIVEN SLO definitions migrated to `adapter.Resource[Slo]`
  WHEN a user invokes the `gcx slo` command surface AND `gcx resources` for the SLO definition type
  THEN both resolve through the same `ResourceAdapter` and return equivalent data.

- **AC-002 (schema/example auto-coverage)**
  GIVEN SLO definitions migrated to `adapter.Resource[Slo]`
  WHEN a user runs `gcx resources schemas` and `gcx resources examples`
  THEN the SLO definition type appears with a non-nil derived schema and a derived example.

- **AC-003 (read-only / singleton need no nil-plumbing)**
  GIVEN a resource whose client implements only `Lister[T]` and `Getter[T]`
  WHEN it is registered via `adapter.Resource[T]`
  THEN `Create`/`Update`/`Delete` resolve to `ErrUnsupported` with no nil Fn plumbing and no flags in the provider code.

- **AC-004 (dry-run routing)**
  GIVEN a client that implements `Validator[T]`
  WHEN a mutating verb runs with `--dry-run` (or `resources validate`)
  THEN the adapter invokes `Validate` instead of `Create`/`Update`;
  AND GIVEN a client that does NOT implement `Validator[T]` WHEN `--dry-run` runs THEN the mutation is skipped with no error.

- **AC-005 (single audited seam)**
  GIVEN the compiled tree
  WHEN grepping for the capability type assertions (`.(adapter.Lister[`, `.(adapter.Getter[`, …)
  THEN they appear in exactly one helper file inside `internal/resources/adapter/`.

- **AC-006 (dead code removed)**
  GIVEN the tree
  WHEN grepping for the singular `TypedRegistration[` and `ToRegistration`
  THEN no definitions or references remain (test files included).

- **AC-007 (live method preserved)**
  GIVEN the tree
  WHEN grepping for `TypedRegistrations()`
  THEN the plural provider-interface method still exists, `adapter.NewProvider` implements it, and `providers.Register()` / `registry.go` wiring is unchanged.

- **AC-008 (SLO duplication removed)**
  GIVEN SLO definitions after migration
  WHEN grepping the SLO definitions package for `//nolint:dupl`
  THEN neither directive remains.

- **AC-009 (SLO commands untouched)**
  GIVEN `internal/providers/slo/definitions/commands.go`
  WHEN compared against the pre-migration baseline
  THEN it is byte-for-byte unchanged (449 lines) and is attached via the command-attachment hook.

- **AC-010 (HTTP client reuse)**
  GIVEN a provider migrated to `adapter.Resource[T]`
  WHEN its `NewClient` builds the REST client
  THEN it uses `ClientDeps.HTTP` and constructs no `http.Transport`/`RoundTripper` of its own.

- **AC-011 (irregular-plural override)**
  GIVEN a `Kind` whose `strcase` plural is incorrect
  WHEN the `adapter.Resource[T]` declares a `Plural` override
  THEN the derived plural uses the override across discovery and the `resources` pipeline.

- **AC-012 (Step 1 independently shippable)**
  GIVEN only Step 1 (consolidation) has landed
  WHEN `GCX_AGENT_MODE=false mise run all` runs
  THEN it passes with no `adapter.Resource[T]` yet required, OnCall routed through the lifted builder, and SLO's `//nolint:dupl` directives removed.

- **AC-013 (existing providers unaffected)**
  GIVEN the framework has landed
  WHEN the full test suite runs
  THEN every existing provider's registrations and commands continue to work.

- **AC-014 (ADR-008 corrected)**
  GIVEN `ARCHITECTURE.md` row 008 and `docs/adrs/typed-resource-adapter-compliance/001-typed-resource-adapter-foundation.md`
  WHEN read
  THEN the status is `accepted`.

- **AC-015 (new ADR present)**
  GIVEN the `ARCHITECTURE.md` ADR table
  WHEN read
  THEN a new row (021) links to a new ADR file promoting this design.

- **AC-016 (§16 exception documented)**
  GIVEN `docs/architecture/patterns.md` §16
  WHEN read
  THEN it documents the sanctioned single-seam `any`-assertion exception AND the "No `any` type erasure" statement is qualified accordingly.

- **AC-017 (docs teach the declarative model)**
  GIVEN `.claude/skills/add-provider/SKILL.md`, `docs/reference/provider-guide.md`, and `docs/design/provider-checklist.md`
  WHEN read
  THEN each teaches the `adapter.Resource[T]` + `adapter.NewProvider` model, not the hand-built `Registration` boilerplate.

- **AC-018 (§16 exception documented before the Step 2 seam ships)**
  GIVEN the §16 `any`-seam exception has been ruled acceptable (Open Questions — no reasonable alternative within TypedCRUD)
  WHEN the Step 2 capability-assertion seam (`adapter.Resource[T]`) is merged
  THEN `docs/architecture/patterns.md` §16 already documents the sanctioned single-seam exception (FR-024); the seam MUST NOT merge ahead of that documentation.

- **AC-019 (provider ↔ resources output consistency)**
  GIVEN a resource served through the shared adapter (e.g. SLO definitions)
  WHEN the same object is fetched via the provider command surface and via the `resources` pipeline in the same format — e.g. `gcx slo … list -o yaml` / `-o json` vs `gcx resources get <slo-plural> -o yaml` / `-o json`
  THEN the emitted object is field-for-field equivalent across both front doors (per `patterns.md` §16 "Provider / Resources Output Consistency"), and the same equivalence holds for any other provider sharing the adapter code.

## Negative Constraints

- **NC-001**: NEVER introduce a second type-erasure, dispatch, or serialization-bridge mechanism beyond the single audited seam of FR-014.
- **NC-002**: NEVER spread the `any` capability assertion across provider packages — it MUST live in exactly one `adapter` helper.
- **NC-003**: DO NOT auto-generate standard CRUD command verbs (`list`/`get`/`push`/`pull`/`delete`) in this spec.
- **NC-004**: DO NOT modify `internal/providers/slo/definitions/commands.go`.
- **NC-005**: DO NOT change the `resources` discovery / push / pull pipeline.
- **NC-006**: DO NOT delete or rename the live plural `TypedRegistrations()` provider-interface method.
- **NC-007**: NEVER hand-roll HTTP transport in a provider's `NewClient` — the pre-built `ClientDeps.HTTP` MUST be used.
- **NC-008**: DO NOT migrate any provider other than SLO in this spec.
- **NC-009**: DO NOT introduce a reflective or struct-tag GVK model (Option C).

## Risks

| Risk | Impact | Mitigation |
|------|--------|-----------|
| The `NewClient → any → capability-assertion` seam reintroduces type-erasure that §16 forbids | Constitution/pattern violation | Ruled acceptable as a documented exception (Open Questions — no reasonable alternative within TypedCRUD); confine to a single audited helper (FR-014); record the exception in §16 (FR-024) and ship it together with the seam (AC-018) |
| Auto-derived `Plural` via `strcase` mis-handles irregular plurals | Wrong resource plural in discovery / `resources` pipeline | Ergonomic, documented `Plural` override hook (FR-013, AC-011) |
| SLO's create-then-refetch closure moving into a client method changes create/update behavior (e.g. empty-UUID ⇒ create) | Behavioral regression in SLO create/update | Move the behavior verbatim into the client method; cover with SLO adapter tests (FR-020) |
| Lifting OnCall's builder into `adapter` and re-routing OnCall | Regression across OnCall's many registered types | Route OnCall through the lifted builder in Step 1 and rely on the full suite as a regression gate (FR-002, AC-013) |
| Migrating providers is incremental and non-trivial | Inconsistent provider surface during the convergence window | SLO is the reference exemplar; `provider-guide.md`/`provider-checklist.md`/`/add-provider` teach the model; other-provider convergence is explicitly out of scope |
| Command auto-build deferred | The advertised ~540 → ~50 reduction is not realized in this spec; expectation mismatch | Spec is explicitly scoped to registration-side reduction only (182 → ~50); command reduction is a named follow-up spec (Out of Scope) |

## Open Questions

- **[RESOLVED]** Rollout scope — full A+B, sequenced in this one spec (user decision).
- **[RESOLVED]** Migration breadth — SLO (definitions) only as the reference; other providers converge opportunistically in out-of-scope work (user decision).
- **[RESOLVED]** Command auto-build — deferred entirely to a separate follow-up spec; providers keep hand-written `commands.go`, including SLO's (user decision).
- **[RESOLVED]** Does the contained `any`-assertion seam satisfy `patterns.md` §16's "no type-erasure" rule? Ruling (2026-07-07, maintainer): **acceptable as a documented exception** — detecting which capability interfaces a client implements intrinsically requires a type assertion at the `NewClient` boundary, so there is no reasonable alternative within the TypedCRUD model. Remaining obligations: keep the erasure confined to one audited helper (FR-014) and record the sanctioned single-seam exception in §16 (FR-024); the Step 2 seam must not merge ahead of that §16 update (AC-018).
- **[DEFERRED]** Numeric-ID identity — is `adapter.IDNamed` (wrapping `slug.go`) sufficient, or do slug-id providers (Fleet, Synth) need a richer identity hook? Out of scope until those providers migrate.
- **[RESOLVED]** ADR relationship — this design EXTENDS ADR-008 (whose status is corrected `proposed` → `accepted` because its foundation already shipped) and is promoted to its own ADR 021 (user decision + design doc).
