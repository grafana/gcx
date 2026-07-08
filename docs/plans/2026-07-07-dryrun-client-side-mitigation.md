# Design: client-side dry-run mitigation for APIs that ignore `dryRun`

> **Date**: 2026-07-07
> **Status**: draft
> **Related issues**: grafana/gcx#929 (this work), grafana/grafana-enterprise#12569 (upstream server-side fix)

## Problem

gcx implements `resources push --dry-run` and `resources delete --dry-run` purely
as **server-side** dry-run: it forwards `metav1.*Options{DryRun: ["All"]}` to the
Kubernetes-style API and trusts the server to validate-without-persisting.

That assumption is false. All alerting resources are currently served by
legacy-storage bridges — except the alertmanager `config` resource (unified-backed) —
and **none of those bridges honor `dryRun`, so they apply the mutation** instead of
simulating it. A dry-run `push` of an alert rule returns `201 Created` and the rule
actually exists afterwards. Verified on a live stack over both the OAuth /
assistant-proxy path and a direct service-account-token connection, so it is neither
the gcx client nor the proxy: the apiserver itself does not honor `dryRun`.

(Scope: this is the current Grafana Cloud default — no `unified_storage.*.alerting`
config, so these resolve to `StorageModeLegacy`. A resource later migrated to
dual-write/unified would then honor `dryRun`, since dry-run routes to unified storage.)

Root cause (upstream): the alerting legacy storage discards its `*metav1.*Options`
and calls the ngalert provisioning service unconditionally, instead of threading
`dryrun.IsDryRun(opts.DryRun)` into the storage layer the way the generic registry
does (`DryRunnableStorage`). Details and fix options in grafana-enterprise#12569.

We cannot wait on the server fix, and the blast radius is wide: **every alerting CRUD
resource** (alert rules, recording rules, contact points, notification policies,
templates, mute timings, inhibition rules) is legacy-only and unsafe today. More
generally, *any* resource whose effective storage mode is Legacy and whose bridge
doesn't implement `dryRun` is unsafe — so the risk is not alerting-specific, and any
future legacy-only resource inherits it. (Note: resources whose bridge ignores
`dryRun` but which run dual-write/unified — e.g. `shorturl` — are **not** unsafe,
because dry-run never reaches the legacy leg. Safety is decided by mode, not by the
bridge; see "How Grafana actually decides".) gcx needs a client-side mitigation so
`--dry-run` never silently mutates.

## Goals

- `--dry-run` must **never** send a mutating request to an API that does not honor
  `dryRun`. Safety over completeness.
- Preserve today's rich server-side dry-run for resources that genuinely support it
  (dashboards, folders, …).
- Make the degraded behaviour visible: warn on stderr when we fall back to a
  best-effort client-side check.
- Fail safe by default: an unknown resource is treated as "does not honor dryRun".

## Non-goals

- Reproducing server-side **validation** client-side. Best-effort dry-run confirms
  existence, not spec correctness. This is explicitly a weaker guarantee.
- Fixing the server. That is grafana-enterprise#12569.
- Changing behaviour of provider-adapter-backed resources — they already handle
  dry-run correctly client-side (see "Why adapters are safe").

## Background: where dry-run flows today

Two push/delete client paths, routed per-GVK by `ResourceClientRouter`
(`internal/resources/adapter/router.go`):

1. **Provider adapter path** (SLO, synth, read-only alert, …). `typedAdapter`
   (`internal/resources/adapter/typed.go`) already short-circuits on
   `isDryRun(opts.DryRun)` — Create/Update return the validated object without
   calling `CreateFn`/`UpdateFn`, Delete returns nil without calling `DeleteFn`.
   **These are safe and out of scope.**

2. **Dynamic client fallback** (everything without a gcx adapter: dashboards,
   folders, alert rules, recording rules, notification objects, playlists, …).
   `dynamic.NamespacedClient` forwards `opts` straight to `client-go`, which sets
   `?dryRun=All` / a `DeleteOptions` body. **This is the vulnerable path** — dry-run
   correctness depends entirely on the server, and some servers ignore it.

Relevant call sites:
- `internal/resources/remote/pusher.go` — `upsertResource` does `client.Get` first,
  then `client.Update` (L284 / L311, natural-key path) or `client.Create` (L321),
  each with `DryRun: dryRunOpts`.
- `internal/resources/remote/deleter.go` — `deleteResource` (L128) calls
  `client.Delete` with `DeleteOptions{DryRun: dryRunOpts}`; **no prior Get**.
