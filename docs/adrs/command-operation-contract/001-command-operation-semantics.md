# ADR: Command Operation Contract and Pre-GA Surface Convergence

**Created**: 2026-07-16
**Status**: proposed
**Supersedes**: none
**Amends on acceptance**: [cross-signal ADR](../signal-provider-ux/001-cross-signal-command-consistency.md) §6 "Aliases and clean breaks" (compatibility policy for future renames only)

<!-- Status lifecycle: proposed -> accepted -> deprecated | superseded -->

## Context

gcx exposes ~487 runnable leaf commands across its provider and utility
surfaces (19 registered providers; measured at `main@9d193ca7`,
2026-07-16). The command vocabulary evolved organically:
the ~487 leaves end in 142 distinct final tokens, while the five standard
CRUD verbs already cover 274 of them. The
same user intent is spelled differently across providers (`get`, `show`,
`inspect`, `describe-table`), some leaves are nouns whose behavior is not
discoverable from the name (`kg meta logs`), and sub-resource addressing is
inconsistent. For a CLI that is driven by both humans and AI agents — which
discover the surface by walking `--help` as a decision tree and embed exact
invocations in skills — this is a discoverability and automation-contract
defect, not a cosmetic one.

Prior art inside the repo: [CONSTITUTION.md § CLI Grammar](../../../CONSTITUTION.md)
defines `$AREA $NOUN $VERB` as the default grammar with a closed
single-token top-level set; CONSTITUTION.md
§ Provider Architecture currently *forbids* provider-only resources from
using `get`/`list` (mandating `show`/`describe`/`search`), which review
discussion has since turned against; the
[cross-signal ADR](../signal-provider-ux/001-cross-signal-command-consistency.md)
standardized the signal tier; the
[UX-consistency design](../../plans/2026-04-14-ux-consistency-design.md)
deferred verb taxonomy to a dedicated decision. A maintainer ruling
(July 2026) established two rules of thumb: the last path word identifies
the action, and the type of ID you pass commands the nesting. The
[command-surface consistency audit](../../research/2026-07-01-command-surface-consistency-audit.md)
inventoried the deviations.

