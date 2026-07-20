# ADR: Command Operation Semantics and Pre-GA Naming Convergence

**Created**: 2026-07-16
**Revised**: 2026-07-17 (maintainer feedback deferred the executable
per-command contract and made the semantic model a review rubric — see
"Deferred, not rejected" below; this revision additionally **proposes**
an allowed-operation registry with a lexical CI check)
**Updated**: 2026-07-20 (maintainer PR review answered most open
questions — recorded in the rollout plan's decision table; §8 gained a
plain-language restatement after the maintainer asked what the
`list-<subject>` refinement meant; per Slack sync the same day, this ADR
is **alignment only** — implementation does not block on its merge or
acceptance, and the registry + lexical CI PR ships last)
**Status**: proposed
**Supersedes**: none
**Amends on acceptance**: [cross-signal ADR](../signal-provider-ux/001-cross-signal-command-consistency.md) §6 "Aliases and clean breaks" (compatibility policy for future renames only)

<!-- Status lifecycle: proposed -> accepted -> deprecated | superseded -->

## Context

gcx exposes hundreds of runnable leaf commands across its provider and
utility surfaces. The command vocabulary evolved organically: a fresh
build and full command-tree walk shows the five standard CRUD verbs
already cover a majority of the surface, while the remaining commands end
in a long tail of dozens of distinct spellings — most used by barely one
command each. Exact numbers are deliberately not recorded here (volatile
counts go stale; see the documentation-maintenance policy): the
**generated live inventory is authoritative**, and the rollout plan §5
ships the classifier that reproduces the partition deterministically. The
same user intent is spelled
differently across providers (`get`, `show`, `inspect`, `describe-table`),
some leaves are nouns whose behavior is not discoverable from the name
(`kg meta logs`), and sub-resource addressing is inconsistent. For a CLI
that is driven by both humans and AI agents — which discover the surface
by walking `--help` as a decision tree and embed exact invocations in
skills — this is a discoverability and automation-contract defect, not a
cosmetic one.

Prior art inside the repo: [CONSTITUTION.md § CLI Grammar](../../../CONSTITUTION.md)
defines `$AREA $NOUN $VERB` as the default grammar with a closed
single-token top-level set; the pre-amendment CONSTITUTION.md
§ Provider Architecture *forbids* provider-only resources from
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

Maintainer review of the first draft (2026-07-17) set the shape of this
revision: the semantic reasoning is valuable **as a rubric for reviewing
the existing surface**, but designing and embedding a universal
machine-readable per-command contract is more design work than pre-v1
naming convergence needs. This revision therefore uses the rubric to
classify the surface (with LLM assistance where useful) and converges the
names before v1.0.0. The maintainer also said he was **open to** a
registry of allowed verbs with CI catching out-of-list additions; this
proposal **recommends adopting** that registry with a lexical CI check as
the durable guardrail — the registry remains a proposed decision (rollout
D10), not maintainer-directed. A full executable contract
remains a candidate follow-up design (see "Deferred, not rejected").

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

### 1. Operation meaning comes from user-visible behavior

A command's operation is determined by its **user-visible subject,
addressability, result cardinality, and side effects**. It MUST NOT depend
on HTTP method, API path or version, provider-adapter registration, or
transport.

Consequences of this invariant: whether a resource happens to be registered
as a typed adapter is irrelevant to its verb; a genuine read-one is `get`
regardless of how it is implemented; command names must not change when a
provider migrates from `/api` to `/apis`.

### 2. The review rubric

For this phase, the semantic model is a **rubric for reviewing commands**
— applied by humans (with LLM assistance) during the pre-v1
classification and in ordinary PR review — not a set of fields each
command must declare. Reviewing a command name means asking:

- **Operation** — what does it actually do? Which approved operation
  (§§3–7) matches the observed behavior?