- `internal/resources/remote/remote.go` — `buildRouter(dynamicClient, registry)`
  (L27) is the single seam shared by both `NewDefaultPusher` and `NewDefaultDeleter`.
- `cmd/gcx/resources/validate.go:125` also sets `DryRun: true` — a third consumer to
  account for (see Impact).

## Design

### 1. An allowlist keyed by GroupResource

**Decision: allowlist which resources DO honor server-side dry-run; default-deny.**

Why an allowlist (default-deny) rather than a denylist (default-allow):
- A wrong "honors" entry → silent mutation (the exact bug we are fixing). A wrong
  "does not honor" entry → we downgrade to a best-effort check + warning, which is
  merely conservative. The asymmetry means unknowns must default to "does not honor".

**Granularity: `GroupResource` (group + plural resource), not full GVK or bare Group.**
Rationale:
- dry-run support is a property of the **storage backend**, which Grafana configures
  per `unified_storage.<resource>.<group>` (`dualWriterMode`). GroupResource is the
  exact unit Grafana itself keys on.
- A single group can host resources with different backends, so bare Group is too
  coarse. Version rarely changes the storage decision, so full GVK is unnecessarily
  narrow (and churns on version bumps). GroupResource is the sweet spot.

Shape (illustrative — final location TBD, likely `internal/resources/remote`):

```go
// staticServerDryRunAllowlist is the built-in set of resources that honor dryRun
// regardless of storage mode (see "How Grafana actually decides"). folders and
// playlists have no legacy leg at all; dashboards default to Mode 5 (unified) via
// MigratedUnifiedResources enforcement.
var staticServerDryRunAllowlist = map[schema.GroupResource]struct{}{
    {Group: "dashboard.grafana.app", Resource: "dashboards"}: {},
    {Group: "folder.grafana.app",    Resource: "folders"}:    {},
    {Group: "playlist.grafana.app",  Resource: "playlists"}:  {},
}

// dryRunAllowlist is the effective set for a run: static seed ∪ per-context config
// ∪ --assume-server-dry-run flag values (see "Escape hatch"). Membership means
// "known/asserted to honor dryRun". Default is false (fail safe).
type dryRunAllowlist struct{ extra map[schema.GroupResource]struct{} }

func (a dryRunAllowlist) honors(gr schema.GroupResource) bool {
    if _, ok := staticServerDryRunAllowlist[gr]; ok {
        return true
    }
    _, ok := a.extra[gr] // user-asserted
    return ok
}
```

**Seed criterion:** a resource is seeded only if its handler respects `dryRun`
**regardless of storage mode** — i.e. either it has *no legacy leg at all* (served
purely by the generic registry) or it is enforced to unified storage by default.
Verified in grafana OSS:

- `folder.grafana.app/folders` and `playlist.grafana.app/playlists` — **no legacy
  leg**: served directly by the generic registry (`folders/register.go:230`
  `b.storage = NewRegistryStore...`; playlist has no `GetLegacyStorage`/dualwriter).
  Unconditionally safe in every mode.
- `dashboard.grafana.app/dashboards` — has a dualwriter + a legacy leg
  (`dashboard_storage.go`) that ignores `dryRun`, but is **enforced to Mode 5 by
  default**: `MigratedUnifiedResources` (`setting_unified_storage.go:34`) maps
  Dashboard→`true` ("Only Mode5!"), and the enforcement loop overrides config to Mode 5
  whenever migrations run (`storage_type=unified` + target `all`/`core`, the standard/
  Cloud default). The Mode-0 legacy leg is only reachable via a non-default opt-out
  (`enableMigration=false`, non-unified storage type, or a non-core target).

Everything else is denied by default — including all alerting groups (never migrated,
legacy-only) and resources mapped `false` in `MigratedUnifiedResources` that still have
a dryRun-ignoring legacy leg (stars, preferences, datasources, shorturls, snapshots).

#### How Grafana actually decides dry-run support (verified in grafana OSS, 2026-07-08)

The determinant is **not** "does the resource's `legacy_storage.go` honor dry-run"
— it is **which storage the request is routed to**, chosen per-GroupResource by the
effective storage mode. `storageService.NewStorage`
(`pkg/storage/legacysql/dualwrite/storage_service.go` L53) resolves a mode and returns:

- **Unified** (config Mode 4/5) → returns unified storage directly → honors dry-run.
- **DualWrite** (config Mode 1-3) → returns a `dualWriter` that, on dry-run,
  **routes to unified regardless of read mode** (`dualwriter.go` Create L218-219,
  Delete L317-318, Update L365-369 — *"which already handles dry-run correctly via
  DryRunnableStorage"*) → honors dry-run.
- **Legacy** (config Mode 0 / unset default → `storageModeFromConfigMode`) → returns
  the legacy bridge → honors dry-run **only if that bridge implements it** (most don't).

Unified honors dry-run because it sits behind the upstream generic registry, whose
`DryRunnableStorage.Create/Update/Delete` skip persistence on `dryRun` (confirmed in
`k8s.io/apiserver` `.../generic/registry/dryrun.go`).

Three consequences that correct earlier assumptions:

1. **A resource can be safe even if its `legacy_storage.go` ignores dry-run**, as long
   as it's served via unified/dual-write (Mode ≥ 1) — the legacy leg is never hit on
   dry-run. E.g. `shorturl`'s legacy bridge discards options, but shorturl is
   dual-write-configured, so its dry-run safety depends on mode, not the bridge. Do
   **not** allowlist/denylist based on `legacy_storage.go` contents.
2. **The strongest guarantee is "no legacy leg".** A resource served purely by the
   generic registry (no `GetLegacyStorage`, no dualwriter) has no mode in which a
   dry-run persists. Verified for `folders` (`register.go:230`) and `playlists`.
3. **Default mode matters.** `MigratedUnifiedResources` (`setting_unified_storage.go:34`)
   marks Dashboard/Folder/Playlist `true` ("Only Mode5!") and the enforcement loop
   (L361-378) overrides any config to Mode 5 when migrations run (the standard/Cloud
   default). So even dashboards — which *do* have a dryRun-ignoring legacy leg — are
   Mode 5 by default; the legacy leg is only reachable via a deliberate non-default
   opt-out. Resources mapped `false` (stars, preferences, datasources, shorturls,
   snapshots) are **not** migrated by default → legacy → unsafe. Alerting is not in the
   map at all (never migrated, legacy-only) → the empirical `201 Created` reproduced.

**Correctness caveat (must be documented in code):** effective mode is per-stack and
dynamic (migration status reader, else the enforced default, else
`unified_storage.<gr>.dualWriterMode` config). A static allowlist can be wrong only for
a resource whose safety depends on the default *and* whose deployment has taken a
non-default opt-out. Folders/playlists are immune (no legacy leg); dashboards carry
this residual risk. We accept it because the opt-out is deliberate and non-Cloud, and
the escape hatch (below, to *add*) covers it in the interim. The robust long-term fix
is server-side: once the apiserver honors dryRun or rejects an unsupported dryRun with
`400`/`422` (grafana-enterprise#12569), gcx can detect-and-fall-back generically and
the allowlist becomes vestigial. Detecting support via discovery is not possible
(Grafana does not advertise dry-run capability there), and a dynamic storage-status
check is only a loose proxy (see open question #6) — neither is a substitute for the
server contract.

Granularity confirmed as `GroupResource`: `getStorageMode` keys on `gr.String()`
(group + resource) and the `unified_storage` config is keyed the same way, so
per-GroupResource matches Grafana's own unit exactly — bare Group would be wrong for a
group that migrates resources independently.

### 2. A dry-run guard decorating the dynamic client

Wrap `dynamicClient` at the `buildRouter` seam with a decorator that implements
`adapter.DynamicClient`. It only intercepts **mutating verbs carrying `DryRun`**;
read verbs (Get/List/GetMultiple) and all non-dry-run calls pass straight through.
Because it wraps only the dynamic fallback, the adapter path is untouched.

```go
router := buildRouter(newDryRunGuard(dynamicClient, warnWriter), registry)
```

Per-verb behaviour when `DryRun` is set:

| Verb | Allowlisted (honors dryRun) | NOT allowlisted (best-effort) |
|------|------------------------------|-------------------------------|
| **Create** | pass through: send `?dryRun=All`, server validates, no persist | warn on stderr; **do not send**; report `would create <name> (not verified)` → **skipped** |
| **Update** | pass through | warn on stderr; **do not send**; existence already known (pusher Got it) → report `would update <name> (not verified)` → **skipped** |
| **Delete** | pass through | warn on stderr; do a `Get` to confirm existence; **do not send Delete**; report `would delete <name>` / `not found — nothing to delete` → **skipped** |

Best-effort checks (non-allowlisted), cheapest first — none mutate:
1. **Parse/decode** — already done by `FSReader`; malformed files fail at load.
2. **Structural/envelope** — GVK known & supported (the pusher already checks
   `supported[gvk]`), `apiVersion`/`kind`/`metadata.name` present.
3. **Existence** (update/delete only) — the `Get` the pusher/guard already performs.

This is the **initial cut**: it confirms the operation is well-formed and (for
update/delete) that the target exists, but does not validate the spec or any server-side
semantics — referential integrity (folder/datasource UIDs), uniqueness/collisions,
admission webhooks, quota, domain rules (e.g. valid PromQL in an alert rule). Hence
"not verified".

**Follow-up (separate change): client-side schema validation.** Validate the spec
against the OpenAPI schema gcx already fetches (`discovery.SchemaFetcher.FetchSpecSchemas`)
to catch required fields/types/enums/formats. gcx has no schema *validator* today (only a
fetcher), so this needs a new JSON-schema validator dep and ships after the precedent-
based mitigation. Tracked as its own item; not in the first change.

Key points:
- The guard never calls the underlying `Create`/`Update`/`Delete` on the non-honoring
  path — that is the whole safety property.
- **Outcome = `skipped`, not failure or success.** This follows the existing gcx
  precedent for "the API can't do the real operation, and it isn't the user's fault":
  `puller.go` records unsupported/unlistable resources via `summary.RecordSkipped()`,
  *skips silently regardless of `StopOnError`*, and the CLI reports skipped as a
  distinct count that does **not** affect the exit code (`pull.go` raises
  `NewPartialFailureError` only on `FailedCount() > 0`). We mirror that: best-effort
  dry-run results are recorded as skipped, `--on-error fail` does not turn them into a
  non-zero exit, and the summary shows them separately.
- **Signalling:** the guard returns a typed sentinel (e.g. `errDryRunUnverified`) that
  the pusher/deleter recognise and translate to `RecordSkipped()` (not
  `RecordFailure`). Create/Update/Delete on the non-honoring path all resolve to
  skipped; the per-resource would-create/update/delete detail goes to the log/warning,
  not the success count — so gcx never claims it "pushed"/"deleted" something it
  deliberately didn't send.
- **Initial cut:** create/update are recorded skipped (not verified); delete additionally
  confirms existence. When the schema-validation follow-up lands, a schema *violation*
  becomes a genuine `RecordFailure` (bad input to fix), distinct from `skipped` (couldn't
  server-verify) — but until then everything on this path is skipped or a real
  infrastructure error (bad `Get`, processor error).

### 3. Warning UX

- Write to **stderr** (`cmd.ErrOrStderr()`), not stdout, so `--output json`/agent-mode
  payloads on stdout stay clean. Use `cmdio.Warning` (`internal/output/messages.go`).
- Deduplicate: one warning per distinct GroupResource per invocation, not per
  resource — a bulk push of 200 alert rules should warn once.
- Message (draft):
  `warning: <group>/<resource> does not support server-side dry-run; checked client-side only (not verified). No changes were sent.`

## Impact / behaviour changes

- **`resources push --dry-run`** on non-allowlisted resources: no longer mutates.
  Would-create/update/change are reported as **skipped** (not verified), not as
  pushed and not as errors.
- **`resources delete --dry-run`**: now does an existence check instead of sending a
  (server-honored-or-not) delete; result recorded as **skipped**.
- **`resources validate`** (`validate.go:125`, `DryRun: true`): today it leans purely on
  server-side dry-run. Initial cut: allowlisted resources validate as today; a
  non-allowlisted resource is **skipped** ("cannot validate — server-side dry-run
  unsupported"), reported honestly rather than as a false "valid" or an error (precedent:
  `puller.go` skip). The schema-validation follow-up makes `validate` genuinely useful
  for these by checking the spec against the fetched OpenAPI schema. See open question #4.
- **Exit codes:** best-effort results are recorded as `skipped`, which — following the
  `pull` precedent — does **not** trip `--on-error fail` and keeps exit 0. Only genuine
  failures (bad Get, processor error, etc.) count toward `FailedCount()` and the
  non-zero exit. The summary line gains a skipped count, e.g.
  `N pushed, 0 errors (M skipped — dry-run not supported server-side, not verified)`.

## Why adapters are safe (no change needed)

`typedAdapter.{Create,Update,Delete}` already check `isDryRun(opts.DryRun)` before
touching the backend (`internal/resources/adapter/typed.go` L375-458), returning the
round-tripped object (Create/Update) or nil (Delete) without side effects. The guard
deliberately wraps only the dynamic fallback so this behaviour is preserved.

## Escape hatch: extend the allowlist at runtime

Rather than a blunt "skip the guard entirely" switch, let users **add resources** to
the allowlist for a run (or persist them per-context). This keeps the fail-safe
default while giving power users — who know a given stack runs a resource on
dual-write/unified — an escape without disabling the guard for everything.

**Flag** (on `push`/`delete`, and `validate` if it keeps server dry-run):

```
--assume-server-dry-run <groupresource>   # repeatable, or comma-separated
```

- Value is a `GroupResource` string, i.e. `<resource>.<group>` — the same form as
  `schema.GroupResource.String()` and Grafana's `unified_storage` keys, e.g.
  `alertrules.rules.alerting.grafana.app`.
- **Augments** the static allowlist for that invocation; it never removes entries.
- Repeatable: `--assume-server-dry-run a.grp --assume-server-dry-run b.grp`, or CSV.

**Per-context config** (persistence, optional): a list under the context, e.g.
`resources.assume-server-dry-run: [<groupresource>, ...]`, merged with the flag and
the static allowlist.

**Resolution:** effective allowlist = static seed ∪ config list ∪ flag values. The
guard checks membership against this merged set. When a resource is honored only
because the user asserted it (i.e. not in the static seed), print a one-line stderr
note so it's clear the safety assertion came from the user, not gcx.

Deliberately **not** supported (or gated behind an explicit `all`, discouraged):
a wildcard that reinstates the old blanket-trust behaviour. Keeping it per-resource
preserves the fail-safe posture — a typo drops you back to best-effort, not silent
mutation.

## Affected code (implementation surface)

- `internal/resources/remote/remote.go` — wrap `dynamicClient` in `buildRouter`; thread
  a stderr writer to the guard.
- `internal/resources/remote/dryrun_guard.go` *(new)* — the decorator implementing
  `adapter.DynamicClient`.
- `internal/resources/remote/dryrun_allowlist.go` *(new)* — static seed +
  `dryRunAllowlist.honors`; parse/merge the `--assume-server-dry-run` values and
  per-context config into the `extra` set.
- `internal/resources/remote/{pusher,deleter}.go` — plumb the warn writer and the
  resolved `dryRunAllowlist` into `NewDefaultPusher`/`NewDefaultDeleter`; recognise the
  guard's `errDryRunUnverified` sentinel and call `summary.RecordSkipped()` (not
  `RecordFailure`), bypassing `StopOnError` — mirroring `puller.go`'s skip handling.
- `internal/config` — new per-context list `resources.assume-server-dry-run`
  ([]string of GroupResource); loader + config reference docs.
- `cmd/gcx/resources/{push,delete}.go` — register the `--assume-server-dry-run` flag
  (repeatable/CSV), merge with config, pass `cmd.ErrOrStderr()`; add a skipped count to
  the summary line (as `pull.go` already does). `validate.go` — see open question #4.
- `internal/resources/remote/summary.go` — reuse existing `RecordSkipped`/`SkippedCount`
  (already present; no new outcome type needed).
- **Client-side schema validation** — *follow-up change, not the initial cut.* A
  validator checking a resource spec against the schema from
  `discovery.SchemaFetcher.FetchSpecSchemas`; needs a JSON-schema validator
  (`k8s.io/kube-openapi/pkg/validation`, already in the dep tree, or
  `santhosh-tekuri/jsonschema`). Wired into the guard best-effort path and `validate`;
  a schema violation becomes `RecordFailure`, distinct from `skipped`.

## Implementation steps

1. Add `staticServerDryRunAllowlist` + `dryRunAllowlist.honors(GroupResource)` seeded
   with dashboards + folders + playlists; table-driven test (static, config, flag, merged).
2. Add `dryRunGuard` decorator (DynamicClient) with the behaviour matrix above; unit
   tests with a fake DynamicClient asserting **no** mutating call on the non-honoring
   path, and pass-through on the honoring (static or user-asserted) path.
3. Add the `--assume-server-dry-run` flag + `resources.assume-server-dry-run` config
   key; parse GroupResource strings, merge (static ∪ config ∪ flag), and emit the
   "honored by user assertion" stderr note.
4. Wire the guard + resolved allowlist into `buildRouter`; thread the warn writer from
   push/delete cmds. Map the guard's skip sentinel to `RecordSkipped` in pusher/deleter.
5. Warning dedupe + stderr routing; skipped-count in the push/delete summary line
   (as `pull.go` does); snapshot/CLI test.
6. Docs: `DESIGN.md`/error-model note that `--dry-run` / `validate` are best-effort
   (client-side, not verified) for non-honoring APIs; config + CLI reference for the new
   flag/key; reference grafana-enterprise#12569.

**Follow-up (separate change):** client-side schema validation — add a JSON-schema
validator, feed it schemas from `SchemaFetcher`, wire into the guard best-effort path
and into `validate` for non-allowlisted resources (schema failure → `RecordFailure`).
Upgrades those outcomes from `skipped` to structurally-validated. See open question #4.

## Testing

- Unit: allowlist lookup; guard per-verb behaviour (fake client asserts no mutating
  verb sent when not allowlisted; asserts `?dryRun` pass-through when allowlisted).
- Unit: warning emitted once per GroupResource; goes to stderr.
- Unit: non-allowlisted dry-run records `skipped` (not failure); `--on-error fail` still
  exits 0 when only skips occurred; real failures still exit non-zero.
- Integration/CLI: `push --dry-run` and `delete --dry-run` against a stubbed dynamic
  client for both an allowlisted and a non-allowlisted GVK; assert the skipped count in
  the summary line.
- Regression: adapter-backed dry-run unchanged (SLO/synth path).
- (Follow-up) client-side schema validation — well-formed spec passes, schema-violating
  spec is `RecordFailure`, resource with no fetchable schema still skips (no false
  failure).

## Open questions for review

1. ~~Allowlist granularity~~ — **settled: GroupResource** (matches Grafana's
   `getStorageMode`/`unified_storage` keying; see "How Grafana actually decides").
2. ~~Seed list~~ — **settled: dashboards + folders + playlists.** Criterion is "handler
   respects dryRun regardless of mode": folders/playlists have no legacy leg
   (unconditional); dashboards are enforced Mode 5 by default via
   `MigratedUnifiedResources`. All other k8s-native resources (stars, preferences,
   datasources, shorturls, snapshots, all alerting) are denied. (gcx-adapter-backed
   resources like datasources are already safe via `typedAdapter` and are out of scope.)
3. ~~Unverifiable dry-run create: error vs skipped?~~ — **settled: skipped, exit 0**,
   following the `puller.go`/`pull.go` precedent (`RecordSkipped`, bypasses
   `StopOnError`, distinct summary count, not gated by `--on-error fail`). Applies to
   all non-allowlisted create/update/delete best-effort outcomes.
4. ~~`validate` behaviour on non-honoring resources.~~ — **settled.** Initial cut:
   allowlisted validate as today; non-allowlisted are **skipped** (precedent: `puller.go`
   `RecordSkipped`, exit 0), reported honestly, not falsely "valid". Client-side schema
   validation is a **follow-up** (new JSON-schema validator dep) that upgrades those from
   skipped to structurally-validated.
5. ~~Ship the escape hatch?~~ — **in scope: `--assume-server-dry-run` flag + per-context
   config**, both augment (never replace) the static seed. Confirm flag name and whether
   `validate` gets it too.
6. ~~Dynamic storage-status check to replace the static allowlist?~~ — **rejected.**
   Storage mode is a loose proxy for dryRun support: `unified`/`dualwrite` reliably
   honor it, but `legacy` does **not** reliably mean "ignores" — some legacy handlers
   accept and simulate dryRun. A mode check never opens a safety hole (it only ever
   downgrades to best-effort) but it doesn't *measure* dryRun support — it re-applies
   our architectural mapping at runtime, which can drift as Grafana changes handlers,
   for a bigger dependency and no definitive answer.

   The only definitive signals are (a) the server honors dryRun (no-op) or (b) the
   server **rejects** an unsupported dryRun with `400`/`422` — and a rejected request
   cannot have persisted, so falling back to best-effort is safe. (b) is the clean
   long-term contract and is already the alternative fix in grafana-enterprise#12569.
   Once the server honors-or-rejects, gcx needs **no allowlist**: send dryRun, and on a
   dryRun-specific `400` fall back to best-effort. Until then, the static allowlist is
   the pragmatic interim; it becomes vestigial once #12569 lands.