External forces: Grafana's App Platform structures its APIs as
`/apis/<group>/<version>/namespaces/<ns>/<resource>[/<name>]` — collection
routes for list/create, named-resource routes for get/update/delete — while
the legacy `/api` surface is being migrated
([API structure in Grafana](https://grafana.com/docs/grafana/latest/developer-resources/api-reference/http-api/apis/))
with no exact `/apis` replacement guaranteed for every endpoint
([Migrate to the new APIs](https://grafana.com/docs/grafana/latest/developer-resources/api-reference/http-api/apis-migration/)).
Command names must survive that backend migration. Grafana's
[Git Sync resource-export documentation](https://grafana.com/docs/grafana/latest/as-code/observability-as-code/git-sync/export-resources/)
already instructs users to automate with `gcx resources pull`: command
paths are automation contracts today. The
[Grafana App SDK's resource package](https://github.com/grafana/grafana-app-sdk/tree/main/resource)
models Get/List/Create/Update/Patch/Delete plus custom subresource routes;
[Grafana's MCP surface](https://github.com/grafana/mcp-grafana) uses
`list_*`/`get_*`/`query_*`/`search_*` and retains `describe_*` where the
result is genuinely a schema/composite view.

gcx has not shipped v1.0.0. This is the last cheap moment to converge the
surface: after GA, command paths are stable interfaces and every rename is
a breaking change.

## Decision

### 1. Transport-neutral operation contract

A command operation is determined by its **user-visible subject,
addressability, result cardinality, and side effects**. It MUST NOT depend
on HTTP method, API path or version, provider-adapter registration, or
transport.

Consequences of this invariant: whether a resource happens to be registered
as a typed adapter is irrelevant to its verb; a genuine read-one is `get`
regardless of how it is implemented; command names must not change when a
provider migrates from `/api` to `/apis`.

### 2. The contract model

Every active runnable command has a declared contract:

| Field | Values | Meaning |
|---|---|---|
| Operation | `list`, `get`, `create`, `update`, `delete`, `patch` (reserved), `upsert`, `push`, `pull`, `query`, `search`, view ops (§5), domain ops (§6), utility ops (§7) | what it does |
| Subject | free noun (`recommendations`, `alerts`, `labels`, …) | what it acts on |
| Surface form | `direct` \| `compound` \| `shorthand` | how it is spelled (syntax shape only) |
| Conformance | `standard` \| `protocol_exception` | whether the spelling follows the default grammar or a ratified protocol family |
| Category | `entity` \| `manifest` \| `query` \| `view` \| `domain` \| `utility` | operation family |
| Effect | `read_only` \| `mutating` \| `destructive` (+ `effect_varies` flag) | conservative static risk classification |
| Addressing | `none` \| `singleton` \| `subject` \| `parent` \| `selector` \| `composite` | what identity it takes |
| Result shape | `item` \| `collection` \| `item_or_collection` \| `report` \| `stream` \| `mutation` \| `none` \| `opaque` | the **logical semantic payload**, independent of serialization (not a wire/output schema). Deletes and updates may be `mutation` (a summary is returned) or `none` (silent success) — operation + effect already communicate that a mutation occurred |
| Lifecycle | `active` \| `deprecated` (+ replacement, earliest-eligible removal) | stability state |
| Contract source | `explicit` \| `inferred` \| `builtin` \| `unresolved` | where the contract came from |

Surface form and conformance are separate axes on purpose: a compound or a
shorthand can be perfectly standard (`list-alerts`, `labels`), while a
direct spelling can be a protocol exception (`resources get`). `direct`
means the token names its operation; `shorthand` means it doesn't
(`labels` → list, `view` → get); `compound` is `<operation>-<subject>`.

Category and Effect are deliberately separate: `query`/`view`/`domain`
describe operation families; side effects are an orthogonal axis.

**Effect is the command's conservative static risk classification**, with
an explicit ordering `read_only < mutating < destructive`. A command whose
effect depends on its invocation (e.g. the raw API escape hatch) declares
its **maximum** effect and sets `effect_varies: true`, with
`effect_conditions[]: {when, effect}` as explanatory metadata for humans —
never a machine-evaluated decision procedure (flag precedence is
implementation-defined; e.g. an explicit `-X GET` overrides `-d`, so
`gcx api -X GET -d …` remains a GET). `--dry-run` and similar preview
flags do NOT downgrade the classification.

**Effect's boundary**: it classifies the command's *semantic* effect on
user-managed remote resources and on durable local configuration or files
the user manages (contexts, credentials the user set, written output
files). It deliberately **excludes transparent implementation
side effects** — OAuth/session token rotation, credential-store
migration performed by the config loader, caches, telemetry and notifier
bookkeeping, logs — the same distinction HTTP's safe-method semantics
draw between the requested operation and incidental server activity.
Datasource-UID auto-persistence stays **in** scope because it changes
future target resolution (see the hidden-write policy, §4). Under this
boundary `login` is `mutating` (it durably writes user credentials and
context config), while an ordinary read that happens to refresh a token
is `read_only`.

**`destructive` means possible loss**: deletion, revocation, or
whole-resource/file replacement that can discard existing state — not
merely `delete` (a full-object upsert that can discard an existing spec is
destructive in the same sense; the App SDK's Upsert can completely replace
an object). An ordinary field update that overwrites one value is
`mutating`, not destructive.

**Effect is one safety input, not a complete authorization decision.**
Agents combine it with the resolved target and cardinality, the actual
invocation arguments, required permissions, reversibility, and their own
confirmation policy.

`singleton` is an **addressing** concept, not a result shape. Logical
cardinality is user-visible, not container-visible: a `get` result may
contain nested arrays.

Two values exist for commands the simple enums cannot truthfully
describe, and each carries **structured supporting data** — the enum alone
is not a valid contract for them:

- `composite` addressing MUST record the ordered identity tuple as
  `identity_parts[]: {name, role, required, position_or_flag}`
  (`instrumentation services get <cluster> <namespace> <service>` →
  three ordered required positional parts).
- `opaque` result means the shape is not statically known
  (`gcx api PATH` returns whatever the endpoint returns).

Identity supplied through flags (rather than positionals) is likewise
declared in `identity_parts` with `position_or_flag` set to the flag
name; explicit identity metadata is REQUIRED for every syntax-ambiguous
case — optional identities, variadic or multiple targets, selectors,
flag-based identity, composite identity, and alternative forms. A command
accepting **alternative identity forms** ("one positional OR two flags")
declares `identity_forms[]` — each entry a complete `identity_parts`
list; plain `identity_parts` is shorthand for a single form.

Cobra-owned built-ins (`help`, `completion <shell>`) carry
`contract_source=builtin` rather than entering the migration census.

Examples:

- `slo definitions list` → {list, definitions, direct, standard, entity, read_only, none, collection}
- `irm oncall alert-groups list-alerts <group-id>` → {list, alerts, compound, standard, entity, read_only, parent, collection}
- `metrics labels` → {list, labels, shorthand, standard, query, read_only, none, collection}
- `kg entities inspect <entity>` → {inspect, entities, direct, standard, view, read_only, subject, report}
- `resources get [RESOURCE_SELECTOR]...` → {get, resources, direct, protocol_exception, entity, read_only, selector, item_or_collection}

### 3. Entity operations (normal provider/product behavior)

- `list` enumerates zero or more independently meaningful subjects.
- `get` retrieves one addressable subject or singleton in its stable
  structured representation.
- `create` requires absence; `update` requires existence; `delete` removes
  an existing subject.
- `update` is a typed resource update — which, like most resource APIs,
  MAY be partial. `patch` is **reserved** specifically for operations
  taking an explicit patch document or patch-operation list (the App SDK
  exposes Patch separately from Update); "partial" alone does not
  distinguish them. No command uses `patch` until such a surface exists.
- `upsert` is defined by the user-visible guarantee, not the transport:
  **one invocation creates when absent or updates/replaces when present,
  without requiring the caller to choose create versus update**. It is
  permitted only where the backend genuinely provides that semantic —
  splitting a true upsert into `create`/`update` is rejected: it would
  falsely promise existence checks and introduce read-then-write races.
  (The alert notification-template provisioning API — a single PUT keyed
  by template name — is evidence that its current implementation
  qualifies; endpoint shape is evidence, never the definition.) A
  full-object upsert that can discard existing state is classified
  `destructive`.
- `push`/`pull` are the manifest (GitOps) operations, unchanged.

**Generic resource-tier exception (protocol family).** The Kubernetes-style
resource family is an explicit exception: `gcx resources get
[RESOURCE_SELECTOR]...` uses **selector addressing** and may return an item
or a collection (`dashboards`, `dashboards/foo`, `dashboards/foo,bar`,
multiple kinds). This mirrors kubectl and is already published in Grafana's
Git Sync documentation. It is modeled as
{get, direct, protocol_exception, selector, item_or_collection} — the universal
read-one definition explicitly does not apply to this family.

### 4. Query operations and signal shorthands

`query` executes a user-supplied data query; `search` performs discovery of
matching subjects. Signal nouns — `labels`, `series`, `metrics`,
`metadata`, and (recommended, pending the decision on the cross-signal
ADR's ratification) `profile-types` with the contract {list,
profile-types, shorthand, standard, query, read_only, none, collection} —
are **approved surface shorthands** mapped to their real list/query
semantics ({list|query, <noun>, shorthand, standard, query}). They are not
independent semantic operations, and they are not violations: the
shorthand set is closed and governed. This resolves the tension between
the "last word identifies the action" rule and the ratified signal-tier
command set. `profile-types` uses one shared builder mounted at **both**
`profiles profile-types` and `datasources pyroscope profile-types` —
approval covers both mount points. The acceptance commit records the
selected `profile-types` outcome here, in the cross-signal ADR, and in
the naming guide's vocabulary table — not merely a status flip.

**Hidden-write policy (reads are pure).** Read and query commands MUST
NOT mutate persistent state **within Effect's semantic boundary** (§2 —
incidental token rotation, caches, and similar transparent bookkeeping
are outside it). Today several datasource
reads violate this: datasource resolution best-effort persists an
auto-discovered datasource UID to configuration
(`ResolveAndSaveDatasource` in `internal/datasources/query/resolve.go`),
which is squarely inside Effect's boundary (it changes future target
resolution), making those reads effectively mutating. The decided policy: read/query commands move to a **pure
resolver**, with persistence of discovered datasource UIDs relocated to
an explicit setup/config action — preserving honest `read_only`
contracts instead of marking dozens of ordinary reads
`mutating` + `effect_varies` (safe but agent-hostile). Until that
refactor lands, affected commands are census entries carrying the honest
`mutating`/`effect_varies: true` classification. The resolver refactor
itself will be tracked as a separate implementation issue (filed with
the acceptance commit) — the naming decision does not depend on it.

### 5. View operations

View verbs are permitted only where the output contract **materially
differs from ordinary retrieval**:

- `status` — current condition or health
- `timeline` — time-ordered events
- `inspect` — composite diagnostic analysis involving related data
- `diff` — comparison between states
- `stats` — numeric aggregates
- `report` — a cohesive analytical artifact
- `describe` — a narrowly defined schema/composite view, admitted per
  command where behavior supports it

Within gcx, `show` is overloaded and lacks a distinct invariant — today it
variously means "read one", "list many", and "render a computed view" —
so it is not canonical here (other CLIs use it coherently; gcx never did). `summary` is not canonical; existing
`summary` commands are classified during migration as `stats`, `status`, or
`report` according to their real output. Neither `describe` nor `summary`
is declared "universally nonstandard" — Grafana's MCP surface offers
**surface-name evidence** (`describe_clickhouse_table`,
`describe_athena_table`, `get_dashboard_summary`) that such spellings
appear where the output is genuinely a schema or composite view; this is
evidence about naming, not a claim that `summary` is an independent
semantic operation. The rule is the materially-different-output test,
applied per command.

`kg entities inspect` is ratified as a valid diagnostic view (its output is
an RCA timeline plus related entities; its own help text distinguishes it
from property reads).

### 6. Domain operations

Domain verbs (examples: `acknowledge`, `silence`, `escalate`, `open`,
`close`, `resolve`, `restore`, `run`, `deploy`, `validate`, `sync`) are
the right shape where CRUD would misrepresent the behavior — you close an
incident, you do not "update" it closed. Domain operations MUST be
entries in a governed, reviewed vocabulary registry (a normal reviewed PR
to the registry, not a constitutional amendment per verb). **This ADR
ratifies the governance model only — no individual domain operation is
ratified by this document**; the initial registry lands as a separately
approved diff (see the rollout plan), with inverse pairs
(`unacknowledge`, `unsilence`, `unresolve`) reviewed together with their
base verbs.

### 7. Utility operations

Commands that operate on the CLI itself or on the connection — not on
Grafana resources — carry full tuples like any other command. One row per
**runnable** command, derived from its actual `Use` and behavior
(verified at `main@9d193ca7`); non-runnable groups (e.g. the top-level
`setup` group, whose only runnable child is `setup status`) do not carry
contracts:

| Runnable command | Semantic operation (surface) | Effect | Addressing | Result |
|------------------|------------------------------|--------|------------|--------|
| `login [CONTEXT_NAME]` | `login` | mutating (persistent local credentials/config) | subject — optional context via `identity_parts` (`{context_name, required: false}`) | mutation |
| `setup status` | `status` (view op, §5) | read_only | none | report |
| `version` | `version` | read_only | none | item |
| `commands` | `commands` (emit the machine-readable catalog) | read_only | none | collection |
| `help-tree [COMMAND...]` | `help-tree` | read_only | selector — optional variadic command paths via `identity_parts` | report |
| `api PATH` | **`request`** (spelled `api`; conformance `protocol_exception`) | **destructive** (`effect_varies: true` — most invocations are GETs, but agents authorize against the maximum) | subject (the PATH) | opaque |
| `config view` | `get` (spelled `view`) | read_only | singleton | item |
| `config current-context` | `get` (spelled `current-context`) | read_only | singleton | item |
| `config path` | `list` (spelled `path` — enumerates the config-source entries contributing to the merged configuration) | read_only | none | collection |
| `config list-contexts` | `list` (canonical compound `list-contexts`) | read_only | none | collection |
| `config use-context [CONTEXT_NAME]` | `update` (spelled `use-context`) | mutating (local config) | subject — optional via `identity_parts` | mutation |
| `config set PROPERTY_NAME PROPERTY_VALUE` | `update` (spelled `set`) | mutating (local config) | subject (the property) | none (silent success today; a result change is a separate output decision) |
| `config unset PROPERTY_NAME` | `update` (spelled `unset` — clears a property or removes a map entry on the config singleton) | **destructive** — its maximum normal effect: `gcx config unset contexts.foo` deletes an entire context from config; its keychain-backed secrets are left **orphaned** (reconciliation only visits surviving contexts) | subject (the property or entry) | none (silent success today) |
| `config edit [type]` | `update` (spelled `edit` — interactive) | **destructive** static maximum with `effect_varies: true` — unrestricted editing can remove whole contexts, same as `unset` | subject — optional via `identity_parts` | none (editor session; no result payload) |

**`request` is the semantic operation; `api` is its surface spelling** —
naming the operation after the transport surface would violate §1. The
`config` family's approved protocol-family exceptions
(conformance `protocol_exception`) are exactly the closed list in §9:
`view`, `use-context`, `current-context`, `path`, `list-contexts`, `set`,
`unset`, `edit` — each mapped to its real entity semantics above.
`config check` is NOT in the approved list: its operation (`check`) is a
registry candidate and the command remains a census entry until the
registry diff is approved. The runnable `instrumentation setup <cluster>`
is likewise NOT a utility — it is a domain operation acting on a cluster
and goes through the domain-registry review like any other domain
candidate.

### 8. Addressability

The command path indicates the type of the first required positional
identity:

- Identity is the **parent's** → operation-subject compound under the
  parent: `$PARENT $OPERATION-$CHILD $PARENT_ID`
  (`alert-groups list-alerts <group-id>`).
- Identity is the **child's** → the child is independently addressable
  and warrants a child resource group
  (`experiments trials get-scores <trial-id>`). The basis is stable
  addressability/identity — not how many child operations happen to exist
  today (operation count is a heuristic, not the rule).
- Catalog children with no parent identity may keep a noun group with
  `list` (`incidents severities list`).
- Selector, optional, variadic, multiple, or flag-supplied identities
  cannot be derived from syntax and REQUIRE explicit contract metadata
  (e.g. `instrumentation services get <cluster> <namespace> <service>`;
  `slo definitions status [UUID]` returns one or many depending on the
  optional argument).

Syntax alone cannot prove an identity's resource type; explicit semantic
metadata is the source of truth for ambiguous cases.

### 9. Pre-GA surface convergence (normative)

gcx v1.0.0 will ship a fully classified canonical command surface:

- Every active runnable command MUST conform to this operation contract or
  be an intentional canonical exception.
- Commands whose names misrepresent their subject, addressability, result,
  or side effects MUST be renamed or removed before v1.0.0.
- The temporary legacy-contract baseline is migration scaffolding only and
  MUST be empty before v1.0.0.
- First-party documentation, examples, skills, annotations, and generated
  references MUST use canonical paths.
- **Zero gcx-owned `lifecycle=deprecated` commands or noncanonical
  compatibility paths ship in v1.0.0.** If maintainers want an exception,
  they explicitly amend this decision — that is cleaner than designing
  permanent exception machinery (owner/evidence/removal-policy records
  would have no home once the census is deleted at convergence).

Migration does not mean blindly renaming every unusual command. Each
current command resolves through exactly one of four outcomes:
**keep and annotate** (already canonical), **ratify** (intentional
canonical exception — recorded in this ADR's exception lists or the
vocabulary registry), **rename** (name misrepresents the contract), or
**remove** (obsolete or duplicated).

Intentional canonical exceptions (closed lists): the single-token
top-level commands enumerated in CONSTITUTION.md § CLI Grammar (which this decision
extends to include `commands`, `help-tree`, and `api` — they exist today
and must not be simultaneously canonical exceptions and constitutional
violations); kubectl-parity `config` commands — exactly the closed list
of §7: `view`, `use-context`, `current-context`, `path`, `list-contexts`,
`set`, `unset`, `edit` (`config check` is a census candidate awaiting the
registry diff, not an approved exception); Cobra built-ins (`help`,
`completion <shell>`, `contract_source=builtin`); the resource-tier
protocol family (§3); the signal shorthands (§4).

**`gcx api` safety contract.** The raw API escape hatch is not a harmless
utility: it can mutate or delete arbitrary API objects. Its complete
tuple: operation **`request`** (§7; `api` is the surface spelling,
conformance `protocol_exception`), subject `grafana-http-api`, category
`utility`, effect **`destructive`** with `effect_varies: true` — the
static classification agents authorize against — and explanatory
`effect_conditions` covering **both** `--method` and `--data` (a request
body implies POST **only when `--method` is omitted**; an explicit
`--method` wins), addressing
`subject` (the PATH positional), result **`opaque`**. It MUST never be
presented — in help text, catalog metadata, or agent annotations — as
read-only.

**Convergence is explicit-only.** Before v1.0.0, every gcx-owned runnable
command must carry an **explicit** contract, declared beside its
constructor or inherited from an explicit shared builder. Bootstrap
inference, the legacy baseline, its loader, and its debt ceiling are
migration machinery and are **removed after convergence**; from v1.0.0 on,
CI permanently rejects `inferred` and `unresolved` contracts (Cobra
built-ins remain `builtin`). This prevents inference from becoming the
next permanent debt system.

### 10. Enforcement

- **Local metadata ownership.** New commands declare their contract beside
  their constructor or inherit it from a shared builder. A permanent
  central path-keyed contract registry is rejected: a recent single-provider
  rename churned 61+61 lines in the existing path-keyed annotation map —
  path-keyed registries are rename hazards by construction.
- **One shared resolver.** CI enforcement, the machine-readable command
  catalog, and command-surface generation all consume the same resolution
  logic; there is exactly one definition of a command's contract.
- **Inferred metadata is never authoritative for agent authorization.**
  Bootstrap inference exists to stage migration — it may classify spelling
  and obvious cardinality, but agents MUST treat only `explicit` contracts
  as authoritative for safety decisions; anything `inferred` is unverified
  by definition and disappears before v1.0.0.
- **Conservative inference, bootstrap only.** Inference from command syntax
  exists to classify the existing tree cheaply and is limited to obvious
  cases: a no-identity `list`; a `get` with exactly one simple required
  positional; a no-argument `get` (singleton); a simple operation-subject
  compound with exactly one required parent positional. Optional/variadic/
  multiple/flag-supplied identities, selector syntax, alternatives in
  `Use`, views, and domain actions are never inferred.
- **Fail closed on the complete tuple.** Every required contract field —
  operation, subject, surface form, category, effect, addressing, result
  shape — must resolve; a command missing any of them either carries
  explicit metadata or an exact-path entry in the temporary legacy
  baseline, otherwise CI fails. The operation registry additionally
  declares **allowed combinations** and CI validates the full tuple
  against them: `list` normally permits read_only + collection; `get`
  normally permits read_only + item (the selector protocol family
  explicitly permits `get` + item_or_collection); `delete` must be
  destructive, with result `mutation` or `none` depending on whether the
  command returns a mutation summary (allowed combinations validate
  *logical* semantics, never wire/output formats, which remain out of
  scope); the `config` protocol-family exception
  explicitly permits {`update`, destructive, none} for `config unset` and
  `config edit` (their maximum normal effect can remove whole map entries
  such as contexts).
  A command declaring a vocabulary operation with an incompatible
  effect/result tuple fails even though the token is valid.
- **Honest ratchet.** The legacy baseline is exact-path, carries rule
  codes, an owner, and a rationale per entry, and may only shrink. A fixed
  count ceiling prevents net growth but does not prove shrink-only
  behavior on its own; shrink-only is enforced by review of baseline diffs
  (optionally by a base-branch comparison in CI, outside the hermetic test
  suite). The GA gate — an empty baseline — is the terminal guarantee.
- **Versioned surface.** A deterministic, versioned command-surface JSON
  document (paths, positional shapes, aliases, semantic contract fields,
  lifecycle, hidden/deprecated mappings) is generated and drift-checked in
  CI alongside the existing reference docs.
- **Alias governance.** Permanent synonyms and deprecated compatibility
  paths are distinct, explicitly declared concepts. Undeclared aliases that
  misrepresent semantics (e.g. CRUD-named aliases on a true upsert) are
  contract violations to resolve during migration.

### 11. Compatibility policy

v1.0.0 is deliberately the clean compatibility boundary. **Noncanonical
command paths are renamed or removed during v1 development, with no
deprecated aliases and no compatibility forwarders**: gcx is in public
preview, and the preview period is exactly when clean breaks are
permitted. Migration support is documentation, not shims — the v1.0.0
release notes / migration guide carry a complete old-path → new-path
table, and published Grafana documentation that references gcx commands
(e.g. the Git Sync export guide) is updated at release time.

After v1.0.0, command paths are stable interfaces:

- A minor release MAY add a canonical replacement and deprecate the old
  path while keeping it functional; the deprecation notice goes to
  **stderr only** (never through the stdout stream — structured output
  must stay byte-clean), with replacement metadata and an
  earliest-eligible removal version in the contract.
- Removal occurs no earlier than the next major version, requires
  explicit approval, and requires zero first-party references.
- Any post-GA compatibility path is expected to remain until the next
  major.

On acceptance, this section supersedes the cross-signal ADR's §6 pre-GA
clean-break stance for any path renamed under this contract going forward
(a scoped amendment — that ADR's already-executed renames are
unaffected).

### Explicitly rejected

- "The last word is always a verb" as the enforcement rule (word-shape
  grammar): rejected in favor of declared operation semantics — it
  misclassifies `status`, `labels`, and `list-trials`, and cannot express
  subjects, addressing, or effects.
- Universal `get`-returns-one-item: rejected; the resource-tier selector
  family is a documented protocol exception.
- `show`/`describe`/`search` as the mandated verbs for provider-only
  resources (current CONSTITUTION text): rejected and replaced by this
  contract.
- A permanent central path-keyed contract registry: rejected (rename
  hazard).
- Nested noun groups as the default sub-resource shape (`alert-groups
  alerts list`): rejected in favor of the addressability rule.
- Splitting true upserts into create/update: rejected (races, false
  existence semantics).
- Broad v1 forwarders for pre-GA names: rejected (v1 ships canonical).
- A final v0.x compatibility release carrying temporary forwarders:
  considered and declined — gcx is in public preview, and preview paths
  are not preserved across the deliberately clean v1 boundary; the
  migration guide and updated published documentation carry users over.

### Out of scope

Output-format defaults and codecs; typed-adapter (TypedCRUD) migrations;
backend capability modeling (native/emulated/unsupported), pagination,
rate limiting, retry/idempotency, and authentication standardization —
tracked separately as client-platform work; k6 run-history consolidation
except where a semantic rename requires it.

## Consequences

Easier: agents and humans can predict a command's behavior from its name
and its machine-readable contract; new commands have one law to follow and
one place to declare it; CI prevents regression; renames become mechanical
(contract-verified) instead of debatable; the v1 surface is a real
contract.

Harder / costs: an initial migration census (~150–200 unresolved entries
expected under conservative inference) must be burned down before v1.0.0;
every gcx-owned command ends v0.x with an **explicit** contract (declared
locally or via an explicit shared builder), so every provider owner
reviews classifications for their commands; a handful of long-standing
names change pre-GA (one-time breakage in a pre-stability period,
supported by the v1 migration guide's old→new table and updated published
documentation); the vocabulary registry
adds a review step for genuinely new operations.

Follow-up work: encode the contract in code (metadata package + shared
resolver + CI ratchet + catalog + versioned surface); run the migration
census; execute provider-sized rename batches; empty the baseline; remove
the bootstrap-inference machinery at convergence; add the GA gate.
