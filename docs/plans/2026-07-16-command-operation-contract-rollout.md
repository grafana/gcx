# Rollout Plan: Command Operation Contract

> **Date**: 2026-07-16
> **Status**: draft — companion to the [Command Operation Contract ADR](../adrs/command-operation-contract/001-command-operation-semantics.md)
> **Related issues**: #387 (verb taxonomy umbrella)

The ADR records the decision; this plan carries the migration artifacts:
the maintainer decision table, the rollout sequence, the census schema,
the GA acceptance checklist, the initial vocabulary partition, and (as a
post-GA reference) the compatibility-forwarder mechanics.

## 1. Maintainer decision table

Each item wants an explicit yes/no (or a correction). Recommended answers
reflect the evidence gathered in July 2026; "verified" means checked
against actual command behavior on main.

| # | Decision | Recommended | Notes |
|---|----------|-------------|-------|
| D1 | **Transport neutrality** — genuine read-one is `get`, genuine enumeration is `list`, regardless of adapter registration. Inverts the current CONSTITUTION "never `get`/`list`; use `show`/`describe`/`search`" rule. Constitutional change. | Yes | The old rule leaks implementation tiers to users |
| D2 | **Semantic contracts replace word-shape grammar** — "last word is a verb" becomes the effect of the contract, not the rule. | Yes | Word-shape misclassifies `status`, `labels`, `list-trials` |
| D3 | **Addressability rule** — parent-ID compounds, child groups by child identity, explicit metadata for selector/optional/variadic/multi/flag/composite identities. | Yes | Formalizes the July Slack ruling; same outcomes on every audited case |
| D4 | **Signal shorthands + full ADR 023 ratification** — (a) `labels`/`series`/`metrics`/`metadata` are approved surface shorthands mapped to real list/query semantics, not violations and not independent operations; (b) the cross-signal ADR (023) is ratified **in full** — its surface shipped in April 2026 and also governs datasource flags, time semantics, and the permanent traces `search`/`tags` aliases (which fall under alias governance); (c) `profile-types` sits outside the closed shorthand set and must be resolved as part of this decision: add it to the set with the contract {list, profile-types, shorthand, standard, query, read_only, none, collection} — covering **both** mount points of its shared builder (`profiles profile-types` and `datasources pyroscope profile-types`) — or send it to the census. | Yes to (a)+(b); (c): recommend adding to the set | Approving D4 flips ADR 023 to accepted in the acceptance commit |
| D5 | **View vocabulary** — `status`/`timeline`/`inspect`/`diff`/`stats`/`report`; `describe` admitted narrowly per command; `show` and `summary` not canonical (existing `summary` commands reclassified by real output). `kg entities inspect` stays. | Yes | Verified: `inspect` is a genuine RCA view |
| D6 | **True upsert** — defined by the user-visible create-or-update guarantee; keep `alert templates upsert`; its `create`/`update`/`apply` aliases are a semantic hole resolved during migration. Overrides the earlier audit line-item. | Yes | Verified: single PUT keyed by name; splitting adds races |
| D7 | **Pre-GA convergence, absolute** — v1.0.0 ships a fully classified canonical surface; census empty before v1; **zero gcx-owned `lifecycle=deprecated` commands or noncanonical compatibility paths ship in v1.0.0** (an exception requires explicitly amending this decision); each command resolves keep / ratify / rename / remove. | Yes | The load-bearing commitment |
| D8 | **Zero inferred/unresolved at v1** — every gcx-owned command carries an explicit contract before v1.0.0; bootstrap inference, the census loader, and the debt ceiling are removed after convergence; CI permanently rejects `inferred`/`unresolved` post-v1. | Yes | Prevents inference becoming permanent debt |
| D9 | **No final v0.x compatibility release — clean start.** Rename and remove noncanonical command paths during v1 development; ship v1.0.0 with only the canonical surface — no deprecated aliases, no compatibility forwarders. Migration support: the v1 migration guide carries a complete old→new command table, and published Grafana documentation referencing gcx commands is updated at release time. | Yes — clean start | Public-preview status warrants migration docs, not preserved preview paths |
| D10 | **Domain-verb governance rule** — D10 approves **governance only**: *existing use does not automatically ratify an operation*; each operation receives a precise semantic definition in the registry, and each occurrence is classified against it. **No candidate in §5 is ratified by this decision.** The initial-registry diff is a separately visible artifact requiring explicit maintainer approval before the implementation PR merges; operations discovered during census generation enter the same way. Example of why per-occurrence classification matters: `remove` is legitimate for membership semantics (both resources survive, e.g. removing a conversation from a collection) while `remove-sourcemap` destroys the sourcemap and is classified `delete-sourcemap` — the *definition* decides, not the token. | Yes | Keeps this ADR manageable; prevents existing tokens becoming standards by accident |
| D11 | **Utility enumeration** — extend CONSTITUTION's closed single-token list with `commands`, `help-tree`, `api` (they exist today; they cannot be both canonical exceptions and constitutional violations). `gcx api` carries a **destructive** static effect with `effect_varies: true` (conditions on `--method` and `--data` — a body implies POST **only when `--method` is omitted**, an explicit `--method` wins) and an opaque result, and is never presented as read-only. | Yes | Constitutional change; behavior verified in `cmd/gcx/api/command.go` |
| D12 | **Adaptive pilot** — `logs adaptive patterns show`, `metrics adaptive recommendations show`, `traces adaptive recommendations show` → `list`. | Yes | Verified: all three return collections (`ListRecommendations`) |
| D13 | **Tracking granularity + umbrella scope** — provider-sized migration tracking issues replace the umbrella issue's original "file individual issues per mismatch" wording (recorded here as an explicit deviation), and the umbrella issue becomes **the v1 convergence umbrella — i.e. a release gate**: it remains open until the census is empty and every GA gate passes. That scope expansion is deliberate and needs conscious approval. | Yes | Process decision |
| D14 | **Reads are pure** — read/query commands must not mutate persistent state **within Effect's semantic boundary** (incidental token rotation/caches/telemetry are outside it); datasource-UID auto-persistence (`ResolveAndSaveDatasource`) moves to a pure resolver + explicit setup/config action; until refactored, affected reads are census entries classified `mutating`/`effect_varies` (ADR §4). The resolver refactor **will be tracked** as a separate implementation issue, filed with the acceptance commit — the naming decision does not depend on it. | Yes | Preserves honest `read_only` contracts; avoids agents treating ordinary queries as mutation-risk |

