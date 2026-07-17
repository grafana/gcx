# Rollout Plan: Command Operation Semantics and v1 Naming Convergence

> **Date**: 2026-07-16 (revised 2026-07-17 after maintainer feedback)
> **Status**: draft — companion to the [Command Operation Semantics ADR](../adrs/command-operation-contract/001-command-operation-semantics.md)
> **Related issues**: #387 (verb-taxonomy / UX-consistency umbrella — this
> plan is its **naming workstream**; #387 also tracks output-format
> consistency and global `--limit` work per the maintainer's issue
> comment, which this plan does not cover)

The ADR records the decision; this plan carries the migration artifacts:
the maintainer decision table, the rollout sequence, the classification
worksheet, the v1 acceptance checklist, and the initial vocabulary
partition.

The 2026-07-17 revision reflects maintainer feedback: the executable
per-command contract is **deferred** and the semantic model becomes a
**review rubric**. The maintainer said he was **open to** a registry of
allowed verbs with CI catching out-of-list additions; **this proposal
recommends adopting** that registry with a lexical CI check — the
registry and its process remain proposed (D10), not maintainer-directed.
Where a decision below records a maintainer answer, it cites the answer;
everything else is a recommendation going through normal review.

## 1. Maintainer decision table

Each item wants an explicit yes/no (or a correction). "Verified" means
checked against actual command behavior on the current tree.

