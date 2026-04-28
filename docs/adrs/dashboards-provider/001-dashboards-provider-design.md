# Dashboards provider: CRUD shorthands, search, and version history

**Created**: 2026-04-21
**Status**: accepted (research complete — see [Research Findings](#research-findings))
**Bead**: none
**Supersedes**: none

<!-- Status lifecycle: proposed -> accepted -> deprecated | superseded -->

## Context

### Problem

Dashboards are the most commonly touched Grafana resource, but gcx has no
provider-level commands for them. Users must fall back to
`gcx resources get dashboards/<name>` for single-dashboard operations and
`gcx resources pull -p .` for bulk operations. This is ergonomically poor
for the resource type users interact with most, and inconsistent with
every other provider (SLO, Synthetic Monitoring, IRM, k6, etc.) that
exposes a dedicated command surface.

The Grafana 12+ Kubernetes-compatible dashboard API (`dashboard.grafana.app`)
also exposes capabilities that have no CLI surface today:

- Full-text **search** by title, tag, folder at
  `/apis/dashboard.grafana.app/v{preferred}/namespaces/{ns}/search` —
  dashboard-only (the endpoint does not generalize across resource types).
- **Version history** and restore — exact endpoint shape (K8s subresource
  on the dashboard, separate CRD, or fallback to the legacy
  `/api/dashboards/uid/:uid/versions` + `/restore` routes) needed
  verification against a live instance.

This ADR decides how to fill those gaps.

### Sources

- [docs/plans/2026-04-14-ux-consistency-design.md](../../plans/2026-04-14-ux-consistency-design.md) §D10 — Dashboard Provider
- [docs/plans/2026-04-14-ux-consistency-design.md](../../plans/2026-04-14-ux-consistency-design.md) §D9 — Contracts 1 (CRUD Table Columns), 2 (JSON/YAML Envelope), 5 (Imperative vs Declarative Separation), 7 (Action Verbs)
- [docs/research/2026-04-21-dashboards-api-capabilities-grafana-kubernetes-provider.md](../../research/2026-04-21-dashboards-api-capabilities-grafana-kubernetes-provider.md) — companion research report resolving the five Research Dependencies below (7 domains, 47 sources, 89% overall confidence)
- [CONSTITUTION.md](../../../CONSTITUTION.md) — providers are self-registering plugins; K8s envelope is the file format
- Existing precedent: [internal/providers/sigil/eval/templates/commands.go](../../../internal/providers/sigil/eval/templates/commands.go) for `versions` sub-command shape
- Existing dashboards command tree: [cmd/gcx/dashboards/command.go](../../../cmd/gcx/dashboards/command.go), currently hosting only `snapshot`
- Resources pipeline entrypoints reused by the new provider:
  [internal/resources/dynamic/](../../../internal/resources/dynamic) (K8s dynamic client — primary surface),
  [internal/resources/discovery/](../../../internal/resources/discovery) (preferred-version resolution),
  [internal/resources/remote/](../../../internal/resources/remote) (Pusher/Puller/Deleter — used by the declarative `resources push/pull` path, **not** by this provider's imperative CRUD),
  [internal/resources/adapter/typed.go](../../../internal/resources/adapter/typed.go) (TypedCRUD[T])

### Research status

This ADR was drafted ahead of the research pass that would firm up the
wire-protocol details for search, version history, restore, preferred
version, and list-payload completeness. That pass has completed — see
[docs/research/2026-04-21-dashboards-api-capabilities-grafana-kubernetes-provider.md](../../research/2026-04-21-dashboards-api-capabilities-grafana-kubernetes-provider.md).
The findings are folded into the Decision section below; the closing
[Research Findings](#research-findings) section summarizes each resolution
and lists the residual live-instance probes recommended pre-merge.

### Constraints (from the sources above)

- CONSTITUTION — providers are self-registering plugins via `init()` +
  `providers.Register()`, contributing commands through `Commands()` and
  optionally adapter registrations through `TypedRegistrations()`.
- D9 Contracts 1, 2, 5, 7 — authoritative; this ADR applies them, does not
  re-litigate them.
- `gcx resources get/push/pull/delete` behavior for dashboards is preserved
  unchanged — the shorthands are strictly additive.
- `gcx dashboards snapshot` keeps working with no agent-mode breakage.
- K8s-envelope YAML is the single file format shared between
  `dashboards create -f` / `update -f` and `resources push`.
- Search stays provider-only (endpoint is dashboard-specific).

### Out of scope

- Alert rules write support (separate Area 8b ADR).
- Dashboard permissions management (future work).
- Auto-detection of stale dashboards / pruning heuristics (future work).
- Migration from legacy `/api/dashboards/db` endpoints (gcx is K8s-API-first).

## Decision

### Command tree (v1)

```
gcx dashboards list
gcx dashboards get <name>
gcx dashboards create -f FILE
gcx dashboards update <name> -f FILE
gcx dashboards delete <name> [--yes]
gcx dashboards search <query> [--folder UID]... [--tag TAG]... [--limit N] [--sort KEY] [--deleted]
gcx dashboards versions list <name> [--limit N]
gcx dashboards versions restore <name> <version> [--yes] [--message MSG]
gcx dashboards snapshot <name>                      # already exists — moved, not replaced
```

Identifier in every position is called `<name>` — the slug-id that lives in
`metadata.name`. The CLI surface and help text never use "UID" or "ID";
documentation notes that for dashboards, `metadata.name` is the same value
the Grafana UI calls the "dashboard UID", for users migrating from the
legacy `/api/dashboards/uid/:uid` endpoints. The second positional on
`versions restore` is `<version>` — an integer generation number, not a
resource identifier (Contract 5's `<name>` convention therefore does not
apply to it).

Note: `list` does **not** expose a `--folder` flag. Folder filtering is
only available via `search --folder UID`. Rationale: folder is stored as
a dashboard annotation (`grafana.app/folder`), not a label, so the K8s
LIST labelSelector cannot filter by it. See
[List-payload completeness](#list-payload-completeness) for detail.

### Provider architecture — new package `internal/providers/dashboards/`

A new self-registering provider package is created at
`internal/providers/dashboards/`, following the layout of
`internal/providers/slo/` (provider root + one sub-package per command
group). The package:

- Defines `DashboardsProvider` implementing the
  [providers.Provider](../../../internal/providers/provider.go) interface.
- Self-registers in `init()` via `providers.Register()`.
- Contributes a single root `dashboards` Cobra command with CRUD, search,
  versions (list + restore), and the existing snapshot as subcommands.
- **Does not** contribute a `TypedRegistrations()` entry — dashboards are
  already served by the discovery-based dynamic path in the resources
  pipeline. Registering a redundant typed adapter would create two
  registration paths for the same GVK and force a synthetic "Dashboard"
  struct around what is fundamentally arbitrary JSON. The provider
  returns `nil` from `TypedRegistrations()` and is a "commands-only"
  provider.
- `Validate()` returns `nil` and `ConfigKeys()` returns `nil` — dashboards
  use Grafana's built-in authentication, same as SLO.

The provider's CRUD command implementations delegate to the K8s dynamic
client in the resources pipeline
(`internal/resources/dynamic.NamespacedClient`) scoped to the dashboards
GVR resolved via
[discovery.Registry](../../../internal/resources/discovery/registry.go).
`create`, `update`, and `delete` call `NamespacedClient.{Create, Update,
Delete}` directly — **not** `remote.Pusher`/`Puller`. Pusher/Puller
implement upsert/batch semantics for the declarative `gcx resources push`
pipeline, which is incompatible with the fail-fast Contract 5 semantics
of imperative commands (`create` must 409 on duplicate; `update` must
404 on missing). `versions list` and `versions restore` are also pure
K8s LIST + GET + UPDATE via `NamespacedClient` — see the wire-protocol
subsections below. Only `search` (and, transitively, nothing else —
`list` does not expose `--folder`) needs a purpose-built thin HTTP
client, because the `/search` endpoint is v0alpha1-pinned and lives
outside the dynamic client's verb surface.

**Alternatives considered and rejected** (for this decision only):

1. *Commands under `cmd/gcx/dashboards/` with no provider package* —
   simplest (one less layer), but violates the CONSTITUTION invariant that
   providers are self-registering plugins, and dashboards would not appear
   in `gcx providers list`. Rejected.
2. *Provider package + TypedRegistrations entry backed by a
   `TypedCRUD[Dashboard]` wrapper* — consistent with the SLO/OnCall/Faro
   shape, but a `Dashboard` struct is necessarily a thin wrapper around
   `map[string]any` or `unstructured.Unstructured` (panel configs are
   arbitrary JSON with no stable schema across versions). Typed adapters
   add value when they enable typed codecs and validation; here they would
   only duplicate what discovery already gives us for free. Rejected.
3. *Provider package without the resources-pipeline delegation — build a
   standalone dashboards REST client* — would duplicate the dynamic-client
   plumbing, disagree on retry/auth transport, and diverge over time.
   Rejected.
4. *CRUD via `remote.Pusher`/`Puller`* — this was the pre-research draft.
   Research revealed Pusher has upsert/batch semantics incompatible with
   Contract 5 fail-fast on imperative `create`/`update`. Rejected; CRUD
   now goes through `dynamic.NamespacedClient` directly.

### Identifier terminology

Per Contract 5, every positional *resource identifier* is `<name>`. The
CLI surface, help text, error messages, and documentation never use
"UID" or "ID". A single sentence in `docs/reference/cli/gcx_dashboards.md`
(auto-generated) and in the user-facing dashboards-as-code guide notes
that for dashboards, `metadata.name` is the same value the Grafana UI
historically labels "Dashboard UID", so users copying a UID out of the
browser can paste it verbatim. The `<version>` positional on `versions
restore` is an integer generation number, not a resource identifier;
Contract 5's `<name>` convention does not apply.

### Interaction with `gcx resources`

Both paths coexist and are each canonical for their intent:

- **Imperative (this provider)**: `gcx dashboards create -f file.yaml`,
  `gcx dashboards update <name> -f file.yaml`,
  `gcx dashboards delete <name>`. Explicit, fails fast on the wrong state
  (create fails on duplicate, update fails if missing).
- **Declarative (GitOps)**: `gcx resources push -p ./dashboards/`,
  `gcx resources pull -p ./dashboards/`,
  `gcx resources delete -p ./dashboards/`. Idempotent upsert across a
  directory.

Both accept and produce the same K8s envelope YAML/JSON format
(Contract 2). A file that round-trips through `dashboards get <name> -o yaml`
is directly consumable by `resources push` and vice versa. No format
divergence is permitted.

Bulk listing continues to be `gcx dashboards list` (for interactive use)
and `gcx resources pull -p dir` (for GitOps export). We do not add a
`dashboards pull` or `dashboards push` — those are Contract 5 anti-patterns
that the SLO provider is already being asked to remove.

### Snapshot integration

`gcx dashboards snapshot` stays at the same command path — no rename, no
break for agent mode. Its implementation moves from
[cmd/gcx/dashboards/snapshot.go](../../../cmd/gcx/dashboards/snapshot.go) into
`internal/providers/dashboards/snapshot/` as a subcommand contributed by
the new provider. The current
[cmd/gcx/dashboards/command.go](../../../cmd/gcx/dashboards/command.go) is
deleted (the provider's `Commands()` returns the root `dashboards` command
that previously lived there).

The `Short` description on the `dashboards` command changes from
"Render Grafana dashboard snapshots" (currently pointing users to
`gcx resources` for CRUD) to a single-line summary covering the full
CRUD + search + snapshot surface.

### Verb contract application (from D9)

- **Contract 1 (table columns)** — `list` and `get` default columns:

  ```
  Table:  NAME  TITLE  FOLDER  TAGS  AGE
  Wide:   NAME  TITLE  FOLDER  TAGS  PANELS  URL  AGE
  ```

  **STATUS column is intentionally dropped for dashboards** — dashboards
  have no runtime state (no "enabled/failing/ok"), and Contract 1 explicitly
  qualifies STATUS as applicable only "if the resource has a state". The
  column is omitted and this choice is documented as a Contract 1 exception
  in [docs/design/output.md](../../../docs/design/output.md). See
  [Open question 1](#open-questions) for the alternative of surfacing
  `schemaVersion` as a separate column.

- **Contract 2 (envelope)** — `get -o yaml|json`, `list -o yaml|json`,
  `search -o yaml|json`, and `versions list -o yaml|json` all produce
  the standard `apiVersion / kind / metadata / spec` envelope. `spec`
  is the raw dashboard JSON. The one exception is `search`, where the
  server returns `DashboardHit` items (not full Dashboard objects);
  gcx wraps those in a `DashboardSearchResultList` envelope for
  Contract 2 compliance — see [Search command](#search-command).

- **Contract 5 (imperative vs declarative)** — covered above under
  "Interaction with `gcx resources`". `-f FILE` behaves identically here as
  in every other provider; K8s envelope is the single file format.

- **Contract 7 (action verbs)** — `versions restore` is a nested-noun
  action verb on an existing resource. It takes two positionals
  (`<name>` for the dashboard slug-id; `<version>` for the target
  revision generation number), prompts on stderr by default, takes
  `--yes` for non-interactive operation, and emits `cmdio.Success` to
  stderr on completion. The two-positional shape is a documented
  extension of Contract 7's "exactly one positional" rule: the rule
  applies to bare action verbs (like `create`/`update`/`delete`); a
  nested noun/verb action verb may carry a second positional when it is
  a non-name qualifier (here, an integer generation number).

### Wire-protocol decisions (resolved by research)

Each subsection below states the firm decision and summarizes the research
evidence. Full detail and citations: see the research report referenced in
Sources above.

#### Search command

URL and verb — version is pinned to `v0alpha1`:

```
GET /apis/dashboard.grafana.app/v0alpha1/namespaces/{ns}/search
```

The server registers search storage only under v0alpha1; for every other
requested version it returns `nil`. The Grafana UI hardcodes the
`v0alpha1` literal regardless of its negotiated CRUD version. gcx does
the same — the search URL uses the literal `v0alpha1` and is **not**
routed through `discovery.Registry`. This is the one documented
exception to the "never hardcode versions" rule.

Flag surface:

```
gcx dashboards search <query> [--folder UID]... [--tag TAG]... [--limit N] [--sort KEY] [--deleted]
```

- `<query>` — free-text positional; empty permitted when a filter is supplied.
- `--folder UID` — repeatable; folder UID (same value as a folder's
  `metadata.name`). Folder-path syntax not supported in v1; defer to a
  follow-up amendment if user feedback requests it.
- `--tag TAG` — repeatable; **AND semantics** confirmed server-side
  (multiple tags filter to dashboards carrying every listed tag).
- `--limit N` — page size.
- `--sort KEY` — sort key (e.g., `name_sort`, `-name_sort` for descending).
- `--deleted` — include soft-deleted (trashed) dashboards.
- The `type` query parameter filters the result set server-side. The
  legacy value `dash-db` is silently ignored by the server (live probe
  confirmed — identical hit mix with and without it). The modern value
  `dashboard` (no hyphen) IS honored: gcx sends `type=dashboard` and
  the server excludes folders from the response. No client-side filter
  is required.

Response payload — **not** full Dashboard objects. The endpoint returns
a flat JSON payload with no top-level `kind`/`apiVersion`:

```json
{
  "hits": [
    {"resource": "dashboards", "name": "<uid>", "title": "<title>",
     "folder": "<folder-uid>", "tags": ["tag1", "tag2"]}
  ],
  "maxScore": 0.23,
  "queryCost": 317863,
  "totalHits": 65
}
```

Each hit carries `resource` (`"dashboards"` or `"folders"` — plural),
`name` (slug-id), `title`, `folder` (UID), and `tags` (omitted when
empty) — enough for the default table columns. Panel count is **not**
returned, so wide-mode PANELS is omitted for search hits (documented
in the CLI reference). The Go types `SearchResults` / `DashboardHit`
referenced in the research report are server-side Go struct names,
not wire-format keys.

**`type=dash-db` is silently ignored server-side** (confirmed by live
probe — identical `totalHits` and hit mix with and without the
parameter). The modern form **`type=dashboard`** (no hyphen) IS
honored: the server filters folders out of the response. gcx sends
`type=dashboard` and does not apply a client-side `resource` filter.

Default table columns: `NAME TITLE FOLDER TAGS AGE` (list codec minus PANELS).

Pipeline class **(B) — K8s-aggregated non-standard verb.** Needs a thin
HTTP client built via `rest.HTTPClientFor(&cfg.Config)` — inherits auth
and retry transport from the resources pipeline's already-built
`*rest.Config`.

`-o json|yaml` envelope — because the response is not a full Dashboard,
gcx wraps hits in a dashboards-specific envelope for Contract 2
compliance. The envelope contains only dashboards-typed hits (folders
are excluded by the server via `type=dashboard`):

```yaml
kind: DashboardSearchResultList
apiVersion: dashboard.grafana.app/v0alpha1
items:
  - kind: DashboardHit
    apiVersion: dashboard.grafana.app/v0alpha1
    metadata:
      name: "<uid>"
    spec:
      title: "<title>"
      folder: "<folder-uid>"
      tags: ["tag1", "tag2"]
```

Search output is explicitly **not** a round-trip source for
`resources push` — hits are metadata-only. Users who want to export
after searching should pipe through `dashboards get <name> -o yaml`.

#### Version history

Version history is a **plain K8s LIST** on the main dashboards collection
with magic selectors — not a subresource, not a separate CRD, not a
legacy REST endpoint:

```
GET /apis/dashboard.grafana.app/{v}/namespaces/{ns}/dashboards
  ?labelSelector=grafana.app/get-history=true
  &fieldSelector=metadata.name={uid}
  &limit={N}
```

- `labelSelector=grafana.app/get-history=true` activates history-LIST
  mode server-side. Without it the endpoint returns current dashboards.
- `fieldSelector=metadata.name={uid}` is **mandatory** — server-side
  field-selector parsing only honors `metadata.name`.
- Standard K8s pagination: `limit` and `continue`.

Response payload — each item is a **full Dashboard object** with the
historical `spec`. `metadata.generation` is the version identifier;
the `grafana.app/updatedTimestamp` annotation is the revision
timestamp (see note under the column table below); the
`grafana.app/message` and `grafana.app/updatedBy` annotations carry
the commit message and author.

CLI:

```
gcx dashboards versions list <name> [--limit N]
```

Revisions listed in descending order by generation. Default columns:

| Column    | Source |
|-----------|--------|
| VERSION   | `metadata.generation` |
| TIMESTAMP | annotation `grafana.app/updatedTimestamp` (humanized) |
| AUTHOR    | annotation `grafana.app/updatedBy` (empty if absent) |
| MESSAGE   | annotation `grafana.app/message` (empty if absent) |

Note: **TIMESTAMP comes from the `grafana.app/updatedTimestamp`
annotation, not `metadata.creationTimestamp`.** Live probe confirmed
that `creationTimestamp` is identical across all revisions of a
dashboard — it is inherited from the original create, not per-revision.
The `grafana.app/updatedTimestamp` annotation varies per revision and
is the correct source.

AUTHOR and MESSAGE may be empty on revisions created by older Grafana
versions or API integrations that do not set these annotations (live
probe found MESSAGE empty on every historical revision of the sample
dashboard). Render empty strings (not `<nil>` or errors).

Pipeline class **(A) — fully reuses `dynamic.NamespacedClient.List()`
as-is.** No new HTTP client. The implementation passes
`metav1.ListOptions` with the label + field selectors above.

`-o json|yaml` envelope — standard `kind: DashboardList`. Already
Contract 2 compliant; no custom wrapper.

#### Versions restore

CLI:

```
gcx dashboards versions restore <name> <version> [--yes] [--message MSG]
```

- `<name>` is the dashboard slug-id (`metadata.name`).
- `<version>` is the integer `metadata.generation` returned by
  `versions list`.
- Prompts on stderr unless `--yes` is set (Contract 7).
- `--message MSG` overrides the default annotation value
  `"Restored from version N"`.
- Emits the new version number on success via `cmdio.Success`.

Implementation — **K8s-compound sequence** (mirrors the Grafana UI):

1. LIST history for `<name>` (see Version history above); pick the
   target revision's full spec.
2. GET the current dashboard to fetch its `metadata.resourceVersion`
   (optimistic-concurrency token).
3. Construct an update object: current metadata (with `resourceVersion`
   for optimistic concurrency) + historical spec + annotation
   `grafana.app/message = "Restored from version N"` (overridable via
   `--message MSG`).
4. PUT (update) the dashboard. Server increments `metadata.generation`
   → append semantics (new revision created, old revisions retained).

Pipeline class **(A) — pure K8s LIST + GET + UPDATE.** Reuses
`dynamic.NamespacedClient`; no new HTTP client. Two HTTP round-trips
instead of one, which is negligible for an interactive operation.

Concurrency and edge cases:
- Concurrent modification between step 2 and step 4 surfaces as HTTP 409
  from the `resourceVersion` precondition — the command reports the
  conflict and exits non-zero; users can retry.
- No-op short-circuit: if target generation equals current generation,
  the command exits success without issuing a PUT (avoids creating a
  redundant revision).

**Alternative rejected — legacy REST path.** The endpoint
`POST /api/dashboards/uid/{uid}/restore` with body `{"version": N}` also
works and yields identical append semantics. It is rejected because:
(a) the Grafana UI has stopped using it in favor of the K8s-compound
path; (b) the server-side Swagger marks it "will be removed when
/apis/dashboards.grafana.app/v1 is released" (v1 has shipped — the
removal clock is ticking); (c) it forces a new thin HTTP client for a
single command (pipeline class (D) — legacy REST under `/api/`, not
`/apis/`); (d) it has no request-body slot for a commit message, so
`--message` would be unsupported.

**Command shape — `versions restore` adopted (closes [OQ2](#open-questions)).**
A flat action verb `rollback <name> --to <version>` was the pre-research
leaning. After research and interactive review, the ADR adopts nested
noun/verb `versions restore <name> <version>`:
- Groups semantically with `versions list` under the `versions` noun.
- Matches the sigil precedent (`sigil templates versions …`).
- Two positionals are terser than positional + `--to` flag for the
  common case and read naturally as "restore dashboard X version 3".

**Subcommand group shape vs CONSTITUTION's `$VERB-$CHILD` example.**
The CONSTITUTION's command grammar shows `$VERB-$CHILD` (e.g.
`gcx dashboards list-versions`) as an illustrative shape for child
operations. With a single child operation that pattern is viable;
with two child operations (`list` and `restore`) a dedicated
`versions` subcommand group is cleaner and more extensible. Critically,
`restore` has no natural hyphenated sub-verb form — `restore-version`
reads awkwardly and inverts the conventional verb-first grammar that
`versions restore` preserves. Using a `versions` noun group also
mirrors the established sigil precedent (`sigil templates versions …`),
keeps both operations discoverable under one help page, and leaves room
for future children (e.g. `versions diff`) without requiring new
top-level verbs. This constitutes a deliberate, justified departure
from the literal `$VERB-$CHILD` example in the CONSTITUTION; the
underlying `$NOUN $VERB` shape is consistent with Contract 7's
action-verb semantics.

#### Version-discovery strategy

- Default: use the preferred version reported by
  [discovery.Registry](../../../internal/resources/discovery/registry.go)
  for `dashboard.grafana.app`, via `LookupPartialGVK()` — identical to
  the resources pipeline. gcx **never hardcodes** the CRUD version.
  (The `v0alpha1`-only search endpoint is the one documented exception.)
- Override: `--api-version` flag on each command for explicit pinning
  (matches the flag shape used by `gcx resources get`).

Preferred version in current deployments: **`v2`** in both OSS 12.x
(default) and Grafana Cloud. This is gated by `FlagDashboardNewLayouts`,
which is GA with `expression: "true"` in both default profiles. The
`dashboard.grafana.app` group registers six versions (`v0alpha1`, `v1`,
`v1beta1`, `v2alpha1`, `v2beta1`, `v2`); `discovery.Registry` handles
resolution and gcx treats the negotiated version as opaque.

Breaking spec-shape change across v1 → v2:

- **v1**: `spec.panels` — array (grid layout).
- **v2**: `spec.elements` + `spec.layout` — scenes / new-layout model.

Implication for gcx: `spec` is treated as **opaque JSON for CRUD**
(Contract 2 — the envelope is the contract, not the spec shape). The
only code path that needs version-awareness is the table codec's
wide-mode PANELS column: branch on the object's `apiVersion` and walk
`len(spec.panels)` (v1) or `len(spec.elements)` (v2). No migration or
spec rewriting is performed client-side.

#### List-payload completeness

The `list` endpoint returns **full Dashboard objects per item** (full
metadata + full spec). No per-item fan-out is required — `dashboards
list` is a single HTTP round-trip per page.

Column → field mapping:

| Column | Field |
|--------|-------|
| NAME   | `metadata.name` |
| TITLE  | `spec.title` |
| FOLDER | annotation `grafana.app/folder` (empty → rendered as "General") |
| TAGS   | `spec.tags` (comma-separated) |
| AGE    | `metadata.creationTimestamp` (humanized) |
| PANELS (wide) | `len(spec.panels)` (v1) or `len(spec.elements)` (v2) |
| URL (wide)    | Synthesized client-side: `{grafana-url}/d/{name}/{slug}` |

Pagination is standard K8s (`limit` + opaque `continue` token). gcx's
default `--limit` is 100 (consistent with other providers); `--limit 0`
or `--all` iterates to completion. Pipeline class **(A) — reuses
`dynamic.NamespacedClient.List()`**.

**`list` does not expose `--folder`.** Folder is stored on dashboards
as an **annotation** (`grafana.app/folder`), not a label. The K8s LIST
labelSelector cannot filter by annotation. Three options were
considered; `list` intentionally omits the flag and routes users to
`search --folder UID` instead:

| Option | Verdict |
|--------|---------|
| Delegate `list --folder` to the search endpoint internally | Rejected — hidden endpoint switch; PANELS disappears; couples `list` to the v0alpha1 search surface |
| **Omit `--folder` from `list`; require `search --folder UID`** | **Adopted** — clean separation; one endpoint per command; no hidden-switch surprise |
| Client-side filter-all (LIST entire namespace, filter by annotation) | Rejected — O(N) per namespace; prohibitive at scale |

Implication: CLI help on `list` surfaces a one-line note directing users
to `gcx dashboards search --folder UID` when they want folder-filtered
listings. The user-facing manage-dashboards guide documents the split.

Server-side selector support on the dashboards LIST verb (for reference):

| Selector | Value | Effect |
|----------|-------|--------|
| `fieldSelector=metadata.name=<uid>` | — | Filter by exact name. |
| `labelSelector=grafana.app/get-history=true` | — | Activate history-LIST mode. |
| `labelSelector=grafana.app/get-trash=true` | — | Return soft-deleted dashboards. |
| `labelSelector=grafana.app/deprecatedInternalID=<n>` | — | Resolve legacy numeric ID. |

Other selectors return either full or silently-empty results.

## Consequences

### Positive

- Dashboards get the same imperative-command surface every other provider
  has, closing the biggest ergonomic gap in gcx.
- Search exposes a K8s-native capability that has no CLI surface today.
- Delegation to `dynamic.NamespacedClient` for CRUD, versions, and
  restore means zero duplication of auth/retry/transport/discovery
  machinery, and only `search` needs a new thin HTTP client.
- The commands-only-provider pattern (provider package contributes
  `Commands()` but returns `nil` from `TypedRegistrations()`) is validated
  as a first-class option in the provider-system playbook, useful for any
  future resource that is already K8s-discoverable but wants a dedicated
  command surface.
- `list` is a single HTTP round-trip per page — full Dashboard objects
  are returned by the LIST verb, so no per-item fan-out is required to
  populate any default or wide-mode columns.
- Snapshot is preserved at its current command path; no agent-mode break.

### Negative

- The "commands-only provider" variant is novel in this codebase — other
  providers either register adapters (SLO, OnCall, Faro) or are
  non-adapter REST-API wrappers (IRM, synth). Contributors may look for a
  `TypedRegistrations()` entry here and be briefly confused. Mitigated by a
  comment in `provider.go` explaining the intentional decision.
- Folder filtering is split across two commands: `list` without `--folder`
  and `search --folder UID`. Users who expect `list --folder` to work
  will need to re-learn. Mitigated by a one-line CLI-help note on `list`
  and explicit coverage in the manage-dashboards guide.
- `versions restore` carries two positionals instead of the one-positional
  shape Contract 7 describes for bare action verbs. This is documented as
  an extension: nested noun/verb action verbs may take a second
  non-name qualifier positional. Contributors adding future nested
  action verbs should follow the same convention.
- Dashboards gain two command paths (`gcx dashboards ...` and
  `gcx resources ... dashboards/...`). Users need a clear mental model of
  imperative vs declarative; this is already codified in Contract 5 but
  will require user-education via the command help text.

### Neutral / follow-up

- `cmd/gcx/dashboards/snapshot.go` moves into the provider package. Import
  paths for the snapshot logic change; any downstream imports will need
  to update.
- [DESIGN.md](../../../DESIGN.md) / the ADR table in
  [ARCHITECTURE.md](../../../ARCHITECTURE.md) need an entry for this
  ADR (ADR 016) now that it is `accepted`.
- [claude-plugin/skills/manage-dashboards/](../../../claude-plugin/skills/manage-dashboards/)
  should be updated to reference the new CRUD commands alongside the
  existing `resources` path, and to document the `list` / `search
  --folder` split.
- If future user feedback requests additional search filters (e.g.,
  `--starred` — not confirmed supported by the v0alpha1 endpoint in the
  research pass; folder-path syntax — not confirmed either), the flag
  set should be widened in a follow-up amendment to this ADR, not a
  separate ADR.

## Research Findings

The five research dependencies identified when this ADR was drafted are
resolved by the companion research report
[docs/research/2026-04-21-dashboards-api-capabilities-grafana-kubernetes-provider.md](../../research/2026-04-21-dashboards-api-capabilities-grafana-kubernetes-provider.md)
(7 research domains, 47 sources, 89 citations, 89% overall confidence).
The resolutions are folded into the Decision section above; this section
recaps each one and lists the residual live-instance probes recommended
pre-merge.

1. **Search endpoint capabilities — resolved.** URL
   `GET /apis/dashboard.grafana.app/v0alpha1/namespaces/{ns}/search`,
   version pinned to `v0alpha1` (server registers the endpoint only
   under v0alpha1; UI hardcodes the literal). Query params: `query`,
   `folder` (UID, repeatable), `tag` (AND semantics, repeatable),
   `limit`, `sort`, `deleted`. **`type=dash-db` accepted but silently
   ignored** (live probe — identical results with and without it); the
   modern `type=dashboard` value IS honored server-side. gcx sends
   `type=dashboard`; no client-side filter is needed. Response is a flat JSON
   payload (no top-level `kind`/`apiVersion`); each hit carries
   `resource`, `name`, `title`, `folder`, and optional `tags`. gcx
   wraps hits in a `DashboardSearchResultList` envelope for Contract 2
   compliance. Pipeline class (B) — one new thin HTTP client. Residual:
   folder-path syntax and `starred` filter were not confirmed; both
   omitted from v1.

2. **Version-history endpoint shape — resolved.** Plain K8s LIST on the
   main dashboards collection with magic selectors
   (`labelSelector=grafana.app/get-history=true&fieldSelector=metadata.name={uid}`).
   Not a subresource; not a separate CRD; not the legacy REST endpoint.
   Each item is a full Dashboard object; `metadata.generation` is the
   version identifier; `grafana.app/message` / `grafana.app/updatedBy`
   annotations carry the commit message and author. Pipeline class (A)
   — reuses `dynamic.NamespacedClient.List()` as-is; no new HTTP client.
   This reclassification removes a client surface that the pre-research
   ADR draft assumed was necessary.

3. **Restore (rollback) semantics — resolved; path chosen.** Two viable
   paths, both with identical append semantics (new generation = current
   + 1). ADR chooses the **K8s-compound path** (LIST history → GET
   current → PUT with historical spec + `grafana.app/message`
   annotation), matching what the Grafana UI does — pipeline class (A),
   zero new client surface. The legacy REST path
   (`POST /api/dashboards/uid/{uid}/restore` with body `{"version": N}`)
   is explicitly rejected: class (D), UI has stopped using it, endpoint
   marked in Swagger for eventual removal, no `--message` slot.

4. **Preferred API version — resolved.** `v2` in both OSS 12.x (default)
   and current Grafana Cloud — `FlagDashboardNewLayouts` is GA with
   `expression: "true"` in both default profiles. gcx uses
   `discovery.Registry.LookupPartialGVK()` and never hardcodes a CRUD
   version (the v0alpha1-only search endpoint is the one documented
   exception). Breaking spec-shape change across v1 → v2: `spec.panels[]`
   (grid) → `spec.elements` + `spec.layout` (scenes). `spec` is treated
   as opaque JSON for CRUD; only the wide-mode PANELS codec branches on
   `apiVersion`.

5. **List-payload completeness — resolved.** LIST returns full Dashboard
   objects per item — `title` and `tags` inside `spec`; folder as
   annotation `grafana.app/folder`. Panel count walks `spec.panels` (v1)
   or `spec.elements` (v2). No per-item fan-out required; `list` is a
   single HTTP round-trip per page. Important consequence: `--folder`
   filter cannot use labelSelector (annotation, not label) — ADR
   removes `--folder` from `list` and routes folder filtering to
   `search --folder UID` instead.

### Live-stack validation (completed 2026-04-21)

The four probes recommended by the research report were run against a
Grafana Cloud dev stack (context `<my-context>`, namespace
`stacks-<stack-id>`). All four passed at the level of the research's
core claims; two surfaced empirical deltas that have been folded into
the Decision section above.

| Probe | Command | Result |
|-------|---------|--------|
| 1 | `gcx api /apis/dashboard.grafana.app/ --context=<my-context>` | **PASS** — `preferredVersion: v2`; all six versions registered (`v0alpha1`, `v1`, `v1beta1`, `v2alpha1`, `v2beta1`, `v2`). |
| 2 | `gcx api /apis/dashboard.grafana.app/v2/namespaces/stacks-<stack-id>/dashboards/<uid> --context=<my-context>` | **PASS** — full Dashboard envelope returned from the bare GET path (no `/dto` subresource required). |
| 3 | `gcx api "/apis/dashboard.grafana.app/v0alpha1/namespaces/stacks-<stack-id>/search?query=dashboard&limit=20" --context=<my-context>` | **PASS with deltas** — (a) raw server response has no top-level `kind`/`apiVersion`; it is a flat `{hits, maxScore, queryCost, totalHits}` object (gcx wraps it in `DashboardSearchResultList` per Contract 2); (b) `type=dash-db` is silently ignored — dashboards **and** folders come back regardless; the modern value `type=dashboard` IS honored by the server (folders excluded); gcx therefore sends `type=dashboard` with no client-side filter; (c) `resource` field values are plural (`"dashboards"` / `"folders"`), not singular; (d) hit shape `{resource, name, title, folder, tags?}` with `tags` omitted when empty. |
| 4 | `gcx api "/apis/dashboard.grafana.app/v2/namespaces/stacks-<stack-id>/dashboards?labelSelector=grafana.app/get-history=true&fieldSelector=metadata.name=<uid>&limit=10" --context=<my-context>` | **PASS with delta** — 9 historical revisions returned for the sample dashboard (gen 1-9); `metadata.generation` encodes the version id as expected; but `metadata.creationTimestamp` is **identical** across all revisions (inherited from original create). Per-revision timestamp lives in the `grafana.app/updatedTimestamp` annotation — TIMESTAMP column source updated accordingly. `grafana.app/message` was empty on every revision of the sample (annotation was not written by the writer that created it). |

Deltas folded back into the Decision section:
- Search command: `type=dash-db` ignored server-side; `type=dashboard` (modern form) IS honored — no client-side filter needed; raw response shape clarified.
- Version history: TIMESTAMP column sources `grafana.app/updatedTimestamp` annotation, not `metadata.creationTimestamp`.

None of the deltas invalidate the ADR's accepted decisions; all are
implementation-detail refinements. Overall confidence now ~96%.

## Open Questions

1. **`schemaVersion` or `version` as a column?** Contract 1 reserves the
   STATUS column for resources that have runtime state. Dashboards don't,
   but they do have a `schemaVersion` (dashboard JSON schema revision) and
   a `resourceVersion` (K8s optimistic-concurrency token). Should wide
   output surface either of these as a column in place of STATUS? Leaning
   no — `schemaVersion` is an implementation detail users rarely need, and
   `resourceVersion` is a transient etag. This ADR leaves them out; easy
   to add later if user feedback asks.

2. **`rollback` vs `versions restore`** — *resolved.* The ADR adopts
   nested noun/verb `versions restore <name> <version>` over the flat
   `rollback <name> --to <version>` that was the pre-research leaning.
   Reasons: groups semantically with `versions list` under the
   `versions` noun; mirrors the sigil precedent (`sigil templates
   versions …`); two positionals read naturally as "restore dashboard X
   version 3" and are terser than positional + `--to` for the common
   case. The two-positional shape is documented as an extension of
   Contract 7 (see Verb contract application).