## 2. Rollout sequence

1. **Draft ADR + governing-doc reconciliation** (this PR, kept draft).
2. **Explicit maintainer approval** of D1–D14 on the issue.
3. **Acceptance commit**: this commit does more than flip statuses. It
   (a) flips BOTH ADRs — the operation contract (024) and the
   cross-signal ADR (023, whose surface shipped in April but whose
   ratification is bundled with D4) — to `accepted` with dated
   `**Accepted**:` lines and ARCHITECTURE table statuses; (b) **writes
   the selected D4(c) `profile-types` outcome into both ADRs AND the
   naming guide's vocabulary table (naming.md §9.7)**; (c) marks
   ADR 023's stale "needs implementation" / follow-up checklists as
   historical/completed. Then merge. Constitutional changes land only in
   this accepted state; the CONSTITUTION contract requirement stays
   scoped to new/modified commands until step 4 lands.
4. **Implementation PR**: contract metadata package, one shared resolver,
   conservative bootstrap inference, embedded migration census, CI ratchet
   (full-tuple fail-closed + allowed-combination validation + census
   liveness/ceiling + deprecated-path safety + skills rejection of
   deprecated paths), catalog fields, versioned command-surface JSON in
   the reference/drift chain. Atomically with this, the CONSTITUTION and
   provider-checklist requirements become universal.
5. **Pilot**: the three adaptive `show → list` renames (D12) — clean
   renames per D9 (no forwarders), updating skills, annotations, docs,
   and the surface snapshot atomically.
6. **Provider-sized migration PRs in parallel, all before GA**: each
   census entry resolves keep-annotate / ratify-exception / rename /
   remove; each PR replaces census entries with explicit contracts and
   lowers the ceiling. PR count is review-safety, not a product stage.
7. **Convergence**: census empty; bootstrap inference and census
   machinery removed; the permanent explicit-only CI gate activates.

## 3. Migration-census schema