| # | Decision | Disposition | Notes |
|---|----------|-------------|-------|
| D1 | **Transport neutrality** — genuine read-one is `get`, genuine enumeration is `list`, regardless of adapter registration. Inverts the pre-amendment CONSTITUTION "never `get`/`list`; use `show`/`describe`/`search`" rule. Constitutional change. | Recommended (maintainer feedback agreed meaning comes from user-visible behavior, never the backend) | The old rule leaks implementation tiers to users |
| D2 | **Semantic review rubric + lexical operation registry** replace word-shape grammar — "last word is a verb" becomes the effect of reviewed vocabulary, not the rule. Reframed 2026-07-17 per maintainer feedback: the rubric guides review; the registry + lexical CI are the proposed executable check. **Not** executable per-command contracts. | Recommended | Word-shape misclassifies `status`, `labels`, and compounds like `list-tables` |
| D3 | **Addressability principle** — parent-ID compounds vs child groups by child identity (ADR §8). The earlier *required identity metadata* clause is removed — ambiguous identities are stated in help text and judged in review; formal identity metadata belongs to the deferred contract design. | Recommended | Formalizes the July Slack ruling; same outcomes on every audited case |
| D4 | **Signal shorthands + ADR 023 ratification + `profile-types`** — (a) `labels`/`series`/`metrics`/`metadata` are approved surface shorthands mapped to real list/query semantics; (b) ratifying the cross-signal ADR (023) in full is **bundled with this decision and still awaits maintainer approval**; (c) `profile-types` → **`list-types`** at both mounts of its shared builder — this specific outcome is the maintainer's answer (2026-07-17). | (a)+(b) recommended — pending approval; (c) **decided: `list-types`** | Approving D4 flips ADR 023 to accepted in the acceptance commit |
| D5 | **View vocabulary** — `status`/`timeline`/`inspect`/`diff`/`stats`/`report`; `describe` admitted narrowly per command; `show` and `summary` not canonical (existing `summary` commands reclassified by real output). `kg entities inspect` stays. | Recommended | Verified: `inspect` is a genuine RCA view |
| D6 | **`upsert` and `push` both kept, distinguished by user workflow** (ADR §3): `upsert` = direct single-entity create-or-update in one invocation; `push` = manifest apply — creates or updates each supplied resource (potentially many, no deletion of remote objects absent from the manifests) with a pipeline/summary result. Never defined by transport or atomicity. The alert-template `create`/`update`/`apply` aliases are **proposed for removal** in the alert batch. This answers the maintainer's "what about push? IIUC resources push are upserts" — push does create-or-update per supplied resource; the two commands differ in workflow, not mechanism. | **Recommended — awaiting maintainer approval** | Overrides the earlier audit line-item that wanted upsert split |
| D7 | **v1 gates on naming convergence only** — reviewed canonical names; noncanonical paths and non-approved aliases renamed or removed; docs/skills/annotations synchronized; complete migration dispositions; explicit human disposition of "human review" items (ADR §9). The earlier census-empty/explicit-contract gates are gone. | Recommended | The load-bearing commitment, now scoped to naming |
| D8 | ~~Zero inferred/unresolved contracts at v1~~ — **deferred/superseded**: there are no per-command contracts in this phase, so there is nothing to infer. Belongs to the deferred contract design (ADR "Deferred, not rejected"). | Deferred | |
| D9 | **No final v0.x compatibility release — clean start.** Rename and remove noncanonical command paths during v1 development; ship v1.0.0 with only the canonical surface — no migration/deprecation aliases, no compatibility forwarders (intentional permanent synonyms only with explicit approved disposition). Migration support: the v1 migration guide carries complete migration dispositions (§4 gate 6), and published Grafana documentation referencing gcx commands is updated at release time. | **Maintainer answered yes** (2026-07-17): clean start, "with a table in a migration guide to map" | |
| D10 | **Allowed-operation registry (proposed)** — a reviewed list of operations, each with a written definition; registered **once per operation**, not per occurrence or spelling. Lexical CI validates the terminal tokens of **runnable** commands (and aliases attached to runnable commands as alternate terminal tokens); group aliases are path prefixes, inventoried and dispositioned in the worksheet, never operation-validated. Semantic fit is human review. Includes the **`list-<subject>` rule** (ADR §8). *Existing use does not automatically ratify an operation*; the initial registry lands as a separately approved diff. | Recommended — awaiting approval (the maintainer said he was **open to** a registry; the generic `list-<resource>` allowance follows his suggestion; the **addressability-based conditions are this proposal's refinement**) | Prevents existing tokens becoming standards by accident |
| D11 | **Utility enumeration** — extend CONSTITUTION's closed single-token list with `commands`, `help-tree`, `api` (they exist today; they cannot be both canonical exceptions and constitutional violations). `gcx api` can mutate or delete arbitrary API objects and is never presented as read-only (behavior verified in `cmd/gcx/api/command.go`). | Recommended (normal review) | Constitutional change |
| D12 | **Adaptive pilot** — `logs adaptive patterns show`, `metrics adaptive recommendations show`, `traces adaptive recommendations show` → `list`. Verified: all three return collections (`ListRecommendations`). Honest caveat: the pilot fixes the verb only — logs uses subject `patterns` where metrics/traces use `recommendations`; that cross-signal subject mismatch remains and is flagged for classification, not solved by this rename. | Recommended | |
| D13 | **Tracking granularity + workstream scope** — provider-sized migration tracking issues replace the umbrella issue's original "file individual issues per mismatch" wording (recorded here as an explicit deviation; **pending D13 approval**). This plan is **the naming workstream of #387**: the ADR §9 naming gates complete this workstream, but they do **not** close #387 — the maintainer's issue comment lists further work beyond this PR (output-format consistency review; a global `--limit` with truncation hints). #387's overall scope and closure are not redefined by this plan and need explicit maintainer agreement. | Recommended (normal review) | Process decision |
| D14 | ~~Reads become pure~~ — **withdrawn as a decision.** Reduced to a factual caveat (ADR §4): several datasource reads persist auto-discovered datasource UIDs into config today (`ResolveAndSaveDatasource`), and documentation, help text, and classification must describe that behavior truthfully. Any pure-resolver/configuration refactor is **separate, out-of-scope work** — not designed, decided, or gated here. | Withdrawn (not a decision) | Affected today: e.g. `profiles profile-types`, `metrics labels` paths through `internal/datasources/query/resolve.go` |

### 1a. Open approval questions (explicit — none of these are decided)

Presenting this plan does not depend on resolving these, but none of them
may be treated as already approved:

1. Adopt the allowed-operation registry + lexical CI as the proposed
   first implementation? (D2/D10)
2. Keep `upsert` and remove its misleading `create`/`update`/`apply`
   aliases? (D6)
3. Accept the addressability-based refinement of the `list-<subject>`
   rule? (D10 — the generic allowance follows the maintainer's
   suggestion; the two conditions are this proposal's)
4. Ratify ADR 023 bundled with this decision, or separately? (D4a/b)
5. How does #387 track the output-format-consistency and global
   `--limit` work relative to this naming workstream? (D13)
6. Exact initial operation-registry contents and definitions (separately
   approved diff).

Owner decision (routed to its provider batch, **not** a prerequisite for
approving this ADR): instrumentation `clusters apps` terminology and
list/get population require an explicit Beyla/Instrumentation Hub owner
answer (§3b) — silence is not a disposition.

## 2. Rollout sequence

1. **Revised ADR + governing-doc reconciliation** (this PR, kept draft).
2. **Explicit maintainer approval** of the decision table on the issue.
3. **Acceptance commit**: flips BOTH ADRs — operation semantics (024) and
   the cross-signal ADR (023, whose surface shipped in April but whose
   ratification is bundled with D4) — to `accepted` with dated
   `**Accepted**:` lines and ARCHITECTURE table statuses; writes the
   D4(c) `list-types` outcome into both ADRs and the naming guide's
   vocabulary table; marks ADR 023's stale "needs implementation"
   checklists as historical. Constitutional changes land only in this
   accepted state; the CONSTITUTION naming requirement applies to
   new/modified commands until the registry CI lands.
4. **Inventory + classification**: generate a deterministic inventory
   from the live Cobra tree with two record kinds — a **command record**
   for every active runnable command (including hidden runnable commands
   and Cobra built-ins) and an **alias record** for every alias on every
   node, runnable or not (§3). Classify each command as **OK / suggested
   fix / human review** against the ADR rubric (LLM-assisted,
   human-reviewed). Every *suggested fix* cites the observed source
   behavior it is based on.
5. **Owner approval**: provider/product owners approve renames, removals,
   ratified exceptions, every *human review* disposition, every alias
   disposition, and any *suggested fix* resolved as `keep`.
   Instrumentation's `clusters apps` questions route to the
   Beyla/Instrumentation Hub owners as part of their batch (§3b).
6. **Registry PR**: the allowed-operation registry (operations + written
   definitions, seeded from §5 and the worksheet) as a separately
   approved diff, plus the lexical CI check — the algorithm and scoping
   rules in ADR §10 (runnable-command terminal tokens and their aliases;
   scoped exceptions; a temporary exact-path deviation list that must be
   empty before v1.0.0).
7. **Pilot**: the three adaptive `show → list` renames (D12) — clean
   renames per D9 (no forwarders), updating skills, annotations, docs,
   and generated references atomically. The `profile-types → list-types`
   rename (D4c) rides the same early wave.
8. **Provider-sized migration batches in parallel, all before GA**: each
   command record resolves keep / ratify / rename / remove; every alias
   record resolves keep-synonym / remove; each batch records its
   migration dispositions (§4 gate 6).
9. **Migration documentation**: the complete migration-disposition table
   in the v1 release notes / migration guide — every changed or removed
   primary command and alias mapped to its outcome; published Grafana
   docs updated at release.

## 3. Classification worksheet

A lightweight review artifact, generated fresh from the live Cobra tree —
the sole migration input (the July-1 audit is historical context only and
must not be worked from). The rubric's dimensions (operation, subject,
spelling, addressing, result, effect) are **review lenses, not required
fields** — they are filled in only where they carry the argument. Not
every leaf needs a written tuple.

The worksheet must be able to **prove the v1 gates**, so it separates
**commands** (runnable) from **aliases** (attached to any node) and
carries each record's lifecycle to completion:

```yaml
version: 1
generated_at: "YYYY-MM-DD"            # stamp the walk date
commands:                             # one record per ACTIVE RUNNABLE command,
                                      # including hidden runnable commands and
                                      # Cobra built-ins
  - path: gcx kg meta logs            # exact command path, no wildcards
    classification: human-review      # ok | suggested-fix | human-review
    observed_behavior: "prints KG log-related metadata (collection)"
                                      # REQUIRED for suggested-fix and
                                      # human-review, with a source citation
    evidence: internal/providers/kg/...
    proposed_fix: ""                  # rename target / removal, for suggested-fix
    disposition: pending              # pending | keep | ratify | rename | remove
    canonical_path: ""                # full final path when disposition=rename
    replacement: ""                   # when disposition=remove: the full path of
                                      # the command to use instead, or the literal
                                      # value `none` (removed, no replacement) —
                                      # REQUIRED for remove; never left blank
    approved_by: ""                   # owner who approved
    migration_status: n/a             # n/a | pending | recorded
    owner: kg                         # provider/owner directory
    batch: ""                         # migration-batch identifier once scheduled
aliases:                              # one record per alias on ANY node —
                                      # runnable commands AND non-runnable groups
  - owner_path: gcx k6 load-tests     # the node the alias is attached to
    alias: test
    node_runnable: false              # false ⇒ a path prefix, not an operation
    disposition: pending              # pending | keep-synonym | remove
    approved_by: ""
    replacement: ""                   # when disposition=remove: the full canonical
                                      # path (leaf alias) or canonical group prefix
                                      # (group alias), or the literal `none` —
                                      # if the owning group is itself renamed, this
                                      # is the RENAMED prefix, so the mapping stays
                                      # correct end-to-end
    migration_status: n/a             # n/a | pending | recorded
    owner: k6
    batch: ""
```

These fields generate the §4 gate 6 migration outcomes mechanically:
`disposition=rename` → `old path → <canonical_path>`;
`disposition=remove` + `replacement=<path|prefix>` →
`old path → removed; use <replacement>`; `disposition=remove` +
`replacement=none` → `old path → removed; no replacement`. Group-alias
records emit one prefix mapping, not per-descendant rows.

Rules:

- Command records cover **every active runnable Cobra command** —
  including hidden runnable commands and Cobra built-ins.
- Alias records come from **every Cobra node, runnable or not**.
  Non-runnable group aliases such as `k6 load-tests` ⇢ `tests`/`test`,
  `k6 runs` ⇢ `run`, `irm` ⇢ `oc`, and `synthetic-monitoring` ⇢
  `sm`/`synth` are **aliases/path prefixes, not operation verbs** — they
  get dispositions here and are never operation-validated by CI.
- Every *suggested fix* cites observed source behavior (never inferred
  from the name).
- Approval: `ok` commands need no separate approval. Rename, remove,
  ratify, and human-review dispositions require owner approval — and so
  does a *suggested fix* resolved as `keep` (overriding the suggestion is
  an explicit owner call). Every alias disposition requires owner
  approval.
- **The initial inventory snapshot is preserved.** Renamed or removed
  paths must not vanish from the worksheet as batches land — the
  snapshot plus per-record `canonical_path`/`replacement`/
  `migration_status` is the source the v1 migration-disposition table is
  generated from.

Do not add executable semantic metadata, inferred contracts, lifecycle
metadata on Cobra commands, a central path-keyed command registry, or a
versioned command-surface JSON — those belong to the deferred contract
design if it ever lands.

## 3a. Migration safety gates (every rename)

Every rename — whether in a rename-only batch or a mixed rename/removal
batch — preserves:

- the positional-argument contract;
- flag names, types, defaults, and shorthands;
- payload and output schemas;
- command behavior;
- error identity/type and exit codes.

These rules apply to **every rename**, including renames inside mixed
rename/removal provider batches. A rename **must** apply the mechanically
necessary old-path → new-path substitutions everywhere the command path
appears as **active** text: `Use`, usage/help text, examples, completion
suggestions, error suggestions, diagnostics containing the command path,
agent annotations, skills, and documentation. Records whose purpose is to
document the old surface are deliberately **excluded** from substitution:
migration mappings, the initial inventory snapshot, and historical audit
evidence keep old paths. Any behavior change is extracted into a
separately reviewed change — never smuggled into a rename. The
provider/domain owner approves each classification in their batch.

## 3b. Known human-review items (non-blocking, routed to owner batches)

- **Instrumentation `clusters apps`** (maintainer-raised, 2026-07-17):
  the help text talks about *configurations* while the group is named
  `apps`, and `apps get <cluster> <namespace>` addresses a namespace, not
  an "app". Observed at HEAD (`cmd/gcx/instrumentation/clusters/apps/`):
  `list <cluster>` returns only the **declared** namespace entries
  (annotated with a `discovered` flag), while `get <cluster> <namespace>`
  also succeeds for a **discovered-but-undeclared** namespace, returning
  a fabricated minimal view — so get's population is a superset of
  list's, and get's own help text ("exits non-zero … when the namespace
  has no declared configuration") misstates its behavior. Questions for
  the Beyla/Instrumentation Hub owners in their batch: what is the
  canonical product noun; are discovered-but-unconfigured namespaces
  addressable subjects; should `list` and `get` expose the same
  population? The ADR deliberately does not guess — and this item
  requires an **explicit owner answer as part of the batch approval**:
  silence is not a disposition, and keeping the current name is itself an
  explicit choice (ADR §9).
- **Adaptive subject mismatch** (from D12): logs `patterns` vs
  metrics/traces `recommendations` — same API family, different nouns.
  Classification decides whether a subject rename is warranted.

## 4. v1 acceptance checklist (all must pass to tag v1.0.0)

1. Every active runnable command has a worksheet **command record** with
   a resolved disposition (keep / ratify / rename executed / removed),
   and every alias on every node has a resolved **alias record**
   (keep-synonym / remove). Zero `pending` records; the lexical CI's
   temporary deviation list is empty.
2. **Zero** migration/deprecation aliases or compatibility forwarders in
   the shipped tree; every remaining alias is an explicitly approved
   permanent synonym (e.g. traces `search`/`tags` per D4).
3. First-party docs, skills, README, and agent annotations use canonical
   paths only (the skills-drift test covers skills; a docs sweep covers
   the rest).
4. The allowed-operation registry is merged (separately approved diff)
   and the lexical CI check is green with an empty deviation list.
5. Both ADRs (023 cross-signal, 024 operation semantics) are `accepted`;
   CONSTITUTION/DESIGN/naming.md carry the reconciled text; the
   ARCHITECTURE ADR table is current.
6. The **v1.0.0 release notes / migration guide carry the complete
   migration-disposition table unconditionally**: every changed or
   removed primary command and alias resolves to exactly one of
   `old path → new path`, `old path → removed; use <replacement>`, or
   `old path → removed; no replacement`. Group aliases use one prefix
   mapping (e.g. `gcx k6 test …` → `gcx k6 load-tests …`) rather than
   expanding every descendant. Published Grafana documentation
   referencing gcx commands is updated at release time (D9).
7. Gates 1–6 complete **the naming workstream** of #387 (per D13). They
   do not by themselves close #387, whose remaining scope (output-format
   consistency, global `--limit`) is tracked separately with the
   maintainers.

## 5. Initial operation registry (working input for D10)

The point-in-time measurements that motivated this plan are deliberately
not maintained here or in the ADR (per the
[doc-maintenance policy](../reference/doc-maintenance.md), volatile
counts are not hardcoded in docs). The **generated live inventory is
authoritative** — including the split of out-of-family leaves between
utility, noun leaves, and verb leaves. The classifier is reproducible: a
leaf is "inside" when its final token is one of {list, get, create,
update, delete, upsert, push, pull, query, search, labels, series,
metrics, metadata, status, timeline, inspect, diff, stats, report,
describe} or an `<operation>-<subject>` compound whose stem is in that
set; the inventory generator ships this list as code.

Per D10, **existing use does not automatically ratify an operation, and
nothing in this section is ratified by this plan**. The entries below are
candidates only. The actual initial registry lands as a separately
visible, explicitly maintainer-approved diff: each entry carries a
precise one-line semantic definition, and every occurrence in the tree is
then classified against that definition — a token matching a registry
name is not itself compliance.

- **Registry candidates** (each needs its definition written at
  registry time): `wait`, `test`, `export`, `edit`, `check`, `validate`,
  `run`, `deploy`, `install`, `uninstall`, `snapshot`, `configure`,
  `emit`, `new`, `acknowledge`/`unacknowledge`, `silence`/`unsilence`,
  `resolve`/`unresolve`, `escalate`, `open`/`close`, `restore`, `sync`,
  `apply`, `cancel`, `dismiss`, `prune`, `scaffold`, `generate`,
  `import`, `serve`, `chat`, `judge`, `evaluate`, and — with a
  membership-semantics definition (both resources survive) — `add`/
  `remove`.
- **Classification-first tokens** (no registry entry proposed; each
  occurrence is classified, renamed, or justified individually): `set`,
  `unset`, `save`, `show`, `summary`, `current`, `health` (likely
  `status`), `map`, `mode`, `pause`/`resume`/`share`, `token-reset`
  (likely `reset-token`), and all noun leaves. Note `remove-sourcemap`
  destroys the sourcemap and classifies as `delete-sourcemap` regardless
  of the `remove` registry entry — the definition decides, not the token.

**Alias inventory (input to alias dispositions).** The flat command
catalog does not emit aliases, so the worksheet inventories them from the
Cobra tree directly — one record per alias on **every node, groups
included** (§3; a group alias is a path prefix that renames every path
beneath it, never an operation verb). Known CRUD-adjacent aliases needing
explicit dispositions: alert templates `upsert` ⇢
`create`/`update`/`apply` (proposed removal, D6); dashboards `list` ⇢
`ls` and `delete` ⇢ `rm`; the k6 **group** aliases `runs` ⇢ `run` and
`load-tests` ⇢ `tests`/`test`; config `use-context` ⇢ `use`; alert
`notification-policies` ⇢ `policies`. Declared permanent synonyms
already on the books: traces `query` ⇢ `search` and `labels` ⇢ `tags`
(cross-signal ADR). Dozens of further aliases are benign
singular/abbreviation synonyms — on leaves and on groups
(`template`, `probe`, `incident`/`inc`, `oc`, `sm`, `ag`, `ec`, `ep`,
`proj`, `env`, …); each still gets an explicit keep-synonym or remove
disposition in its provider batch — "benign" is a starting guess, not a
disposition.

## 6. Claims changed or rejected against the July-1 audit

Recorded so the audit's checklists are not worked from (the audit itself
now carries a historical/non-authoritative label):

