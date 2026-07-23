---
type: feature-plan
title: "Config format rework: stacks / cloud / contexts split"
status: implemented
spec: https://github.com/grafana/gcx/issues/890#issuecomment-5004314096
created: 2026-07-20
---

# Config Format Rework (#890)

The accepted architecture and security boundaries are recorded in
[ADR-022](../adrs/config-v1/001-versioned-split-config-and-secret-trust.md).

> **Historical implementation plan:** The security, backup, keychain,
> layered-migration, and write-back details below were superseded by ADR-022
> and the current architecture documentation. Do not treat those sections as
> normative behavior.

Restructure the gcx config from per-context everything into kubeconfig-style
named sections: `stacks:` (Grafana connection + providers), `cloud:` (named
grafana.com auth entries), `contexts:` (thin bindings of stack + cloud +
datasource defaults). Full proposed format and rules:
[issue comment](https://github.com/grafana/gcx/issues/890#issuecomment-5004314096).

## Target shape (condensed)

```yaml
version: 1                    # new - first explicitly versioned format; legacy format has none
current-context: ops

resources:                    # global; per-stack override merges by union
  assume-server-dry-run: [...]

stacks:
  raintank-ops:
    slug: raintankops         # optional; today's CloudConfig.Stack, moved here
    grafana: { server: ..., oauth-token: ..., ... }   # today's GrafanaConfig, unchanged
    providers: { synthetic-monitoring: { url: ... } }
    resources: { assume-server-dry-run: [...] }

cloud:
  raintank:
    token: "..."              # or oauth-token + oauth-token-expires-at
    api-url: https://grafana-ops.com    # optional, default grafana.com
    oauth-url: https://grafana-ops.com

contexts:
  ops:
    stack: raintank-ops       # required ref into stacks:
    cloud: raintank           # optional ref into cloud:; no ref = no cloud functionality
    datasources: { prometheus: grafanacloud-raintank-prom }
```

Key rules (details in the issue comment):

- cloud binding is explicit and optional; dangling ref = validation error;
  missing ref = runtime error on cloud-dependent ops, with a recovery hint
- ~~`cloud.<name>.stacks` is the CAP realm~~ dropped from v1 (see resolved
  question 12): what a credential can see is runtime discovery, not config
- legacy `default-*-datasource` fields dropped (breaking format change anyway)
- `current-context`, `diagnostics`, `telemetry` unchanged

## Design decisions

| Decision | Rationale |
|----------|-----------|
| New format is `version: 1`; the legacy format stays unversioned | the version field is a property of the new schema family — legacy is detected by shape, never by number. Loader rule: version absent + legacy shape → migrate; future migrations key off the number |
| Loader auto-migrates legacy→v1 on load, silently, with a `.bak` of the old file and a one-line notice | precedent: plaintext→keychain migration already rewrites config on load. A prompt breaks agent/CI use (gcx is heavily agent-driven); a `gcx config migrate` command means every code path supports both formats until the user runs it — dual support in loader/merge/editor/env-overrides is where the real cost is. Backup covers the rollback hazard (old binary can't parse the new format — codec rejects unknown fields) |
| Read-only config file → migrate in memory + warn, don't fail | same graceful degradation as keychain-unavailable path |
| `Context` remains the resolved read surface | `.Grafana`/`.Providers` consumers are pervasive (rest.go, stack_id.go, login, every provider via ConfigLoader). Context keeps `Stack`/`Cloud` name refs on disk plus unexported resolved pointers (populated at load, `yaml:"-"`), exposed via accessors. Consumers change mechanically or not at all; write paths use explicit helpers that know the real home (stack entry vs cloud entry vs context) |
| Migration dedups identical cloud configs | the whole point of #890 is not repeating the same org token per context. Contexts whose resolved (token, api-url, oauth-url) tuples match collapse into one cloud entry |
| Migrated cloud entries named from the api-url host (`grafana-com`, `grafana-ops-com`) | migration runs inside `Load` with no network, so org-slug naming is impossible offline; the host describes the cloud environment, which is what the entry models. Collision (two distinct tokens, same host) → suffix with the first context name (`grafana-com-prod`). `gcx cloud login` may later rename to org-slug-based names once discovery runs |
| Context→stack migration is strictly 1:1, no stack dedup | Grafana auth is genuinely per-context today (separate oauth tokens per login); collapsing risks merging distinct credentials. Users consolidate by hand |
| No legacy `gcx config set` path aliases | breaking format change anyway; old paths fail with an error naming the new path (e.g. `cloud.token` → "use cloud.<entry>.token"). No dual grammar to maintain |
| Login suffixes host-named entries on credential collision (like migration) | a second `gcx cloud login` with a different CAP from an unbound context would otherwise quietly replace the `grafana-com` entry other contexts share; same-credential logins still dedup. Sentinels are resolved before comparing; unresolvable compares as different (fail toward not clobbering) |
| One credential per stack entry is the intended v1 model | two identities against one stack (human vs CI, read-only vs admin) = two stack entries. Stated in docs; a future version can add credential indirection on stack entries without breaking v1 files |
| New keychain key scheme: `stack:<name>:<field>` and `cloud:<name>:<field>` | today's `AccountKey` is `<context>:<field>` (`internal/credentials/credentials.go:69`); secrets move homes, so migration must write new entries and delete old ones or sentinels dangle |
| Env overrides synthesize an ephemeral cloud entry | `GRAFANA_CLOUD_TOKEN` et al. win over whatever the context references; entry never persisted |
| Stack and cloud entries are atomic across layers (same name → highest-priority layer wins wholesale, no field merge) | field-merging same-named entries lets a repo-local `.gcx.yaml` contribute a `server`/`api-url` to an entry whose token lives in the user config — routing a personal credential to a repo-chosen destination (from [#890 discussion](https://github.com/grafana/gcx/issues/890#issuecomment-5029465359)). Wholesale entries give the invariant that a credential and its destination always come from the same file; a hostile repo layer can only shadow (break), not exfiltrate. Migration's predictable entry names (`grafana-com`) made the old field-merge behaviour targetable. Kubeconfig takes named entries wholesale for the same reason. Contexts stay field-merged: they carry refs and datasource defaults, no secrets or destinations, and refs can only select whole entries |
| Write paths materialize the full entry in the write layer | with atomic entries, writing a partial stack into a layer that lacks it would shadow a lower layer's fuller entry wholesale; `SaveProviderConfig` copies the effective stack entry into the target file when creating it there |

## Work breakdown

### 1. New types + validation (`internal/config/types.go`)

- `StackConfig{Slug, Grafana *GrafanaConfig, Providers, Resources}`
- `CloudEntry{Token, OAuthToken, OAuthTokenExpiresAt, APIUrl, OAuthUrl, Orgs, Stacks}`
- `Config` gains `Version int`, `Stacks map[string]*StackConfig`,
  `Cloud map[string]*CloudEntry`, `Resources *ResourcesConfig`
- `Context` slims to `Stack string`, `Cloud string`, `Datasources map[string]string`
  (+ resolved accessors); drop legacy `Default*Datasource` fields
- validation: context refs must resolve; move `GrafanaConfig.Validate` triggering accordingly
- `ResolveStackSlug` (types.go:247) reads `StackConfig.Slug`, else derives from
  server URL (order unchanged); `ResolveCloudAPIURL` reads the cloud entry
- `Context.AssumeServerDryRun()` returns builtin ∪ global ∪ stack union

### 2. Loader: dual decode + auto-migration (`internal/config/loader.go`, `keychain.go`)

- sniff shape before strict decode (top-level `stacks:`/`version:` vs `contexts.*.grafana`);
  decode legacy format into legacy structs, convert, persist via existing `Write`
  path — hook exactly where `migratePlaintextSecrets` runs (loader.go:388-397)
- per-context conversion: context `foo` → stack `foo` (grafana + providers +
  slug from `Cloud.Stack`), cloud entry (deduped + host-named, see decisions),
  context `foo` referencing both; per-context `resources` → stack entry
- keychain re-key: resolve legacy sentinels, write under new keys, **never delete
  the old ones** — `.legacy.bak` sentinels point at the legacy keys, so deleting
  them turns the backup into a config full of dangling sentinels. Legacy keys must
  also be exempted from `reconcileKeychain`'s staleness cleanup (it runs on every
  `Write` and would GC them on the first ordinary config write). Cleanup is a
  deliberate follow-up once the format has soaked. Rework `AllFields`/`fieldRef`
  (`internal/config/keychain.go:58-129`) for the three homes
  (stack: grafana-token/password/oauth-*/sm-token; cloud: cloud-token/oauth-token)
- self-verify before replacing the file: decode the just-serialized bytes back as
  the new format and run the validation invariant in-process, then atomic-rename —
  converter/serialization bugs surface while the legacy file is still untouched
- backup `<path>.legacy.bak` + one-line stderr notice. Ordering: run the existing
  plaintext→keychain sentinelization first, back up the **sentinelized** legacy
  file, then convert — a raw-bytes backup would retain plaintext secrets for
  users jumping from pre-keychain versions (old binaries resolve sentinels, so
  rollback still works). Backup is write-once: never overwrite an existing
  `.legacy.bak` (an old binary running during the upgrade window can rewrite
  the file to legacy and trigger re-migration)
- `MergeConfigs` (merge.go) gains `stacks:`/`cloud:` merge arms for layered configs

**Migration failure model** — invariants: migration deletes nothing (file replaced
atomically with write-once backup; keychain copy-only); the legacy file is never
modified until the new state is durably written and self-verified; a failed run
leaves a working config and retries on the next load (detection is by shape, so
migration is idempotent).

| Failure | Behaviour |
|---------|-----------|
| legacy parse | strict decode as legacy shape; failure → loader error exactly as today, file untouched |
| conversion | pure in-memory transform; unexpected shape → clear loader error, file untouched, user can downgrade or hand-edit |
| backup write | abort persist entirely; in-memory migration + warn (never persist without the rollback safety) |
| keychain re-key | copy-only, never delete — legacy keys are what keep `.legacy.bak` restorable. Sentinels embed their own account key, so on keychain failure the new config persists with the *old* sentinel strings, which still resolve. Crash mid-sequence leaves at worst duplicate keychain entries, which are harmless |
| config write | existing `Write` is atomic (temp file + rename) — old or new file, never torn |
| persistently unwritable config (CI) | in-memory migration + warn on every invocation — correct for baked-in configs |

Requires: sentinel resolution stays keyed by the sentinel's embedded account key
(not recomputed from config structure), and `reconcileKeychain` must preserve —
not re-key — sentinels whose values it cannot read.

Migration does **not** gate on semantic validation: validation stays at point of
use (as today — `LoadConfigTolerant` skips it for config-editing commands), so a
broken unused context can't block every command. Conversion is total — every
legacy field has a defined home — so invalid configs migrate to equally invalid
configs and fail at the same moment they would have before. Test invariant:
a context fails validation after migration iff it failed before (dangling refs
excluded — migration only writes refs to entries it creates).

### 3. Env overrides (two duplicated sites)

- `internal/config/envparse.go` `ParseEnvIntoContext` → parse into stack/cloud/context split;
  `GRAFANA_CLOUD_*` builds ephemeral cloud entry
- rewire both callers: `cmd/gcx/config/command.go:44-114` (LoadConfigTolerant)
  and `internal/providers/configloader.go:82-181` (envOverride/cloudEnvOverride)

### 4. Write paths

- `gcx login` (`internal/login/login.go` persistContext/mergeAuthIntoExisting):
  create/update stack entry + context binding; populate `slug` when resolved
- `gcx cloud login` (`cmd/gcx/cloud/command.go`, `internal/config/cloud_login.go`):
  create/update a cloud entry, bind it to the current context
- `gcx config set/unset` (`internal/config/editor.go`, `path.go`): paths are
  LITERAL — they name the exact location in the file, starting from a
  top-level section; nothing resolves against the current context. Bare and
  legacy paths error with the absolute path spelled out (computed from the
  current context, copy-pasteable). Initially implemented with
  ownership-routed bare paths; replaced with literal paths after review
  (resolved question 13)
- `SaveProviderConfig`/`SaveDatasourceUID` (`internal/providers/configloader.go:364-461`):
  providers → stack entry, datasource UIDs → context
- OAuth refresh persistence (`internal/config/rest.go` WireTokenPersistence):
  write refreshed tokens into the stack entry

### 5. Runtime realm discovery + org-ambiguity guard (`internal/cloud`, PR 3)

Redesigned after the [#890 discussion](https://github.com/grafana/gcx/issues/890#issuecomment-5029465359)
(empty-response ambiguity) and #949 (stack create silently landing in the
account's default org):

- no login auto-fill and no realm fields in config — what a credential can
  see is checked at runtime, when an operation needs it
- discovery is tri-state: `discovered` / `unknown-forbidden` (403, missing
  scope, OAuth-rejected route) / `unknown-error` (transient). The realm is
  NEVER inferred from an empty or failed response
- org-ambiguous GCOM mutations (e.g. stack create) refuse and require
  `--org` when discovery returns more than one org or unknown — gcx must not
  let grafana.com pick a default org it cannot see (#949)
- add org-unscoped `ListInstances` to `internal/cloud` for the discovery call
- any caching goes to the XDG state dir with a timestamp (stack-id cache
  precedent), never into the config file
- prerequisite before building: confirm with the GCOM owners that the
  unscoped `/api/instances` (and `/api/orgs`, if used) behaviour is a stable
  contract — accepted token types, server-side realm filtering, and error
  shapes. The tri-state guard depends on distinguishing 403-forbidden from a
  legitimate empty 200; today that's verified against the route
  implementation, not a documented contract
- (done in PR 1) `oauth-token-expires-at` read with a "run `gcx cloud login`"
  expiry error; GCOM doesn't report a lifetime today so the field is
  populated only defensively or by hand

### 6. Runtime errors for missing cloud binding

- cloud-dependent ops (`internal/providers/configloader.go` loadCloudBase,
  stack discovery, SM token minting) fail with a hint; when exactly one cloud
  entry exists, name it in the message

### 7. Docs + reference

- `GCX_AGENT_MODE=false mise run reference` (config reference regen — CI drift check)
- `docs/architecture/config-system.md` rewrite; CLAUDE.md package-map line if needed
- changelog migration note

### 8. Tests

- legacy→v1 migration golden fixtures (incl. keychain re-key, dedup, backup, read-only fallback)
- new-format load/validate/merge/editor-path/envparse tables
- update fixtures: `internal/config/testdata/config.yaml`,
  `cmd/gcx/config/testdata/*.yaml`; `internal/login/login_test.go` is the
  behavioural spec for login persistence

## Suggested PR sequencing

1. **PR 1**: types + loader + migration + merge + env overrides + tests (steps 1-3) —
   everything reads/writes the new format, all commands keep working
2. **PR 2**: write paths + config set grammar + runtime errors (steps 4, 6)
3. **PR 3**: runtime realm discovery + org-ambiguity mutation guard (step 5)

Docs/reference regen ride along with each PR (CI enforces drift).

PR 1 is the bulk of the work; the layered-merge behaviour is where the estimate
is most likely to slip. If the milestone (2026-07-27) gets tight, PR 3 (realm
discovery) is the cut line — the format lands intact without it.

## Hazards

- keychain entries keyed by context name — migration must re-key or sentinels dangle
- codec rejects unknown fields: old binary + new-format file = hard parse failure (mitigated by `.bak` + notice)
- env override logic duplicated in two packages — both must move in the same PR
- `gcx config set` dot-paths are user-facing API; document the new grammar
- layered configs (system/user/local): migration should only rewrite the layer it loaded
  from; merging a legacy layer with a new-format layer must work during the transition.
  The hairy part: today `mergeContexts` merges *same-named contexts across layers
  field-by-field* (e.g. local layer overlays a cloud token onto the user layer's
  `prod`). Post-migration that becomes ref merging: a partial context in one layer
  migrates to partial artifacts (cloud entry + context with only a `cloud:` ref, no
  stack) and relies on the merge to complete it — needs its own test table. And
  host-based entry naming can mint `grafana-com` in two layers with different
  tokens; map-level merge shadows the lower layer's entry wholesale, silently
  swapping which token its contexts resolve → migration must qualify entry names
  per layer on collision
- cloud-entry dedup is best-effort: comparing tokens requires resolving sentinels,
  and sentinel strings always differ per context. Keychain unavailable → no dedup,
  one entry per context (correct, but document it)
- docs and skills embed legacy-format config YAML (setup-gcx / scaffold-project
  skill markdown, `docs/architecture/config-system.md`, config reference); the
  skills drift test validates commands/flags only — manual sweep required
- `gcx config set grafana.server` on a context now edits its *stack*, which other
  contexts may share — inherent to the kubeconfig model; document, don't warn

## Resolved questions (with Daf, 2026-07-20)

1. migration UX: silent auto-migrate on load + `.legacy.bak` backup + one-line notice
2. migration dedups identical cloud configs into one entry
3. realm discovery (step 5) is in scope, as its own PR (PR 3) — later redesigned
   as runtime discovery + mutation guard, see resolved question 12
4. explicit version field: yes — new format is `version: 1`, legacy format stays
   unversioned and is detected by shape
5. migrated cloud entries named from api-url host; collision → suffix first context name
6. no legacy `gcx config set` path aliases — clear error pointing at the new path
7. context→stack migration strictly 1:1, no stack dedup
9. stack/cloud entries merge atomically across layers (top layer wins wholesale);
   contexts keep field-level merge — see decisions table for the security rationale
11. OAuth-issued cloud tokens live in `oauth-token` (+`oauth-token-expires-at`
   when the flow reports a lifetime), CAP tokens in `token`; readers prefer
   `token`, fall back to `oauth-token`, and an expired OAuth token errors with
   "run `gcx cloud login`". Setting one credential clears the other (an entry
   holds one credential). Legacy configs migrated OAuth tokens as `token`
   (indistinguishable from CAPs); the next `gcx cloud login` moves them
13. `gcx config set` paths are literal (the path you type is the path in the
   file); no bare-path routing through the context's stack ref. Routing was a
   mini-DSL to learn and made `grafana.server` silently edit a stack other
   contexts share; literal paths are self-documenting against `config view`.
   Bare/legacy paths error with the exact absolute path, computed from the
   current context
12. `orgs`/`stacks` dropped from the v1 cloud entry: they were a snapshot of
   what discovery saw at login, not authoritative config, and the empty
   `/api/orgs` response is ambiguous (stack realm vs missing scope vs error) —
   auto-filling `stacks` on it could freeze a snapshot for an org-realm token.
   Config holds user assertions only; discovery is runtime with an explicit
   unknown state (PR 3), and #949-style org-ambiguous mutations get a guard
   instead of a cache
10. migration covers every file the loader touches (all discovered layers, explicit
   files on use); the shadowed duplicate user config and `.gcx.yaml` in unvisited
   directories migrate on first use; the diagnostics pre-read is legacy-aware
   (read-only) so `telemetry: disabled` in a not-yet-migrated file is honoured
8. migration deletes nothing: file replaced atomically behind a write-once backup,
   keychain copy-only (legacy keys keep the backup restorable, exempt from
   staleness GC); new bytes self-verified by decode-back + validation invariant
   before the rename; legacy keychain cleanup is a post-soak follow-up