- **Subject** — what does it act on, and does the name say so?
- **Spelling** — is the operation direct (`list`), a compound
  (`list-alerts`), or an approved shorthand (`labels`)? Shorthands and
  protocol-family exceptions are legal only when governed (§§4, 3, 7).
- **Addressing** — what identity does it take, and does the command's
  position in the tree match the identity's type (§8)?
- **Result** — does it return one subject, a collection, a report, or
  something else, and is that consistent with the operation's meaning?
- **Effect** — can it change or destroy anything? A name or help text
  that presents a mutating or destructive command as a harmless read is a
  defect (see the `gcx api` and `config unset` notes in §7).

These are review lenses, not mandatory metadata. Not every lens needs a
written answer for every command — they exist so that naming arguments
are made in terms of observable behavior ("this returns a collection, so
it is a `list`") rather than taste.

Worked examples: `slo definitions list` enumerates SLO definitions — a
plain `list`. `metrics labels` is an approved signal shorthand whose real
semantics are "list label names" (§4). `kg entities inspect <entity>`
returns an RCA timeline plus related entities — a genuine diagnostic view
(§5), not a `get` in disguise. `gcx resources get [RESOURCE_SELECTOR]...`
is a ratified protocol-family exception (§3) that may return one item or
a collection.

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
- `upsert` and `push` are both create-or-update operations, distinguished
  by **user workflow**, never by transport, backend implementation, or
  atomicity:
  - `upsert` is the **direct single-entity** workflow: one invocation
    creates the subject when absent or updates/replaces it when present,
    without requiring the caller to choose create versus update.
    Splitting a true upsert into `create`/`update` is rejected: it would
    falsely promise existence checks and introduce read-then-write races.
  - `push` is the **manifest (GitOps) apply** workflow: it takes selected
    local manifests, potentially covering many resources, and creates or
    updates each supplied resource (per the
    [push safety doctrine](../../design/safety.md)), reporting a
    pipeline/summary result. It does **not** delete remote resources
    absent from the manifest set — it applies what it is given; it does
    not reconcile the whole remote collection.
  Whether a backend implements either through one HTTP call or several is
  observed evidence recorded during classification — it is never the
  definition. `alert templates upsert` is kept under this definition; its
  live `create`/`update`/`apply` aliases misrepresent the semantics and
  are **proposed for removal** (awaiting maintainer approval, decision D6)
  in the alert provider's migration batch.
- `pull` is the manifest counterpart of `push` (remote → local files).

**Generic resource-tier exception (protocol family).** The Kubernetes-style
resource family is an explicit exception: `gcx resources get
[RESOURCE_SELECTOR]...` uses **selector addressing** and may return an item
or a collection (`dashboards`, `dashboards/foo`, `dashboards/foo,bar`,
multiple kinds). This mirrors kubectl and is already published in Grafana's
Git Sync documentation. The universal read-one definition of `get`
explicitly does not apply to this family.

### 4. Query operations and signal shorthands

`query` executes a user-supplied data query; `search` performs discovery of
matching subjects. The signal nouns `labels`, `series`, `metrics`, and
`metadata` are **approved surface shorthands** mapped to their real
list/query semantics. They are not independent semantic operations, and
they are not violations: the shorthand set is closed and governed. This
resolves the tension between the "last word identifies the action" rule
and the ratified signal-tier command set.

**`profile-types` → `list-types` (decided).** `profile-types` was never in
the closed shorthand set. Per maintainer feedback (2026-07-17) it is
renamed to **`list-types`** — the compound spelling of `list` under the
`list-<subject>` rule (§8): profile types are a discovery/catalog facet of
the datasource (scoped by `--datasource`, not independently addressable).
The rename covers **both** mount points of its one shared builder
(`profiles profile-types` and `datasources pyroscope profile-types`) and
executes as a clean rename in an early migration batch. The acceptance
commit records this outcome here, in the cross-signal ADR, and in the
naming guide's vocabulary table.

**Hidden-write caveat (current behavior, stated truthfully).** Several
datasource reads best-effort persist an auto-discovered datasource UID to
configuration (`ResolveAndSaveDatasource` in
`internal/datasources/query/resolve.go`, called from e.g.
`internal/datasources/pyroscope/profile_types.go` and
`internal/datasources/prometheus/labels.go`), which changes future target
resolution. This ADR takes no position on changing that behavior — any
pure-resolver/configuration refactor is separate, out-of-scope work, not
designed here. What this decision requires is honesty: documentation,
help text, and the classification worksheet must describe these reads as
they are — they are not free of persistent side effects today.

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
so it is not canonical here (other CLIs use it coherently; gcx never did).
`summary` is not canonical; existing `summary` commands are classified
during migration as `stats`, `status`, or `report` according to their real
output. Neither `describe` nor `summary` is declared "universally
nonstandard" — Grafana's MCP surface offers **surface-name evidence**
(`describe_clickhouse_table`, `describe_athena_table`,
`get_dashboard_summary`) that such spellings appear where the output is
genuinely a schema or composite view; this is evidence about naming, not a
claim that `summary` is an independent semantic operation. The rule is the
materially-different-output test, applied per command.

`kg entities inspect` is ratified as a valid diagnostic view (its output is
an RCA timeline plus related entities; its own help text distinguishes it
from property reads).

### 6. Domain operations

Domain verbs (examples: `acknowledge`, `silence`, `escalate`, `open`,
`close`, `resolve`, `restore`, `run`, `deploy`, `validate`, `sync`) are
the right shape where CRUD would misrepresent the behavior — you close an
incident, you do not "update" it closed. Domain operations MUST be
entries in the governed **allowed-operation registry** (§10): each entry
is an operation with a precise written definition, added by a normal
reviewed PR to the registry, not a constitutional amendment per verb.
**This ADR ratifies the governance model only — no individual domain
operation is ratified by this document**; the initial registry lands as a
separately approved diff (see the rollout plan), with inverse pairs
(`unacknowledge`, `unsilence`, `unresolve`) reviewed together with their
base verbs. A token matching a registry name is not itself compliance:
human review decides whether a command's observed behavior fits the
registered definition.

### 7. Utility operations

Commands that operate on the CLI itself or on the connection — not on
Grafana resources — are reviewed with the same rubric. The table below is
the **reviewed utility set**, not an exhaustive census of every
CLI-utility command: it covers the commands this decision ratifies as
canonical exceptions or maps to entity semantics. Other CLI-utility
commands (`config check`, the `dev` tools, `agent` skills management, …)
go through the ordinary registry/classification path. One row per
**runnable** command, derived from its actual `Use` and observed behavior;
non-runnable groups (e.g. the top-level `setup` group, whose only runnable
child is `setup status`) are not operations:

| Runnable command | Semantic operation (surface spelling) | Notes |
|------------------|----------------------------------------|-------|
| `login [CONTEXT_NAME]` | `login` | durably writes local credentials and context config |
| `setup status` | `status` (view op, §5) | read-only report |
| `version` | `version` | read-only |
| `commands` | `commands` (emit the machine-readable catalog) | read-only |
| `help-tree [COMMAND...]` | `help-tree` | read-only; optional variadic command paths |
| `api PATH` | **`request`** (spelled `api`) | see the safety note below |
| `config view` | `get` (spelled `view`) | reads the config singleton |
| `config current-context` | `get` (spelled `current-context`) | |
| `config path` | `list` (spelled `path`) | enumerates the config-source entries contributing to the merged configuration |
| `config list-contexts` | `list` (compound `list-contexts`) | |
| `config use-context [CONTEXT_NAME]` | `update` (spelled `use-context`) | mutates local config |
| `config set PROPERTY_NAME PROPERTY_VALUE` | `update` (spelled `set`) | mutates local config |
| `config unset PROPERTY_NAME` | `update` (spelled `unset`) | see the safety note below |
| `config edit [type]` | `update` (spelled `edit` — interactive) | unrestricted editing can remove whole contexts, same as `unset` |

**`request` is the semantic operation; `api` is its surface spelling** —
naming the operation after the transport surface would violate §1. The
`config` family's approved protocol-family exceptions are exactly the
closed list above: `view`, `use-context`, `current-context`, `path`,
`list-contexts`, `set`, `unset`, `edit` — kubectl parity, each mapped to
its real entity semantics. `config check` is NOT in the approved list: its
operation (`check`) is a registry candidate and the command awaits the
registry diff. The runnable `instrumentation setup <cluster>` is likewise
NOT a utility — it is a domain operation acting on a cluster and goes
through the domain-registry review like any other domain candidate.

**Safety notes (behavior, not metadata):**

- **`gcx api` can mutate or delete any API object.** The raw escape hatch
  is not a harmless utility: depending on method and body it reads,
  mutates, or destroys arbitrary resources, and its result shape is
  whatever the endpoint returns. It MUST never be presented — in help
  text, catalog metadata, or agent annotations — as read-only.
- **`config unset` can destroy real state.** `gcx config unset
  contexts.foo` deletes an entire context from configuration, and its
  keychain-backed secrets are left orphaned (reconciliation only visits
  surviving contexts). `config edit` can do the same interactively. Help
  text and documentation must reflect that these are not ordinary field
  tweaks.

### 8. Addressability and the `list-<subject>` rule

**In plain terms** (restated 2026-07-20 after maintainer review asked
what this section means): it is the rule for choosing between
`gcx <area> things list` and `gcx <area> list-things`.

- If a *thing* has its own ID — you can fetch exactly one with that
  ID — it gets its own command group: `k6 load-tests list`,
  `k6 load-tests get <id>`.
- `list-things` is reserved for the two cases where a noun group cannot
  work: **(a)** plain value enumerations with no ID of their own
  (`cloudwatch list-regions` — there is no `region get`; a region is
  just a value), and **(b)** sub-lists addressed by the *parent's* ID
  (`alert-groups list-alerts <group-id>` — the positional you pass is
  the group's ID, not an alert's).
- "This subject only has a list command today" is **not** a reason to
  spell it `list-things`: the day the subject grows a `get`, you face a
  breaking rename or a mixed shape. The test is "does it have its own
  ID?", never "how many verbs does it have today?".

Why the two conditions matter: without them, the generic allowance makes
`list-<anything>` a legal spelling everywhere, and providers grow flat
verb-noun leaves instead of resource groups — recreating exactly the
leaf-token sprawl the operation registry exists to stop.

The precise statement follows. The command path indicates the type of
the first required positional identity:

- Identity is the **parent's** → operation-subject compound under the
  parent: `$PARENT $OPERATION-$CHILD $PARENT_ID`
  (`alert-groups list-alerts <group-id>`).
- Identity is the **child's** → the child is independently addressable
  and warrants a child resource group (`k6 load-tests get <id>` — load
  tests belong to projects, but a test is addressable by its own ID, so
  load tests get their own group; parent scoping is a filter flag). The
  basis is stable addressability/identity — not how many child operations
  happen to exist today (operation count is a heuristic, not the rule).
- Catalog children with no parent identity may keep a noun group with
  `list` (`incidents severities list`).
- Selector, optional, variadic, multiple, or flag-supplied identities
  cannot be read off the syntax; commands using them state the identity's
  meaning explicitly in help text, and reviewers classify them with the
  rubric rather than by syntax (e.g. `instrumentation services get
  <cluster> <namespace> <service>`; `slo definitions status [UUID]`
  returns one or many depending on the optional argument).

**The `list-<subject>` rule.** `list-<subject>` is the approved compound
spelling of `list` in two situations:

1. the subject is a **discovery/catalog facet** that is not an
   independently addressable resource group — e.g. the datasource-scoped
   discovery commands `cloudwatch list-namespaces` / `list-metrics` /
   `list-dimensions` / `list-regions` / `list-accounts`, `athena
   list-catalogs` / `list-databases` / `list-tables`, `clickhouse
   list-tables` (all scoped by `--datasource`, no parent positional); or
2. the subject is a **parent-scoped collection** that is not
   independently addressable — e.g. `alert-groups list-alerts
   <group-id>`, `services list-operations <service>`.

Use `<subject> list` when the subject is an independently addressable
resource group. "No other verbs exist for this subject today" is NOT by
itself a justification for the compound — addressability is. The
allowed-operation registry contains **`list`, once**; it does not contain
an entry per `list-<subject>` spelling (§10).

Attribution: the generic `list-<subject>` allowance follows the
maintainer's own suggestion (2026-07-17); the two addressability-based
conditions above are **this proposal's refinement** of it. The
maintainer's 2026-07-20 review asked what the refinement meant; the
plain-language restatement at the top of this section is the answer,
and the refinement still awaits his explicit yes (rollout plan §1a Q3).

Worked classification example (illustrative, not a decided rename):
`agento11y experiments scores <run-id>` lists the scores produced by one
experiment run — a noun leaf whose positional is the **parent's** ID.
Under this section the suggested fix is the compound spelling
(`experiments list-scores <run-id>`); the agento11y owner confirms or
overrides that in their migration batch.

### 9. Pre-GA naming convergence (normative)

gcx v1.0.0 ships a reviewed, canonical command surface. The v1 gates are:

1. **Reviewed canonical command names**: every active runnable command has
   been classified against this ADR (OK / suggested fix / human review)
   and carries its reviewed canonical name.
2. **Noncanonical paths and aliases resolved**: commands whose names
   misrepresent their subject, addressability, result, or side effects
   are renamed or removed before v1.0.0; no migration/deprecation aliases
   and no compatibility forwarders ship in v1.0.0. An intentional
   **permanent synonym** may remain only with an explicit approved
   disposition (e.g. the traces `search`/`tags` aliases declared in the
   cross-signal ADR).
3. **Synchronized first-party surfaces**: documentation, examples, skills,
   agent annotations, and generated references use canonical paths only.
4. **Complete migration dispositions**: the v1.0.0 release notes /
   migration guide carry a disposition for **every changed or removed
   primary command and alias** — each resolves to exactly one of
   `old path → new path`, `old path → removed; use <replacement>`, or
   `old path → removed; no replacement`; group aliases use one prefix
   mapping (e.g. `gcx k6 test …` → `gcx k6 load-tests …`) rather than
   expanding every descendant. Published Grafana documentation
   referencing gcx commands (e.g. the Git Sync export guide) is updated
   at release time.

Commands classified "human review" must receive an explicit human
disposition before v1.0.0 — deferring the decision is a disposition only
if the current name is explicitly kept. Silence is never a disposition.

Verification responsibility is split. The classification worksheet plus
owner approval verifies the **semantic** side of gates 1 and 2: rubric
review, canonical-name decisions, and every alias disposition. The
allowed-operation registry's lexical CI check (§10) verifies the
**spelling** side: vocabulary tokens, scoped exceptions, and — via its
temporary deviation list, which must be empty at the v1 tag — closure of
the remaining spelling debt. Lexical CI never verifies semantic fit.

Migration does not mean blindly renaming every unusual command. Each
current command resolves through exactly one of four outcomes:
**keep** (already canonical), **ratify** (intentional canonical exception
— recorded in this ADR's exception lists or the operation registry),
**rename** (name misrepresents the behavior), or **remove** (obsolete or
duplicated).

Intentional canonical exceptions (closed lists): the single-token
top-level commands enumerated in CONSTITUTION.md § CLI Grammar (which this
decision extends to include `commands`, `help-tree`, and `api` — they
exist today and must not be simultaneously canonical exceptions and
constitutional violations); kubectl-parity `config` commands — exactly the
closed list of §7 (`config check` is a registry candidate awaiting the
registry diff, not an approved exception); Cobra built-ins (`help`,
`completion <shell>`); the resource-tier protocol family (§3); the signal
shorthands (§4).

### 10. Enforcement: allowed-operation registry + lexical CI

Ordering (maintainer, PR review 2026-07-20): approved in principle, but
"the most important thing for v1 is that the current commands are
consistent" and "put the registry PR last; we don't want to block on
it" — the registry + lexical CI PR is the **final** rollout step, seeded
from the converged post-migration surface, and gates nothing before it
lands.

- **The registry.** A reviewed list of allowed operations, each with a
  precise one-line written definition (what the operation means, in
  user-visible terms). An operation is registered **once** — not once per
  command or per compound spelling. Additions are normal reviewed PRs;
  the initial registry contents land as a separately approved diff (see
  the rollout plan).
- **The lexical CI check.** Lands with the registry implementation PR.
  The algorithm: walk the built Cobra tree; for every **runnable** node
  (`cmd.Runnable()`) — including hidden runnable commands and Cobra
  built-ins — validate the command's canonical terminal token, and
  validate any aliases attached to that runnable node as **alternate
  terminal tokens**. A token passes when it is a globally allowed
  operation, an `<operation>-<subject>` compound whose stem is an allowed
  operation, or covered by a scoped exception. Aliases attached only to
  **non-runnable group nodes are not operation-validated** — a group
  alias is a path prefix, not an operation — but every alias on every
  node is inventoried and dispositioned through the classification
  worksheet.
- **Three enforcement categories.** (1) **Globally allowed operations**,
  each with a written semantic definition. (2) **Permanent scoped
  exceptions** — exact-path or command-family-scoped: the closed
  single-token top-level utilities of §9/CONSTITUTION, the signal
  shorthands (valid only within the signal-provider and per-datasource
  query families — `labels` or `metrics` elsewhere is NOT covered), the
  kubectl-parity `config` family spellings, `resources get`, and Cobra
  built-ins. Scoped means scoped: approving `set` inside the `config`
  family does not permit an unrelated `$AREA $NOUN set`. The exact
  path/family scope of each exception is enumerated in the registry
  diff. (3) **Temporary exact-path deviations** awaiting
  their migration batch — this list must be empty before v1.0.0. Honest
  limitation: a hermetic Go test cannot prove the deviation list only
  shrinks (a contributor could add an entry in the same PR) — shrink-only
  is a **review rule on deviation-list diffs**; the terminal guarantee is
  the empty list at the v1 tag. No base-branch comparison CI is part of
  this proposal.
- **What lexical CI does NOT do.** It validates spelling only. It does
  not — and does not claim to — validate a command's effect, result
  cardinality, addressing, behavior, or whether that behavior fits a
  registered definition. Semantic fit is a **human review** judgment made
  with the §2 rubric via the classification worksheet. (A future
  executable contract could close that gap; see "Deferred, not
  rejected".)
- **Alias governance.** Permanent synonyms and compatibility paths are
  distinct concepts. Every live alias — on runnable commands and on
  groups — gets an explicit disposition during its provider's migration
  batch: approved permanent synonym, or removal. Aliases that
  misrepresent semantics (e.g. the CRUD-named `create`/`update`/`apply`
  aliases on the true upsert) are naming defects to resolve.
- **Local ownership.** New commands get their names reviewed against the
  rubric in ordinary PR review. A permanent central path-keyed metadata
  registry is rejected: a recent single-provider rename (aio11y →
  agento11y, #990) had to churn the entire path-keyed annotation map —
  path-keyed registries are rename hazards by construction.

### 11. Compatibility policy

v1.0.0 is deliberately the clean compatibility boundary. **Noncanonical
command paths are renamed or removed during v1 development, with no
deprecated aliases and no compatibility forwarders**: gcx is in public
preview, and the preview period is exactly when clean breaks are
permitted. Migration support is documentation, not shims — the v1.0.0
release notes / migration guide carry the complete migration
dispositions of §9 gate 4 (rename mappings and removals, for commands
and aliases alike), and published Grafana documentation that references
gcx commands (e.g. the Git Sync export guide) is updated at release
time. Intentional permanent synonyms with an approved disposition (§9)
are not compatibility paths and may ship.

After v1.0.0, command paths are stable interfaces and the normal
deprecation policy applies:

- A minor release MAY add a canonical replacement and deprecate the old
  path while keeping it functional, with the replacement and an
  earliest-eligible removal version stated in the release notes.
- Removal occurs no earlier than the next major version, requires
  explicit approval, and requires zero first-party references.
- Any post-GA compatibility path is expected to remain until the next
  major. How such a path would be implemented is deliberately not
  designed in this proposal.

On acceptance, this section supersedes the cross-signal ADR's §6 pre-GA
clean-break stance for any path renamed under this decision going forward
(a scoped amendment — that ADR's already-executed renames are
unaffected).

### Explicitly rejected

- "The last word is always a verb" as the enforcement rule (word-shape
  grammar): rejected in favor of reviewed operation semantics — it
  misclassifies `status`, `labels`, and compounds like `list-tables`, and
  cannot express subjects, addressing, or effects.
- Universal `get`-returns-one-item: rejected; the resource-tier selector
  family is a documented protocol exception.
- `show`/`describe`/`search` as the mandated verbs for provider-only
  resources (pre-amendment CONSTITUTION text): rejected and replaced by §1.
- A permanent central path-keyed metadata registry: rejected (rename
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

### Deferred, not rejected: a full machine-readable command contract

The first draft of this ADR specified a universal executable per-command
contract (declared operation/subject/effect/addressing/result tuples,
bootstrap inference, a migration census with CI ratchets, a shared
resolver, catalog integration, and a versioned command-surface JSON).
Maintainer review deferred all of it: it is a possible future design — a
natural follow-up after AI week — **not** a v1 requirement, and nothing
in this ADR depends on it.

If pursued, it needs its own ADR, and that design must resolve at least:
payload/file-derived identity (e.g. upsert identity read from `-f`
payloads, `push FILE...`); effect classification for `push`/`pull`
reconciled with the [push safety doctrine](../../design/safety.md);
the relationship between effect classification and confirmation policy;
alias representation; and mixed-addressing command groups. Every proposed
field must name a concrete consumer and be validated by controlled agent
evaluations before it ships. In the meantime, **no partial effect/danger
metadata ships** — publishing unverified safety metadata is worse than
publishing none.

### Out of scope

Output-format defaults and codecs; typed-adapter (TypedCRUD) migrations;
backend capability modeling (native/emulated/unsupported), pagination,
rate limiting, retry/idempotency, and authentication standardization —
tracked separately as client-platform work; k6 run-history consolidation
except where a semantic rename requires it.

## Consequences

Easier: naming arguments become behavior-based and checkable against a
written rubric and registry; new commands have one vocabulary to follow;
the lexical CI check stops new out-of-vocabulary verbs from landing
silently; renames happen once, pre-GA, with complete migration
dispositions; the v1 surface is reviewed and predictable for humans and
agents.

Harder / costs: the existing surface must be inventoried and classified
(one pass over the live tree, LLM-assisted, owner-reviewed); provider
owners must approve renames, removals, exceptions, human-review and
alias dispositions in their areas; a handful of long-standing names
change pre-GA (one-time breakage in a pre-stability period, supported by
the v1 migration guide's disposition table and updated published
documentation); the registry adds a review step for genuinely new
operations.

Follow-up work: generate the inventory and classification; land the
allowed-operation registry and its lexical CI check; run the adaptive
`show` → `list` pilot; execute provider-sized rename batches; write the
migration guide. The full machine-readable contract, if wanted, is a
separate post-AI-week design (see "Deferred, not rejected").