1. `list-X` → nested noun groups: **reversed** — parent-ID compounds are
   canonical (July ruling), and `list-<subject>` is generically approved
   for discovery/catalog facets (ADR §8).
2. Adaptive `show` → `get`: **wrong target** — they return collections;
   correct rename is `list` (verified in code).
3. `kg entities inspect` → `get`: **rejected** — genuine diagnostic view.
4. `upsert` → `create`/`update`: **rejected** — true create-or-update;
   splitting fakes existence semantics.
5. `describe-table` → `get`: **superseded** — per-command classification
   (Athena ≈ `list-columns`; ClickHouse defensibly `describe`).
6. `get` → YAML default: **out of scope** — output defaults are a
   separate workstream; the "one coordinated change" claim no longer
   holds at HEAD (defaults are already heterogeneous).
7. Universal `get`-returns-one-item: **rejected** — `resources get` is a
   selector protocol family returning item-or-collection.
8. "Missing `wide` codecs" as an audit item: **downgraded** — `wide` is
   registered only when genuinely useful extra fields exist (explicit
   maintainer feedback).

## 6a. Proposed disposition of the original issue work items

The issue body proposed six work items. The dispositions below are
**proposed, pending acceptance of this ADR** — completing this naming
workstream does not by itself close #387 (output-format consistency and
global `--limit` remain separate #387 work):

