# ADR: Command Operation Contract and Pre-GA Surface Convergence

**Created**: 2026-07-16
**Status**: proposed
**Supersedes**: none

<!-- Status lifecycle: proposed -> accepted -> deprecated | superseded -->

## Context

gcx exposes ~487 runnable leaf commands across 16 providers. The command
vocabulary evolved organically: the ~487 leaves end in 142 distinct final
tokens, while the five standard CRUD verbs already cover 274 of them. The
same user intent is spelled differently across providers (`get`, `show`,
`inspect`, `describe-table`), some leaves are nouns whose behavior is not
discoverable from the name (`kg meta logs`), and sub-resource addressing is
inconsistent. For a CLI that is driven by both humans and AI agents — which
discover the surface by walking `--help` as a decision tree and embed exact
invocations in skills — this is a discoverability and automation-contract
defect, not a cosmetic one.

Prior art inside the repo: [CONSTITUTION.md § CLI Grammar](../../../CONSTITUTION.md)
defines `$AREA $NOUN $VERB` and a closed bare-verb set; CONSTITUTION.md
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
the legacy `/api` surface is being migrated with no exact `/apis`
replacement for every endpoint. Command names must survive that backend
migration. Grafana's Git Sync documentation already instructs users to
automate with `gcx resources pull`: command paths are automation contracts
today. The Grafana App SDK models Get/List/Create/Update/Patch/Delete plus
custom subresource routes; Grafana's MCP surface uses `list_*`/`get_*`/
`query_*`/`search_*` and retains `describe_*` where the result is genuinely
a schema/composite view.

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
| Operation | `list`, `get`, `create`, `update`, `delete`, `patch` (reserved), `upsert`, `push`, `pull`, `query`, `search`, view ops (§5), domain ops (§6) | what it does |
| Subject | free noun (`recommendations`, `alerts`, `labels`, …) | what it acts on |
| Surface form | `canonical` \| `compound` \| `approved_shorthand` \| `protocol_exception` | how it is spelled |
| Category | `entity` \| `manifest` \| `query` \| `view` \| `domain` \| `utility` | operation family |
| Effect | `read_only` \| `mutating` \| `destructive` | side effects |
| Addressing | `none` \| `singleton` \| `subject` \| `parent` \| `selector` | what identity it takes |
| Result shape | `item` \| `collection` \| `item_or_collection` \| `report` \| `stream` \| `mutation` \| `none` | what comes back |
| Lifecycle | `active` \| `deprecated` (+ replacement, earliest-eligible removal) | stability state |
| Contract source | `explicit` \| `inferred` \| `unresolved` | where the contract came from |

Category and Effect are deliberately separate: `query`/`view`/`domain`
describe operation families; side effects are an orthogonal axis.
`singleton` is an **addressing** concept, not a result shape. Logical
cardinality is user-visible, not container-visible: a `get` result may
contain nested arrays.

Examples:

- `slo definitions list` → {list, definitions, canonical, entity, read_only, none, collection}
- `irm oncall alert-groups list-alerts <group-id>` → {list, alerts, compound, entity, read_only, parent, collection}
- `metrics labels` → {list, labels, approved_shorthand, query, read_only, none, collection}
- `kg entities inspect <entity>` → {inspect, entities, canonical, view, read_only, subject, report}
- `resources get [RESOURCE_SELECTOR]...` → {get, resources, protocol_exception, entity, read_only, selector, item_or_collection}

### 3. Entity operations (normal provider/product behavior)

- `list` enumerates zero or more independently meaningful subjects.
- `get` retrieves one addressable subject or singleton in its stable
  structured representation.
- `create` requires absence; `update` requires existence; `delete` removes
  an existing subject.
- `patch` is **reserved** for partial modification (the App SDK exposes it
  separately from update); no command uses it until a real partial-update
  surface exists.
- `upsert` is permitted only when the backend genuinely provides
  create-or-update semantics (single endpoint, no existence distinction).
  Splitting a true upsert into `create`/`update` is rejected: it would
  falsely promise existence checks and introduce read-then-write races.
  Evidence: the alert notification-template provisioning API is a single
  PUT keyed by template name.
- `push`/`pull` are the manifest (GitOps) operations, unchanged.

**Generic resource-tier exception (protocol family).** The Kubernetes-style
resource family is an explicit exception: `gcx resources get
[RESOURCE_SELECTOR]...` uses **selector addressing** and may return an item
or a collection (`dashboards`, `dashboards/foo`, `dashboards/foo,bar`,
multiple kinds). This mirrors kubectl and is already published in Grafana's
Git Sync documentation. It is modeled as
{get, protocol_exception, selector, item_or_collection} — the universal
read-one definition explicitly does not apply to this family.

### 4. Query operations and signal shorthands

`query` executes a user-supplied data query; `search` performs discovery of
matching subjects. Signal nouns — `labels`, `series`, `metrics`,
`metadata` — are **approved surface shorthands** mapped to their real
list/query semantics ({list|query, <noun>, approved_shorthand, query}).
They are not independent semantic operations, and they are not violations:
the shorthand set is closed and governed. This resolves the tension between
the "last word identifies the action" rule and the ratified signal-tier
command set.

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

`show` is not canonical (it does not identify whether the result is an
item, collection, or computed view). `summary` is not canonical; existing
`summary` commands are classified during migration as `stats`, `status`, or
`report` according to their real output. Neither `describe` nor `summary`
is declared "universally nonstandard" — Grafana's MCP surface contains
legitimate describe and qualified-summary operations; the rule is the
materially-different-output test, applied per command.

