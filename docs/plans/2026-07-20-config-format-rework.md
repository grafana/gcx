---
type: feature-plan
title: "Config format rework: stacks / cloud / contexts split"
status: draft
spec: https://github.com/grafana/gcx/issues/890#issuecomment-5004314096
created: 2026-07-20
---

# Config Format Rework (#890)

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
    orgs: [daf1]              # populated at login (realm discovery)
    stacks: [dafstack1]       # CAP stack realm = grafana.com slugs, NOT local stack keys

contexts:
  ops:
    stack: raintank-ops       # required ref into stacks:
    cloud: raintank           # optional ref into cloud:; no ref = no cloud functionality
    datasources: { prometheus: grafanacloud-raintank-prom }
```

Key rules (details in the issue comment):

- cloud binding is explicit and optional; dangling ref = validation error;
  missing ref = runtime error on cloud-dependent ops, with a recovery hint
- `cloud.<name>.stacks` is the CAP realm (grafana.com slugs); absent = whole org;
  login must not auto-fill it for org-realm tokens
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
| New keychain key scheme: `stack:<name>:<field>` and `cloud:<name>:<field>` | today's `AccountKey` is `<context>:<field>` (`internal/credentials/credentials.go:69`); secrets move homes, so migration must write new entries and delete old ones or sentinels dangle |
| Env overrides synthesize an ephemeral cloud entry | `GRAFANA_CLOUD_TOKEN` et al. win over whatever the context references; entry never persisted |

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
- `gcx config set/unset` (`internal/config/editor.go`, `path.go`): new dot-path
  grammar for `stacks.<name>.*` / `cloud.<name>.*`; context-relative bare paths
  resolve through the refs (e.g. `grafana.server` on current context → its stack);
  legacy paths get a clear error naming the new path, no aliases
- `SaveProviderConfig`/`SaveDatasourceUID` (`internal/providers/configloader.go:364-461`):
  providers → stack entry, datasource UIDs → context
- OAuth refresh persistence (`internal/config/rest.go` WireTokenPersistence):
  write refreshed tokens into the stack entry

### 5. Cloud realm discovery at login (`internal/cloud`, `cmd/gcx/cloud`)

- add org-unscoped `ListInstances` to `internal/cloud` GCOM client
- realm detection per the issue comment: `/api/orgs` non-empty → org realm
  (write `orgs`, omit `stacks`); empty → stack realm (write both from instances);
  OAuth → orgs derived from instances
- store `oauth-token-expires-at`; expiry error says "run `gcx cloud login`"

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
3. **PR 3**: realm discovery at cloud login (step 5)

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
3. realm discovery (step 5) is in scope, as its own PR (PR 3)
4. explicit version field: yes — new format is `version: 1`, legacy format stays
   unversioned and is detected by shape
5. migrated cloud entries named from api-url host; collision → suffix first context name
6. no legacy `gcx config set` path aliases — clear error pointing at the new path
7. context→stack migration strictly 1:1, no stack dedup
8. migration deletes nothing: file replaced atomically behind a write-once backup,
   keychain copy-only (legacy keys keep the backup restorable, exempt from
   staleness GC); new bytes self-verified by decode-back + validation invariant
   before the rename; legacy keychain cleanup is a post-soak follow-up