| Original item | Proposed disposition |
|---------------|----------------------|
| Codify the verb taxonomy in `docs/design/naming.md` | Addressed by this proposal (complete on acceptance): the operation-semantics ADR (024) + naming.md §9.7 |
| Strengthen CONSTITUTION § Provider Architecture with the full taxonomy | Addressed by this proposal (complete on acceptance) via the ADR's constitutional amendments |
| Audit all providers and file individual issues per mismatch | Proposed replacement: the inventory/classification worksheet + provider-sized tracking issues (explicit deviation, **pending D13 approval**) |
| Document the signal provider pattern as canonical | Addressed by this proposal: ADR §4 (approved shorthands) + ADR 023 ratification (**pending D4 approval**) |
| Audit `wide` codec registration across all providers | **Not pursued** — explicit maintainer objection to enforcing `wide`; the provider-checklist policy line is corrected **in this PR** (register `wide` only when genuinely useful extra columns exist) |
| Refresh `[CURRENT]`/`[ADOPT]`/`[PLANNED]` markers in `docs/design/` | **Rejected** — those markers do not exist anywhere in the repo (verified by repo-wide search); the item was based on a convention that was never adopted |

## 7. Out of scope (tracked separately)

Output-format defaults and codecs; TypedCRUD migrations; backend
capability modeling (native/emulated/unsupported), pagination, rate
limiting, retry/idempotency, auth standardization (client-platform
follow-up issue); k6 run-history consolidation except where a semantic
rename requires it; any pure-resolver/datasource-persistence refactor
(ADR §4 keeps only the factual caveat); post-GA compatibility-forwarder
implementation design (v1 ships none — ADR §11 keeps only the policy);
the full machine-readable command contract (possible future design — ADR
"Deferred, not rejected").