Temporary scaffolding embedded in the contract-metadata package (so the
runtime catalog can report `contract_source` truthfully). The file MUST be
empty before v1.0.0 and is deleted, with its loader, at convergence.

The census records enough to let reviewers independently verify that each
rename is semantics-driven — not just the violation, but the observed
behavior and the full proposed contract:

```yaml
version: 1
generated_at: { commit: "<sha>", date: "YYYY-MM-DD" }   # stamp the inspected tree
entries:
  - path: gcx kg meta logs              # exact CommandPath, no wildcards
    terminal: logs
    area: kg                            # provider/owner directory
    violations: [unknown-operation]     # unknown-operation | ambiguous-addressing |
                                        # incomplete-contract | noncanonical-nesting |
                                        # undeclared-alias | disallowed-combination
    current:                            # observed behavior (filled during classification)
      behavior: "prints KG log-related metadata"
      result_shape: collection          # item | collection | item_or_collection |
                                        # report | stream | mutation | none | opaque
      aliases: []                       # live cobra aliases, if any
      first_party_refs: []              # skills / docs / annotations using this path
    alias_dispositions: []              # one per live alias:
                                        #   - alias: create
                                        #     action: remove        # keep | remove | retarget
                                        #     target: ""            # canonical path when action=retarget
                                        #     rationale: "Misrepresents true upsert semantics."
    proposed:                           # the intended contract (empty while pending)
      operation: ""
      subject: ""
      surface_form: ""                  # direct | compound | shorthand
      conformance: ""                   # standard | protocol_exception
      category: ""
      effect: ""                        # read_only | mutating | destructive (the static maximum)
      effect_varies: false              # true when the effect depends on invocation
      addressing: ""
      result: ""                        # logical semantic result, not a wire schema
      identity_parts: []                # {name, role, required, position_or_flag} —
                                        # required for EVERY ambiguous single form: optional,
                                        # variadic, multiple, selector, flag-based, or composite
      identity_forms: []                # alternative identity forms ("one positional OR
                                        # two flags") — each entry MUST be a complete
                                        # identity_parts list (validated); identity_parts
                                        # alone = a single form
      effect_conditions: []             # {when, effect} — explanatory; required when effect_varies
      path: ""                          # target path when resolution=rename
    resolution: pending                 # pending | keep-annotate | ratify-exception |
                                        # rename | remove
    compatibility: none                 # none (default per D9) | approved-exception (requires amending D7)
    replacement: ""                     # canonical replacement when compatibility != none
    rationale: "Noun-only executable pending semantic classification."
    owner: kg
    batch: ""                           # migration-batch identifier once scheduled
    review_by: 2026-09-01               # informational; format-checked only,
                                        # never date-enforced in CI
```

The census generated fresh from the live tree is the **sole migration
input**; the July-1 audit is historical context only and must not be
worked from.

CI rules: every entry resolves to an exact `CommandPath()` match with zero
remaining args (cobra `Find` follows aliases and hidden commands, so
exactness prevents renamed-with-forwarder paths keeping stale entries);
each entry still violates exactly its recorded codes; owner + rationale
required; count ≤ a frozen ceiling. Honest limitation: a ceiling prevents
net growth but does not prove shrink-only behavior — that comes from
review of census diffs (optionally a base-branch comparison job outside
the hermetic test suite). Expected initial size on current main: ~150–200
entries.

**Sharding for parallel batches**: a single census file and one global
ceiling would serialize every provider PR through the same merge
conflict. Shard the census as
`legacy-command-contracts/<provider>.yaml` with per-provider ceilings and
one global total check, so provider PRs merge in parallel; alternatively
(explicitly, not by accident) serialize merges through a single migration
owner.

## 3a. Migration safety gates (every rename-only batch)

A rename-only batch preserves, byte-for-byte where applicable:

- positional arguments and flags (names, types, defaults, shorthands);
- completion behavior;
- stdout shape and stderr diagnostics;
- error identity and exit codes.

Any behavior change is extracted into a separately reviewed change — never
smuggled into a rename. The provider/domain owner approves each
classification in their batch. Final release notes for the migration
carry a user-facing `old path → new path` table.

## 4. GA acceptance checklist (all must pass to tag v1.0.0)

