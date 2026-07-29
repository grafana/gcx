# Global `--limit` Investigation & List Truncation Contract (#387 Track C)

> **Status: proposal / experiment record.** This documents the investigation
> behind the `--limit` + truncation-hint contract prototyped on branch
> `test/387-e2e-feasibility`, including why a root-level persistent `--limit`
> was rejected, how PR #988's defects are fixed, and what migration remains.
> Nothing here is a settled repo-wide requirement until #387 lands.
>
> The first production PR extracts everything below EXCEPT the
> `alert rules list` partial migration, which stays prototype-only on the
> feasibility branch pending the envelope/unit decisions (§ 5–7) — rows and
> proofs below that exist only on the prototype are marked as such.

## 1. Problem

The command-surface audit (#387) found `--limit` semantics scattered across
~64 local flag registrations (a fresh grep of `Var(&…, "limit"` finds 64
non-test registrations; the audit inventory counted 67 including variants),
with three systemic defects:

1. **Silent truncation** — many list commands slice client-side with no
   signal on stdout or stderr (`adapter.TruncateSlice` callers, `datasources
   list` at the old `list.go:112-114`).
2. **Inconsistent `0` semantics** — `0` variously means "all", "server
   default", "invalid", or "all up to a hidden cap".
3. **No machine-readable contract** — an agent reading `-o json` output
   cannot distinguish a truncated page from the complete set.

PR #988 attempted a fix and was found broken in maintainer review (§ 4).

## 2. Root-level persistent `--limit`: investigated and REJECTED

A single `--limit` persistent flag on the root command looks attractive but
fails on three grounds:

### 2.1 Shadowing mechanics

Cobra merges persistent flags into each command's flag set at execution time;
`pflag.FlagSet.AddFlagSet` **silently skips** flags whose name already exists
in the target set. Every one of the ~64 existing local `--limit` flags would
shadow the root flag, so the "global" flag would apply only to commands that
never declared one — including dozens of non-list commands (`login`, `push`,
mutation verbs) where it is meaningless help pollution. A global default also
could not vary per command (list commands legitimately differ: 0, 20, 50…).

### 2.2 Semantic outliers (same name, different meaning)

At least six existing `--limit` families are not "number of list rows" and
must never be captured by a uniform contract:

| Command family | Location | Semantics |
|---|---|---|
| Loki queries | `internal/datasources/loki/query.go:130`, `cmd/gcx/datasources/query.go:214` | Max **log lines** returned by the Loki engine, not list rows |
| SQL datasources (ClickHouse/Athena) | `internal/datasources/clickhouse/query.go:130`, `internal/datasources/athena/query.go:132`, clamp in `internal/query/sql/types.go:44-56` | Rewrites/append a SQL `LIMIT` clause; existing user `LIMIT` values are silently clamped to 1000 |
| Dashboards list | `internal/providers/dashboards/crud.go:50` | K8s **page size** paired with `--continue` cursor pagination ("0 fetches all pages") |
| Incidents list / activity | `internal/providers/irm/incidents_commands_impl.go:43` (list: `--limit` < 1 is **invalid**, lines 56-57), `:507` + `internal/providers/irm/incidents_client.go:399-402` (activity: `0` is silently **coerced to 50** server-side) | `0` is not "all" on either path |
| Incidents contexts | `internal/providers/irm/incidents_commands_impl.go:747` | `0` = **server default**, not "all" |
| Resources get | `cmd/gcx/resources/get.go:123` | Per-resource-type K8s page size across multiple types |

### 2.3 Capped sources

`irm oncall alert-groups list` bounds even `--limit 0` with a 1000-item
runaway cap (`alertGroupListHardCap`, `oncall_commands_extra.go`). A global
"0 means all results are returned" promise would be a lie there.

### 2.4 Chosen architecture

An **opt-in shared binder + uniform contract**, migrated incrementally:

- `(o *Options) BindListLimit(flags, p, subject, def)` registers `--limit`
  with the maintainer-approved wording ("Maximum number of `<subject>` to
  return. 0 means all results are returned") and hooks `>= 0` validation into
  `Options.Validate()` (already called by every list command).
- Truncation metadata + hints live in `internal/output/listmeta.go`
  (`ListMeta`, `TruncateCompleteList`, `TruncatePagedList`, `PagedListMeta`,
  `AttachListMeta`, `EmitListTruncationHint`).
- Capped sources skip the binder (bespoke wording disclosing the cap) but
  participate in the metadata contract via `ListMeta.Cap`.
- Semantic outliers (§ 2.2) are explicitly out of scope.

See `docs/design/output.md` § 15 for the normative contract text.

## 3. The contract in one page

- Reserved envelope key `list_meta`; **absence == complete set**.
- `ListMeta{truncated, returned, total?, cap?, continue?}`; `total` only when
  observed, `cap` only when the safety cap was the bound, `continue` always
  derived from real argv (filters survive).
- Constructors by source shape: cheaply-complete → `TruncateCompleteList`
  (total observed); paginated with more-pages signal → `PagedListMeta`
  (honors `serverHasMore` even at `limit<=0`); paginated without signal →
  over-fetch `limit+1` + `TruncatePagedList`.
- Hints (TTY): known total → "showing first N of M. See all results with:
  `<cmd> --limit 0`"; unknown total → "showing first N; more results are
  available. See more with: `<cmd> --limit 2N`"; cap → "showing first N
  (safety cap). Refine filters to narrow the result set" (no `--limit`
  suggestion).
- `--json` selection re-attaches `list_meta`; discovery excludes it.

## 4. PR #988 post-mortem: four defects, root causes, fixes

PR #988 introduced `list_meta` + a hint helper and migrated `datasources
list` and `alert-groups list`. Maintainer review found four problems; all are
verified against HEAD and fixed here:

### (a) Hint wording and hardcoded continue command

- **Symptom:** `hint: showing first 1 of 219: gcx datasources list --limit 0`
  — a "summary: command" splice; and the suggested command dropped the user's
  filter flags (`--type prometheus` etc.) because PR988 passed a **constant**
  command string per call site.
- **Root cause:** `EmitHint`'s TTY shape is `hint: <summary>: <command>`
  (`internal/output/format.go`), and PR988 fed it a bare count summary plus a
  hardcoded command.
- **Fix:** `EmitListTruncationHint` folds a connective into the TTY summary
  ("showing first 5 of 219. See all results with") so the line reads as a
  sentence, and the continuation is always derived from `os.Args` via
  `BuildListLimitCommand` (extending `internal/output/pagination.go`'s
  `stripFlag`/`shellJoin` used by the dashboards `--continue` hint) — prior
  `--limit`/`--limit=` spellings stripped, all other flags preserved.
- **Proof:** `TestEmitListTruncationHint` (exact TTY strings incl. the
  filter-preservation case), `TestBuildListLimitCommand`,
  `TestListExplicitLimitTrimsDatasources` (end-to-end stderr assert), and —
  prototype-only, on the feasibility branch —
  `TestRulesList_HintPreservesFilterFlags`.

### (b) `--limit 1 --json uid` returned `{"uid": null}`

- **Symptom:** field selection on a truncated page extracted from the
  envelope instead of the items — only when truncated (worst case).
- **Root cause:** `singleKeyItems` (`internal/output/field_select.go`)
  requires **exactly one** top-level key; adding `list_meta` makes two, so
  the single-key-envelope path fails and `extractFields` runs on the
  envelope.
- **Fix:** `singleKeyItems` now ignores a reserved `list_meta` sibling
  (object-valued only; scoped — any other second key keeps HEAD behavior),
  and the selection output **re-attaches** `list_meta`. The `items`-keyed
  path also carries `list_meta` through instead of dropping it.
- **Proof:** `TestFieldSelectionOnTruncatedEnvelope` (Dafydd's exact repro:
  `{"datasources":[...],"list_meta":{...}}` + `--json uid` →
  `{"datasources":[{"uid":"ds-01"}],"list_meta":{...}}`),
  `TestFieldSelectionOnTruncatedItemsEnvelope`,
  `TestSingleKeyEnvelopeWithUnrelatedSecondKey` (scoping guard),
  `TestListTruncatedFieldSelection` (end-to-end through the real command).

### (c) `--json list` discovery listed `list_meta.*` instead of item fields

- **Root cause:** same arity bug in `sampleFromObject`
  (`internal/output/format.go`) — the envelope no longer matched the
  single-key shape, so discovery sampled the envelope map itself.
- **Fix:** the tolerant `singleKeyItems` fixes the populated case;
  `reflectSingleSliceField` skips the reserved `list_meta`-tagged struct
  field so **empty**-envelope discovery keeps working on structs that carry
  the metadata field.
- **Proof:** `TestDiscoveryOnTruncatedEnvelope`,
  `TestDiscoveryOnEmptyEnvelopeWithListMetaField`,
  `TestListTruncatedFieldDiscovery` (end-to-end).

### (d) `alert-groups list --limit 0` silently capped at 1000 with no meta

- **Symptom:** the rich path caps `--limit 0` at `alertGroupListHardCap=1000`
  (`effectiveCap` fallback in `listAlertGroupsRaw`); PR988's `PagedListMeta`
  short-circuited on `limit <= 0`, so a hard-capped page carried **no**
  `list_meta` — violating its own "absence == complete" rule exactly where
  it matters most.
- **Fix:** `PagedListMeta(returned, limit, serverHasMore, safetyCap)` honors
  `serverHasMore` even at `limit <= 0` and records `Cap` when the cap (not a
  larger user limit) is the binding ceiling (`limit <= 0 || limit >= cap` —
  `limit == cap` counts, since a doubled continuation could never beat the
  cap). The hint
  switches to the cap variant and never suggests `--limit 0`. The legacy
  fallback path (which genuinely drains fully at `--limit 0`, no cap) now
  over-fetches `limit+1` and uses `TruncatePagedList`; at `--limit 0` it
  stays metadata-free, keeping the two paths individually honest.
  Naming correction: this fallback is NOT a production "SA-token" mode —
  the production loader always returns `*OnCallClient`, which implements
  the rich interface (`internal/providers/irm/config.go`,
  `interfaces.go`); the fallback is reachable only by alternate
  `OnCallAPI` implementations (e.g. test doubles) today.
- **Proof:** `TestPagedListMeta` ("limit zero capped by safety cap is
  truncated with cap"), `TestAlertGroupList_RichPath_LimitZeroHitsSafetyCap`,
  `TestAlertGroupList_LegacyPath_LimitZeroDrainsFully`,
  `TestAlertGroupList_LegacyPath_OverFetchDetectsTruncation`.

## 5. Behavior changes vs pre-contract HEAD

All rows below ship in the first production PR except the last, which is
prototype-only (feasibility branch).

| Change | Where |
|---|---|
| `datasources list` default `--limit` 50 → **0** (full set; maintainer's own review suggestion — the source is fully fetched anyway) | `cmd/gcx/datasources/list.go` |
| `datasources list` truncation now emits `list_meta` + stderr hint (was silent slice) | same |
| `datasources list` rejects negative `--limit` via the binder (was accepted, treated as unlimited); `alert-groups list` rejects it with bespoke capped-source wording | binder validation in `Options.Validate`; `alertGroupListOpts.Validate` |
| `alert-groups list` rich path: `list_meta` on truncated pages; `--limit 0` capped fetch now reports `cap:1000` + "safety cap" hint (was silent, indistinguishable from complete) | `internal/providers/irm/oncall_commands_extra.go` |
| `alert-groups list` rich-path hint wording: "showing first N results: … --limit 2N" → "showing first N; more results are available. See more with: `<argv-derived> --limit 2N`" (filters now survive) | same |
| `alert-groups list` legacy path: wire limit is now `limit+1` (over-fetch); truncated pages gain `list_meta` + hint (was silent) | same |
| `--json` selection/discovery: reserved `list_meta` key handled per § 15.5 (no behavior change for envelopes without it) | `internal/output/field_select.go`, `format.go` |
| PROTOTYPE-ONLY (not landed): `alert rules list` **hint-only partial migration** — truncation hints on stderr for table and JSON paths (was silent `adapter.TruncateSlice`); flag type `int64` → `int`; wording via binder. NOT a completed contract exemplar: the JSON/YAML payload is a bare array (no `list_meta`), and `--limit` keeps its pre-existing unit mismatch (table counts flattened **rules**, JSON counts **groups** — `--limit 50` over 2×100-rule groups truncates the table to 50 rules but returns all 200 rules as JSON). The envelope/unit decision is recorded in § 6 and § 7; kept out of the first production truncation PR | `internal/providers/alert/rules_commands.go` (feasibility branch only) |

Non-changes (parity kept): `alert-groups list` keeps its bespoke flag wording
(discloses the cap), its default limit 50, the 1000 hard cap itself, and the
note/filter/nav-hint ordering; `alert rules list` keeps default 50 and its
bare-array JSON shape.

## 6. Remaining migration (~40 list commands)

Mechanical migrations (cheaply complete sources currently using
`adapter.TruncateSlice` or ad-hoc slicing): `alert` groups/templates/
contact-points/mute-timings, `slo` reports, `k6` projects/tests/runs/envvars/
schedules/zones, `fleet` pipelines/collectors, `kg` search, adaptive-telemetry
rules/segments/exemptions (logs/metrics/traces), synth checks/probes, and the
resource-adapter `TruncateSlice` call sites.

Needs per-command decisions:

- **`irm oncall` TypedCRUD lists** (schedules, integrations, escalation
  chains, webhooks, users, teams, …) have **no `--limit` at all** and drain
  the paginated public API fully. Adding the binder there is additive (new
  flag, default 0 keeps behavior); over-fetch `limit+1` gives the truncation
  signal.
- **`incidents list`** rejects `--limit 0` today (`>= 1` validation) — adopting
  the contract is a breaking flag-semantics change; needs a deprecation note.
- **`incidents activity`** coerces `0 → 50` in the client; the client must
  learn a real "all" spelling first.
- **`dashboards list`** already has cursor pagination with a
  `BuildPaginationCommand` hint; converging it on `list_meta` (with
  `continue` carrying the cursor command) is a shape addition, not a rewrite.
- **Bare-array outputs** (`alert rules list`, and any list emitting `[...]`)
  cannot carry `list_meta` until they move to envelopes; `alert rules list`
  is deferred because the bundled `investigate-alert` skill documents
  `gcx alert rules list -o json | jq '.[]…'` (claude-plugin is owned by
  parallel work and the shape change must land together with the skill
  update).
- **`irm oncall alert-groups` bulk-by-filter actions** — `resolveBulkTargets`
  (`internal/providers/irm/oncall_actions.go`) enumerates targets via
  `ListAlertGroupsRaw(ctx, filters, 0)` and discards the returned page info,
  so a sweep whose filter matches more than the 1000-item hard cap silently
  operates on the first 1000 groups. Needs a per-command decision — a bulk
  mutation probably ought to **refuse** on a capped enumeration rather than
  warn.

## 7. Open questions

1. **Should `list_meta` ride only the agents codec?** Currently it is part of
   the JSON/YAML payload for all consumers. Argument for: humans piping
   `-o json` to `jq '.datasources[]'` are unaffected either way (key is
   additive); argument against agents-only: scripts deserve the completeness
   signal too, and split shapes complicate testing. Current implementation:
   all structured formats. Needs a maintainer call.
2. **Retire `adapter.TruncateSlice` centrally?** Once list commands migrate,
   the remaining callers are resource adapters (`List(ctx, limit)` paths)
   where truncation is applied below the command layer and no envelope
   exists. Either the adapter layer returns `(items, truncated bool)` or the
   resources-get pipeline grows its own contract; out of Track C scope.
   The `--json` selection/discovery paths for `unstructured.UnstructuredList`
   likewise do not carry `list_meta` yet — deliberate, since no producer
   attaches it to unstructured lists; wire it up together with this
   migration.
3. **Capped-source binder variant** — a `BindListLimitCapped(..., cap)` with
   wording like "0 means as many as the safety cap (N) allows" would let
   capped sources join the binder; deferred until a second capped source
   appears.
4. **`table` output and `list_meta`** — tables ignore the field by design
   (the stderr hint covers humans). If `wide` output ever needs a footer row
   ("… 50 of 219 shown"), that is a codec concern, not a contract change.
5. **argv fidelity in tests** — continuation commands derive from `os.Args`,
   which tests must pin (`testutils.PinArgv`). A future
   `cmd.CommandPath()`-based derivation could avoid the global, at the cost
   of reconstructing flag spellings; rejected for now to stay aligned with
   the `BuildPaginationCommand(os.Args, …)` precedent.
6. **Explicit source shape vs Total-presence inference** —
   `listContinueCommand` infers "the full set is retrievable via
   `--limit 0`" from the mere presence of `Total`, which forces capped
   sources to withhold an honestly-observed total above the cap (see the
   guard in `alert-groups list`). Making the cap or source shape an explicit
   input to `AttachListMeta` would let observed totals always ride;
   deferred until a second capped source needs it.

## 8. Validation record (2026-07-17, feasibility branch)

- `go test ./internal/output/... ./cmd/gcx/datasources/... ./internal/providers/irm/... ./internal/providers/alert/...` — pass
  (also pass with `GCX_AGENT_MODE=true` forced, guarding agent-harness envs).
- `go build -buildvcs=false -o bin/gcx ./cmd/gcx/` — pass.
- Hint strings locked by exact-match assertions in
  `internal/output/listmeta_test.go::TestEmitListTruncationHint`.

The production extraction (everything except the `alert rules list` rows) is
re-validated in its own PR: full `go test -race ./...`, lint, reference-drift,
and a live main-vs-branch E2E comparison.