`kg entities inspect` is ratified as a valid diagnostic view (its output is
an RCA timeline plus related entities; its own help text distinguishes it
from property reads).

### 6. Domain operations

Domain verbs (`acknowledge`, `silence`, `escalate`, `open`, `close`,
`resolve`, `restore`, `run`, `deploy`, `validate`, `sync`, …) remain valid
where CRUD would misrepresent the behavior — you close an incident, you do
not "update" it closed. Domain operations MUST be entries in a governed,
reviewed vocabulary registry (a normal reviewed PR to the registry, not a
constitutional amendment per verb). Inverse pairs (`unacknowledge`,
`unsilence`, `unresolve`) are ratified together with their base verbs.

### 7. Addressability

The command path indicates the type of the first required positional
identity:

- Identity is the **parent's** → operation-subject compound under the
  parent: `$PARENT $OPERATION-$CHILD $PARENT_ID`
  (`alert-groups list-alerts <group-id>`).
- Identity is the **child's** and the child has multiple operations → a
  child resource group (`experiments trials get-scores <trial-id>`).
- Catalog children with no parent identity may keep a noun group with
  `list` (`incidents severities list`).
- Selector, optional, variadic, multiple, or flag-supplied identities
  cannot be derived from syntax and REQUIRE explicit contract metadata
  (e.g. `instrumentation services get <cluster> <namespace> <service>`;
  `slo definitions status [UUID]` returns one or many depending on the
  optional argument).

Syntax alone cannot prove an identity's resource type; explicit semantic
metadata is the source of truth for ambiguous cases.

### 8. Pre-GA surface convergence (normative)

gcx v1.0.0 will ship a fully classified canonical command surface:

- Every active runnable command MUST conform to this operation contract or
  be an intentional canonical exception.
- Commands whose names misrepresent their subject, addressability, result,
  or side effects MUST be renamed or removed before v1.0.0.
- The temporary legacy-contract baseline is migration scaffolding only and
  MUST be empty before v1.0.0.
- First-party documentation, examples, skills, annotations, and generated
  references MUST use canonical paths.
- Broad hidden forwarders MUST NOT ship in v1.0.0 merely to preserve
  pre-GA naming. A noncanonical path may ship in v1 only through an
  explicit, narrowly scoped maintainer-approved compatibility exception
  carrying evidence of external dependency, replacement metadata, an
  owner, and a removal policy.

Migration does not mean blindly renaming every unusual command. Each
current command resolves through exactly one of four outcomes:
**keep and annotate** (already canonical), **ratify** (intentional
canonical exception — recorded in this ADR's exception lists or the
vocabulary registry), **rename** (name misrepresents the contract), or
**remove** (obsolete or duplicated).

Intentional canonical exceptions (closed lists): the bare top-level verbs
already enumerated in CONSTITUTION.md § CLI Grammar; kubectl-parity
`config` commands (`current-context`, `use-context`, `view`, `path`,
`list-contexts`); CLI-utility commands (`version`, `commands`,
`help-tree`, `api`); Cobra built-ins (`help`, `completion <shell>`); the
resource-tier protocol family (§3); the signal shorthands (§4).

### 9. Enforcement

- **Local metadata ownership.** New commands declare their contract beside
  their constructor or inherit it from a shared builder. A permanent
  central path-keyed contract registry is rejected: a recent single-provider
  rename churned 61+61 lines in the existing path-keyed annotation map —
  path-keyed registries are rename hazards by construction.
- **One shared resolver.** CI enforcement, the machine-readable command
  catalog, and command-surface generation all consume the same resolution
  logic; there is exactly one definition of a command's contract.
- **Conservative inference, bootstrap only.** Inference from command syntax
  exists to classify the existing tree cheaply and is limited to obvious
  cases: a no-identity `list`; a `get` with exactly one simple required
  positional; a no-argument `get` (singleton); a simple operation-subject
  compound with exactly one required parent positional. Optional/variadic/
  multiple/flag-supplied identities, selector syntax, alternatives in
  `Use`, views, and domain actions are never inferred.
- **Fail closed.** A command with unknown operation or unknown addressing
  either carries explicit metadata or an exact-path entry in the temporary
  legacy baseline; otherwise CI fails.
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

### 10. Compatibility policy

Because v1.0.0 is the first stable boundary, **clean pre-GA
canonicalization is the default**: pre-GA renames ship without
compatibility shims unless a final v0.x migration release is planned, in
which case a small, enumerated set of already-shipped paths may carry
hidden forwarders with stderr warnings for that release only. Forwarders,
where used: same-parent moves only for the generic mechanism; fresh
command construction; the deprecation warning wraps Run/RunE and writes to
stderr (never Cobra's built-in `Deprecated`, which writes through the
command output stream and would corrupt structured stdout); structured
stdout is preserved byte-for-byte; cross-parent moves require dedicated
behavioral parity testing. Removal versions are the *earliest eligible*
removal, not automatic deletion; removal additionally requires explicit
approval and zero first-party references.

After v1.0.0: command paths are stable interfaces; non-additive changes
occur only at a major version with the same forwarder discipline.

This supersedes the pre-GA clean-break stance of the cross-signal ADR for
any path renamed under this contract going forward (that ADR's
already-executed renames are unaffected).

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
every provider owner reviews classifications for their commands; a handful
of long-standing names change pre-GA (one-time breakage in a pre-stability
period, softened by a final v0.x migration release if planned); the
vocabulary registry adds a review step for genuinely new operations.

Follow-up work: encode the contract in code (metadata package + shared
resolver + CI ratchet + catalog + versioned surface); run the migration
census; execute provider-sized rename batches; empty the baseline; add the
GA gate.