1. The migration census has **zero entries**; the census file, loader,
   ceiling, and bootstrap inference are removed.
2. Every gcx-owned **active** runnable command — including hidden active
   commands — resolves `contract_source=explicit` (Cobra built-ins:
   `builtin`); CI permanently rejects `inferred` and `unresolved`.
3. Full-tuple validation passes: operation, subject, surface form,
   category, effect, addressing, result shape — all present and within
   the operation's allowed combinations — **including `identity_parts`
   for every ambiguous single identity form (optional, variadic,
   multiple, selector, flag-based, or composite), `identity_forms` for
   alternative forms (each form validated as a complete parts list), and
   `effect_conditions` whenever `effect_varies` is set**.
4. **Zero** gcx-owned `lifecycle=deprecated` commands or noncanonical
   compatibility paths ship in v1.0.0 (an exception exists only via an
   explicit amendment of the convergence decision).
5. Alias and path hygiene, in three distinct clauses:
   (a) first-party docs, skills, README, and agent annotations use
   canonical paths only; (b) the command-surface JSON inventories
   canonical commands **plus approved permanent aliases** (e.g. traces
   `search`/`tags` per D4) with their targets; (c) zero undeclared or
   semantically misleading aliases remain (every live alias has an
   approved disposition).
6. The command-surface JSON schema version is stamped; the surface diff
   vs the last v0.x release is reviewed and changelogged.
7. Both ADRs (023 cross-signal, 024 operation contract) are `accepted`;
   CONSTITUTION/DESIGN/naming.md carry the reconciled text; the
   ARCHITECTURE ADR table is current.
8. The **v1.0.0 release notes / migration guide carry the user-facing
   `old path → new path` table unconditionally**, covering every rename
   executed under this contract, and published Grafana documentation
   referencing gcx commands is updated at release time (D9).
9. The umbrella issue closes only when gates 1–8 hold (per D13).

## 5. Initial operation registry (working input for D10)

Numbers measured at `main@9d193ca7` (2026-07-16, fresh build + tree walk):
487 runnable leaves, 142 distinct terminal tokens, 274 already
strict-CRUD, 81 more in the pre-existing approved token families; **132
leaves end outside the pre-existing token families** — of which ~29 are
CLI-utility paths the ADR ratifies (§§7, 9), roughly 45 are noun leaves
(census: classify or rename), and ~60 are shipped verb leaves to
partition below. The classifier is reproducible: a leaf is "inside" when
its final token is one of {list, get, create, update, delete, upsert,
push, pull, query, search, labels, series, metrics, metadata, status,
timeline, inspect, diff, stats, report, describe} or an
`<operation>-<subject>` compound whose stem is in that set; the census
generator ships this list as code.

Per D10, **existing use does not automatically ratify an operation, and
nothing in this section is ratified by this plan**. The entries below are
candidates only. The actual initial registry lands as a separately
visible, explicitly maintainer-approved diff before the implementation PR
merges: each entry carries a precise one-line semantic definition
(subject class, effect, result), and every occurrence in the tree is then
classified against that definition — a token matching a registry name is
not itself compliance. Operations discovered during census generation
enter the same way.

- **Registry candidates** (each needs its definition written at
  registry time): `wait`, `test`, `export`, `edit`, `check`, `validate`,
  `run`, `deploy`, `install`, `uninstall`, `snapshot`, `configure`,
  `emit`, `acknowledge`/`unacknowledge`, `silence`/`unsilence`,
  `resolve`/`unresolve`, `escalate`, `open`/`close`, `restore`, `sync`,
  `apply`, `cancel`, `dismiss`, `prune`, `scaffold`, `generate`,
  `import`, `serve`, `chat`, `judge`, `evaluate`, and — with a
  membership-semantics definition (both resources survive) — `add`/
  `remove`.
- **Census-first tokens** (no registry entry proposed; each occurrence is
  classified, renamed, or justified individually): `set`, `unset`,
  `save`, `show`, `summary`, `current`, `health` (likely `status`),
  `map`, `mode`, `pause`/`resume`/`share`, `token-reset` (likely
  `reset-token`), and all noun leaves. Note `remove-sourcemap` destroys
  the sourcemap and classifies as `delete-sourcemap` regardless of the
  `remove` registry entry — the definition decides, not the token.

The census generated at implementation time **informs the separately
approved registry diff**; this list seeds the maintainer review.

## 6. Compatibility-forwarder mechanics (post-GA reference only)

Under D9 there are **no pre-GA forwarders** — pilot and migration renames
are clean. This section is retained solely for post-v1 deprecations
(minor releases may deprecate a path with a functional forwarder until
the next major, per ADR §11). Where a post-GA forwarder is built:

- **Fresh construction**: the forwarder builds the canonical command via
  its own constructor (fresh opts struct, fresh flag bindings, fresh
  `ValidArgsFunction`); never copy `pflag.FlagSet` pointers, call a
  foreign `RunE`, re-`Execute`, rewrite `os.Args`, or spawn a subprocess.
- **Warning emission wraps `Run`/`RunE`** (whichever is set) and writes
  one warning to stderr via the agent-mode-aware diagnostic emitter.
  Never use `cobra.Command.Deprecated`: Cobra prints it through the
  command output writer, and gcx sets that to stdout — it would corrupt
  JSON/YAML pipelines.
- **Same-parent moves only** for the generic helper. Cross-parent moves
  require batch-specific behavioral parity tests: effective flag
  type/default/shorthand behavior, persistent initialization and config
  loading, completion, and stdout/stderr/exit-code parity — parent-hook
  presence and flag names alone do not prove equivalence.
- Known trap: `RegisterFlagCompletionFunc` errors on duplicate
  registration against the same flag pointer; fresh construction is safe,
  but a constructor registering completion on an inherited persistent
  flag would fail on the second build — assert in forwarder tests.
- Forwarder metadata: hidden, aliases cleared, lifecycle=deprecated,
  replacement, deprecated-since, earliest-eligible removal version,
  migration issue.

## 7. Claims changed or rejected against the July-1 audit

Recorded so the audit's checklists are not worked from (the audit itself
now carries a historical/non-authoritative label):

1. `list-X` → nested noun groups: **reversed** — parent-ID compounds are
   canonical (July ruling).
2. Adaptive `show` → `get`: **wrong target** — they return collections;
   correct rename is `list` (verified in code).
3. `kg entities inspect` → `get`: **rejected** — genuine diagnostic view.
4. `upsert` → `create`/`update`: **rejected** — true create-or-update;
   splitting fakes existence semantics (verified single PUT).
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

## 7a. Disposition of the umbrella issue's original work items

The issue body proposed six work items; their final disposition (so the
issue can close honestly):

| Original item | Disposition |
|---------------|-------------|
| Codify the verb taxonomy in `docs/design/naming.md` | Done, evolved: the operation contract (ADR 024) + naming.md §9.7 |
| Strengthen CONSTITUTION § Provider Architecture with the full taxonomy | Done via the ADR's constitutional amendments |
| Audit all providers and file individual issues per mismatch | Replaced by the migration census + provider-sized tracking issues (recorded deviation, D13) |
| Document the signal provider pattern as canonical | Done: ADR §4 (approved shorthands) + full ADR 023 ratification (D4) |
| Audit `wide` codec registration across all providers | **Not pursued** — explicit maintainer objection to enforcing `wide`; the checklist-policy correction moves to the housekeeping PR |
| Refresh `[CURRENT]`/`[ADOPT]`/`[PLANNED]` markers in `docs/design/` | **Rejected** — those markers do not exist anywhere in the repo (verified by repo-wide search); the item was based on a convention that was never adopted |

## 8. Out of scope (tracked separately)

Output-format defaults and codecs; TypedCRUD migrations; backend
capability modeling (native/emulated/unsupported), pagination, rate
limiting, retry/idempotency, auth standardization (client-platform
follow-up issue); k6 run-history consolidation except where a semantic
rename requires it.

**Housekeeping PR (separate, small):** restore the accidentally-deleted
ADR-006 file (`docs/adrs/conventional-commits/001-pr-title-enforcement.md`,
deleted by an unrelated rebase in `4c754615`; content recoverable from
`0689aed2`) so the ARCHITECTURE table's [006] link resolves again; apply the `wide`
codec policy correction to the provider checklist (register `wide` only
when genuinely useful extra fields exist — per explicit maintainer
feedback — rather than mandating it for every list/get command).
