# Design: Datasource Management in `gcx`

- **Created**: 2026-06-26
- **Status**: proposed
- **Area**: `gcx datasources`

## Part I — Product & Requirements

### 1. Overview

`gcx datasources` can read and query datasources but cannot manage their full lifecycle. This document proposes completing the lifecycle surface — create, update, delete, health, and schema introspection — under a single, consistent, **declarative** command group that is equally ergonomic for human operators and for AI agents.

The central design thesis: the right primitive is a **declarative document** (a Kubernetes-style manifest) supplied via file, stdin, or round-tripped from a live datasource. Ergonomics come from _scaffolding_ (schema introspection, round-trippable `get`) and _preflight_ (`--dry-run`), not from imperative flags.

Datasources are bridged into the **generic resource framework** so they work with `gcx resources get/pull/push/delete datasources` (a custom resource adapter over the shared client, registered under the canonical `datasource.grafana.app/v0alpha1`, kind `DataSource`, while rendering per-plugin apiVersions) — see §9.8. The dedicated `gcx datasources` commands are a thin wrapper over the same shared core (§9.5). Because the datasource app-platform API is feature-gated and not served by default on current Grafana, the transport today is the legacy `/api/datasources` REST API, behind a small seam so the Kubernetes transport can be added later without changing the surface (§9.6). Per-plugin configuration schemas are likewise deferred behind a seam (§9.7).

---

### 2. Goals & Non-Goals

#### 2.1 Goals

- **G1 — Complete CRUD.** Provide `create`, `get`, `list`, `update`, `delete`
  with consistent grammar and behavior.
- **G2 — Declarative, file-based writes.** `create`/`update` read manifests from
  a file or stdin. Configuration is data, not flags.
- **G3 — Dual-purpose ergonomics.** Every operation is callable as a single,
  non-interactive command by an agent, and as a readable, safe command by a
  human.
- **G4 — Safe secret handling.** Secret values never appear on the command line.
  Agents can apply datasources without holding plaintext secrets in their
  context.
- **G5 — Round-trip.** `get` output can be edited and re-applied without
  reshaping (the fetch → edit → apply loop).
- **G6 — Discoverability.** Users and agents can inspect a plugin's
  configuration schema before authoring a manifest.
- **G7 — Safe mutation.** Destructive operations confirm; reversible operations
  preview.

#### 2.2 Non-Goals

- **NG1** — Imperative create/update via positional/flag-driven field setting.
  (A flag-based path is raised for reconsideration — see §14.)
- **NG2** — Per-field scalar overrides layered on a manifest (`--set key=value`).
- **NG3** — Query-schema introspection and offline manifest validation
  (see §13, Future Work).
- **NG4** — Re-nesting or renaming the existing query commands.
- **NG5** — Bulk datasources-as-code via the generic `gcx resources push -p ./dir`
  pipeline is out of scope here. The dedicated `gcx datasources` commands are the
  surface; a `gcx resources` integration over the same core is possible later
  (§14).

---

### 3. User Stories

#### 3.1 Human operator

- **As an operator**, I author a datasource manifest, apply it with
  `create -f`, and confirm it works with `health`.
- **As an operator**, I clone an existing datasource with
  `get -o yaml > ds.yaml`, tweak the URL, and `update -f ds.yaml`.
- **As an operator**, I remove a stale datasource with `delete`, and I'm asked
  to confirm before it's gone.
- **As an operator**, I keep datasource manifests in git and secrets in a
  separate `--secrets-file`.

#### 3.2 AI agent

- **As an agent**, I emit a compact JSON manifest and pipe it to `create -f -`
  in a single command, with no temporary files left behind.
- **As an agent**, I reference a secret by env-var name (`fromEnv`) so the
  plaintext secret never enters my context or logs.
- **As an agent**, I call `schemas get` to learn required and secure fields
  before authoring a manifest.
- **As an agent**, I `update … --dry-run` to preview a change before applying it.
- **As an agent**, I `delete … --yes` (or rely on agent-mode auto-approval) to
  clean up non-interactively.

---

### 4. Command Surface

```sh
gcx datasources
├── list                       List datasources
├── get        UID             Show a datasource (human detail or apply-ready manifest)
├── create     -f FILE         Create from a manifest (file or stdin)
├── update     UID -f FILE     Update from a manifest (full replace; RMW)
├── delete     UID...          Delete one or more datasources
├── health     [UID]           Health-check one, all, or a type
├── schemas
│   └── get    --type PLUGIN   Show a plugin's configuration schema
├── query                      Generic query (unchanged)
└── <type> query …             Per-type query subcommands (unchanged)
```

CRUD verbs and `health` are flat under the area; the datasource UID is the
positional subject. `schemas` is a subgroup because it operates on plugin
**types**, not instances.

---

### 5. Commands

#### 5.1 `list`

List datasources. Optional `--type` filter and `--limit`. Default human output
is a table; `-o json/yaml` for machines.

```bash
gcx datasources list
gcx datasources list --type prometheus -o json
```

#### 5.2 `get UID`

Show a datasource. Default human detail; `-o yaml/json` emits the apply-ready
manifest (§9.3).

```bash
gcx datasources get sentry-dev
gcx datasources get sentry-dev -o yaml
```

#### 5.3 `create -f FILE`

Create a datasource from a manifest (file or stdin). The server assigns the UID
when `metadata.name` is omitted. `--dry-run` renders the object that would be
created without writing it. No confirmation prompt.

```bash
gcx datasources create -f sentry.yaml
cat sentry.yaml | gcx datasources create -f -
gcx datasources create -f sentry.yaml --dry-run
```

#### 5.4 `update UID -f FILE`

Update an existing datasource from a manifest. This is a **full replace**: fields
omitted from the manifest are reset (stated in the command's help). The current
`resourceVersion` is fetched and applied automatically (optimistic concurrency).

`update` is a reversible apply, so it does **not** prompt. `--dry-run` previews
the resulting object plus a secret-redacted change summary (fields added /
changed / cleared), reusing the object already fetched for the read-modify-write.

```bash
gcx datasources update sentry-dev -f sentry.yaml
gcx datasources update sentry-dev -f sentry.yaml --dry-run
```

#### 5.5 `delete UID...`

Delete one or more datasources by UID. Deletion **prompts for confirmation**
unless `--force`/`--yes`, `GCX_AUTO_APPROVE`, or agent mode is in effect.
`--dry-run` reports what would be deleted. Batch deletes continue on per-UID
errors and report a summary; any failure (including a not-found UID) yields a
partial-failure exit code.

```bash
gcx datasources delete sentry-dev
gcx datasources delete sentry-dev sentry-staging --yes
gcx datasources delete sentry-dev --dry-run
```

#### 5.6 `health [UID]`

Health-check a single datasource, all datasources, or all of a `--type`. The exit
code distinguishes a healthy report from an unhealthy one, and both from a
command that could not run at all — see §11.1.

```bash
gcx datasources health sentry-dev
gcx datasources health
gcx datasources health --type prometheus -o json
```

#### 5.7 `schemas list` and `schemas get --type PLUGIN`

`schemas list` enumerates the datasource **plugin types installed on the
instance** (via Grafana's `/api/plugins?type=datasource`). The `TYPE` column is
the plugin id — the value to pass to `schemas get --type` and to use as
`spec.type` in a manifest. This makes the otherwise-required `--type` discoverable;
the `schemas get` "--type is required" error points here.

`schemas get --type PLUGIN` shows the manifest **configuration schema** for a
plugin. Today this is a generic envelope schema (the common config fields);
per-plugin `jsonData`/`secureJsonData` field schemas are deferred behind the
schema seam (§9.7) until that source lands. `--kind` defaults to `config`;
`--kind query` returns an explicit "not yet supported" error.

```bash
gcx datasources schemas list                                   # discover plugin types
gcx datasources schemas get --type grafana-sentry-datasource
gcx datasources schemas get --type grafana-sentry-datasource -o json
```

---

### 6. Scenarios

#### 6.1 Human: author → apply → verify

```bash
gcx datasources schemas get --type grafana-sentry-datasource   # discover fields
$EDITOR sentry.yaml                                            # author manifest
gcx datasources create -f sentry.yaml                          # apply
gcx datasources health sentry-dev                              # verify
```

#### 6.2 Agent: single-shot create with env-sourced secret

```bash
SENTRY_TOKEN=… gcx datasources create -f - <<'JSON'
{"apiVersion":"grafana-sentry-datasource.datasource.grafana.app/v0alpha1",
 "kind":"DataSource","metadata":{"name":"sentry-dev"},
 "spec":{"type":"grafana-sentry-datasource","title":"Sentry","access":"proxy","url":"https://sentry.example.io/"},
 "secure":{"authToken":{"fromEnv":"SENTRY_TOKEN"}}}
JSON
```

#### 6.3 Clone-and-modify (round-trip)

```bash
gcx datasources get sentry-dev -o yaml \
  | sed 's/sentry-dev/sentry-staging/' \
  | gcx datasources create -f -
```

#### 6.4 GitOps: committed manifest, separate secrets

```bash
gcx datasources update sentry-dev -f manifests/sentry.yaml \
  --secrets-file /run/secrets/sentry.yaml --dry-run   # preview
gcx datasources update sentry-dev -f manifests/sentry.yaml \
  --secrets-file /run/secrets/sentry.yaml             # apply
```

#### 6.5 Batch cleanup

```bash
gcx datasources list --type grafana-sentry-datasource -o json \
  | jq -r '.datasources[].uid' \
  | xargs gcx datasources delete --yes
```

---

### 7. Acceptance Criteria

```md
# create from stdin

GIVEN a valid JSON manifest on stdin
WHEN I run `gcx datasources create -f -`
THEN the datasource is created
AND the command exits 0

# create — secret from env, never printed

GIVEN secure.authToken.fromEnv=SENTRY_TOKEN and SENTRY_TOKEN is set
WHEN I run `gcx datasources create -f manifest.yaml`
THEN the datasource is created with the resolved secret
AND the secret value never appears in any output

# create — missing secret source

GIVEN secure.authToken.fromEnv=SENTRY_TOKEN and SENTRY_TOKEN is unset
WHEN I run `gcx datasources create -f manifest.yaml`
THEN the command fails with a clear error
AND no datasource is created

# get — round-trip

GIVEN an existing datasource "sentry-dev"
WHEN I run `gcx datasources get sentry-dev -o yaml`
THEN the output is an apply-ready manifest (server fields stripped, secure as placeholders)
AND feeding it to `update sentry-dev -f -` succeeds

# update — no prompt, dry-run preview

GIVEN an existing datasource and a TTY
WHEN I run `gcx datasources update sentry-dev -f ds.yaml`
THEN the update applies with no interactive prompt
AND `--dry-run` instead shows the resulting object and a secret-redacted change summary without mutating

# delete — confirmation

GIVEN a TTY and a datasource "sentry-dev"
WHEN I run `gcx datasources delete sentry-dev` and answer no
THEN nothing is deleted and the command exits 0

# delete — batch partial failure

GIVEN "a" (exists) and "missing" (does not)
WHEN I run `gcx datasources delete a missing --yes`
THEN "a" is deleted, "missing" is reported as not found
AND the command exits 4

# delete — agent mode auto-approves

GIVEN agent mode is active
WHEN I run `gcx datasources delete sentry-dev`
THEN no prompt is shown and the datasource is deleted

# health — resource failure vs command failure

GIVEN one healthy and one unhealthy datasource
WHEN I run `gcx datasources health`
THEN both rows are reported AND the command exits 4 (resource failure)
GIVEN credentials are invalid
WHEN I run `gcx datasources health`
THEN no report is produced AND the command exits 3 (command failure)

# schemas — config and query

WHEN I run `gcx datasources schemas get --type X`
THEN a generic envelope schema (apiVersion/kind/metadata/spec) is returned, exit 0
WHEN I run `gcx datasources schemas get --type X --kind query`
THEN a clear "query schema not yet supported" error is returned, exit 2
```

---

## Part II — Technical Design

### 8. Design Principles

| Principle                   | Statement                                                                                                                                                                          |
| --------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Declarative**             | Writes describe desired state in a manifest; the CLI reconciles it.                                                                                                                |
| **Single-shot**             | Any operation is one command — no required multi-step state.                                                                                                                       |
| **Secrets off the wire**    | Secret values are supplied via the manifest `secure` block or resolved from the environment/files; never via flags or process args.                                                |
| **Symmetry**                | What `get` emits, `create`/`update` can consume.                                                                                                                                   |
| **Proportional safety**     | Prompt before destructive ops; preview (not prompt) for reversible ops.                                                                                                            |
| **Consistent grammar**      | Verbs and flags match the conventions used elsewhere in `gcx`.                                                                                                                     |
| **One core, thin surfaces** | A single datasource client + domain type + mapping; commands are thin wrappers. Transport and schema source sit behind seams so they can be swapped without touching the commands. |

---

### 9. The Declarative Model

#### 9.1 Manifest shape

A datasource manifest is a Kubernetes-style envelope whose apiVersion uses the
per-plugin group `{pluginID}.datasource.grafana.app/v0alpha1`:

```yaml
apiVersion: grafana-sentry-datasource.datasource.grafana.app/v0alpha1
kind: DataSource
metadata:
  name: sentry-dev # the datasource UID (optional on create)
spec:
  type: grafana-sentry-datasource # plugin id; selects the API group
  title: Sentry (dev)
  access: proxy
  url: https://sentry.example.io/
  jsonData: # arbitrary, plugin-specific configuration
    orgSlug: my-org
secure: # top-level sibling of spec
  authToken:
    create: <secret-value> # write-only; never returned on read
```

- `spec.type` is the plugin id; it selects the API group and is not persisted as
  a server-side spec field.
- The `secure` block is a **top-level sibling of `spec`**. On read, secret
  entries return only a reference name (`{ name: lds-sv-… }`), never the value.

#### 9.2 Input methods

All three feed the same parser (YAML or JSON; YAML is a JSON superset):

| Method     | Form                                 | Primary consumer                   |
| ---------- | ------------------------------------ | ---------------------------------- |
| File       | `-f sentry.yaml`                     | Humans, GitOps                     |
| Stdin      | `-f -` (pipe/heredoc)                | Agents (single-shot, no temp file) |
| Round-trip | `get UID -o yaml \| update UID -f -` | Both (clone/modify)                |

#### 9.3 Round-trippable `get`

`get` serves two audiences through output format:

- `-o text` / `-o wide` (default for humans): a readable detail view.
- `-o yaml` / `-o json`: an **apply-ready manifest** — server-managed fields
  (`resourceVersion`, `namespace`, managed metadata) stripped, `secure` rendered
  as `{ name: … }` placeholders, `spec.type` retained so the document is
  self-contained and re-appliable.

This makes the kubectl-style loop work for everyone:

```bash
gcx datasources get sentry-dev -o yaml > ds.yaml
$EDITOR ds.yaml
gcx datasources update sentry-dev -f ds.yaml
```

#### 9.4 Secret handling

Secrets are supplied three ways, resolved by `gcx` at apply time. **Exactly one**
source may be set per secure key; setting more than one is an error.

| Source    | Manifest form                                   | When to use                                                            |
| --------- | ----------------------------------------------- | ---------------------------------------------------------------------- |
| Inline    | `secure.authToken.create: <value>`              | Quick, local, trusted shells                                           |
| From env  | `secure.authToken.fromEnv: SENTRY_TOKEN`        | Agents/CI — the manifest holds the **variable name**, never the secret |
| From file | `secure.authToken.fromFile: /run/secrets/token` | Mounted secrets                                                        |

Plus a command-level option to keep the committed manifest secret-free:

```bash
gcx datasources update sentry-dev -f ds.yaml --secrets-file secrets.yaml
```

`--secrets-file` merges secret values into the `secure` block at apply time, so
`ds.yaml` can live in version control while `secrets.yaml` comes from a secret
store.

Rules:

- A referenced env var or file that is missing/empty is a **hard error** — the
  command never silently sends an empty secret.
- Resolved secret values are **never printed** (not in `--dry-run`, diffs, logs,
  or output) — only presence (`set` / `changed` / `cleared`) is reported.
- To remove a stored secret: `secure.authToken.remove: true`.

#### 9.5 Architecture: one core, thin surfaces, swappable seams

The implementation is a single shared core with thin command wrappers and two
seams isolating the parts expected to change.

```text
cmd/gcx/datasources/   create · update · delete · get · health · schemas   (thin)
        │  manifest I/O, secret resolution, --dry-run diff, exit codes, output codecs
internal/datasources/  (the core)
  Datasource           domain/wire type (read + write) with ResourceIdentity
  DataSourceManifest   user-facing envelope (apiVersion/kind/metadata/spec/secure) ⇄ Datasource
  secrets              secure-block resolution → secureJsonData; redaction; warn heuristic
  Transport (seam)     Create/Update/Delete/GetByUID/List/Health
        └─ Client      legacy /api/datasources REST   (today's only transport)
  SchemaProvider (seam) generic envelope schema today; per-plugin source later
```

- **Single domain type.** `Datasource` is used for both reads and writes; the
  command layer maps the manifest envelope to/from it (`spec.title` ↔ `name`,
  `metadata.name` ↔ `uid`, `secure` ↔ `secureJsonData`/`secureJsonFields`).
- **Per-plugin apiVersion.** Manifests use `{pluginID}.datasource.grafana.app/v0alpha1`;
  `spec.type` is optional and derived from the group when omitted. The REST
  transport ignores the group, but the manifest stays forward-aligned with the
  per-plugin app-platform API.
- **Secrets** are a top-level extension on the manifest, resolved to
  `secureJsonData` before send and surfaced as `{name}` placeholders on read.

#### 9.6 Transport seam (legacy REST today)

The datasource app-platform API is **feature-gated and not served by default** on
current Grafana, so the legacy `/api/datasources` REST API is the transport
today. A small `Transport` interface isolates it so a Kubernetes app-platform
implementation can be added later without touching the command layer:

```go
// Transport is the datasource lifecycle interface used by the commands.
type Transport interface {
    List(ctx) ([]*Datasource, error)
    GetByUID(ctx, uid) (*Datasource, error)
    Create(ctx, *Datasource) (*Datasource, error)
    Update(ctx, uid, *Datasource) (*Datasource, error)
    Delete(ctx, uid) error
    Health(ctx, uid) (*HealthResult, error)
}

// NewTransport returns the transport for the given config (REST today).
func NewTransport(cfg) (Transport, error)
```

When the per-plugin app-platform groups are served, a second `Transport`
implementation plugs in behind `NewTransport` — `--dry-run`, secret resolution,
the redacted diff, round-trip `get`, and the `health` exit codes are all in the
command/core layer and carry over unchanged.

#### 9.7 Schema seam (generic envelope today)

`schemas get` returns a **generic envelope schema** generated from the manifest
spec type. It describes the common config fields but treats plugin-specific
`jsonData`/`secureJsonData` as opaque — a real per-plugin schema requires a
source that does not exist on a stock Grafana yet.

The lookup sits behind a seam (`ConfigSchema(pluginType)`) so that when
per-plugin schemas become available (served by Grafana APIs/CDN), the command
surface stays the same and only the source changes. `schemas validate` and
`--kind query` are deferred to that point.

**Type discovery and validation.** `schemas list`, backed by
`Client.ListPluginTypes` (Grafana's `/api/plugins?type=datasource`), lists the
installed datasource plugin types so users can discover valid `--type` /
`spec.type` values. `schemas get --type X` validates `X` against the same listing
and **fails** for an unknown plugin id (rather than silently emitting a generic
schema for a non-existent type); both the missing-`--type` and unknown-type
errors link to `schemas list`. This makes `schemas get` require a live
connection. (D22)

#### 9.8 Resource-framework integration

Datasources are registered into gcx's generic resource pipeline so they are
managed exactly like dashboards, SLOs, and the other typed providers — via
`gcx resources get/pull/push/delete datasources`. A provider
(`internal/providers/datasources`) contributes a custom resource adapter over the
shared REST `Client`:

- **Descriptor** `datasource.grafana.app/v0alpha1`, kind `DataSource` — a single,
  statically-registered group the resources discovery registry routes on. The
  rendered manifests carry a **per-plugin** apiVersion
  (`{pluginID}.datasource.grafana.app/v0alpha1`); routing collapses it back (see
  normalization below). The legacy REST transport ignores the group; `spec.type`
  carries the plugin id.
- **Converged manifest shape (matches the canonical app-platform `DataSource`
  and the dedicated `gcx datasources` output).** The adapter does **not** use
  `adapter.TypedCRUD` — that envelope is spec-only and cannot express the
  canonical top-level `secure` block. Instead it maps to/from the shared
  `DataSourceManifest`:
  - `secure` is a **top-level sibling of spec** — a map of `InlineSecureValue`
    (`{create, name, remove, description}`, plus gcx-side `fromEnv` / `fromFile`
    indirection resolved into `create` before send). This mirrors
    `DataSource.secure` in Grafana's OpenAPI snapshot, not `spec.secureJsonData`.
  - `spec` is keyed by **`title`** (the canonical `DataSourceSpec` display-name
    field), not `name`.
  - The natural key for cross-stack push is `spec.title`.
- **`secure` vs `secureValues`.** These are different layers and must not be
  conflated. `secure` (above) lives on the **instance** and holds actual secret
  values/references. `secureValues` is a **schema-level** field in the plugin
  SDK's `pluginschema.Settings` (`[]SecureValueInfo{key, description, required}`)
  that declares *which* secret keys exist and whether they are required; it
  belongs in `schemas get` output (the SchemaProvider seam, §9.7), never in an
  instance manifest.
- A missing-secret warning fires on push (D17); 404 maps to a Kubernetes
  NotFound so push upserts fall through to create.
- **Schema/Example** render the converged envelope: a `spec` reflected from
  `DataSourceSpec` plus a top-level `secure` block, with `apiVersion` accepting
  both per-plugin and canonical groups via pattern.

The provider adds **no commands of its own** — the human-facing `gcx datasources`
tree is mounted separately and reuses the same client, mirroring how
`gcx dashboards` and `gcx resources … dashboards` coexist over one API.

**Per-plugin apiVersion normalization.** Grafana's app-platform serves a
*per-plugin* API group — `{pluginID}.datasource.grafana.app/v0alpha1` — rather
than the single base group. Manifests and both command paths therefore carry the
per-plugin apiVersion, but the resource pipeline registers exactly one descriptor
(`datasource.grafana.app/v0alpha1`) backed by the type-agnostic legacy REST API.
To bridge the two, the provider registers a generic **GVK normalizer**
(`resources.RegisterGVKNormalizer`) that collapses any
`*.datasource.grafana.app/v0alpha1` `DataSource` onto the canonical descriptor.
The normalizer is applied at the **routing boundary** — the `supported[gvk]`
descriptor lookup in the `Pusher` and `Deleter` — so it covers every code path,
including objects **fetched** from the server (delete-by-selector and
push-by-selector fetch first, so they never pass through the manifest reader).
Objects keep their per-plugin apiVersion end-to-end; only the routing lookup is
normalized. Because the datasource type is read from `spec.type`, the canonical
group carries no information that is lost. The normalizer registry is generic:
any provider whose served groups differ from its registered descriptor can opt
in the same way.

---

### 10. Design Decisions

| ID  | Decision                                                                    | Rationale                                                                                                                                                                              |
| --- | --------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | Declarative, file/stdin manifests for writes                                | Configuration is structured data; keeps secrets off argv; supports GitOps and arbitrary nested config.                                                                                 |
| D2  | CRUD verbs flat; `schemas` is the only subgroup                             | The UID is the positional subject; schema introspection is the one operation scoped to plugin _types_, not instances.                                                                  |
| D3  | `health` stays a flat leaf verb                                             | It is a cross-cutting action over instances, not a noun.                                                                                                                               |
| D4  | `delete` prompts; `create`/`update` do not                                  | Delete is destructive/irreversible; create/update are reversible applies. Proportional safety.                                                                                         |
| D5  | `--dry-run` on create/update/delete                                         | Preview mutations without writing; the update preview reuses the object already fetched for RMW.                                                                                       |
| D6  | `get -o yaml/json` = apply-ready manifest                                   | Enables the fetch → edit → apply loop for humans and agents; rich detail remains under `-o text`.                                                                                      |
| D7  | Secret indirection (`fromEnv` / `fromFile` / `--secrets-file`)              | Lets agents and GitOps apply datasources without embedding plaintext secrets in manifests or model context.                                                                            |
| D8  | Exactly one secret source per key; missing source errors                    | Removes ambiguity; prevents silently writing empty secrets.                                                                                                                            |
| D9  | Resolved secrets never printed                                              | Diffs/dry-run/logs report presence only.                                                                                                                                               |
| D10 | `delete UID...` deletes by UID; failures classified per-UID                 | The REST transport addresses datasources by UID; a missing/failed UID is isolated so a batch still reports a partial-failure summary.                                                  |
| D11 | No imperative flags, no `--set` overlays (default)                          | Preserves the declarative model and avoids partial-imperative argv configuration. A flag-based path is reconsidered in §14.                                                            |
| D12 | `schemas` ships config-kind first; query/validate later                     | Config schema is the achievable, high-value slice; query schema is deferred.                                                                                                           |
| D13 | One shared core (single `Datasource` type) behind transport + schema seams  | Commands are thin wrappers; the transport (REST today, K8s later) and schema source swap without touching the command layer. (§9.5)                                                    |
| D14 | Model `secure` as a top-level manifest extension resolved before send       | The manifest envelope is `{apiVersion,kind,metadata,spec}`; datasources additionally need write-only secrets + read-back references, which map to `secureJsonData`/`secureJsonFields`. |
| D15 | `health` returns exit 4 for unhealthy resources; 1/2/3 for command failures | Lets scripts and agents distinguish "a datasource is down" from "the check could not run". (§11.1)                                                                                     |
| D16 | Transport behind a seam; legacy REST today, K8s app-platform later          | The datasource app-platform API is feature-gated and absent by default; the legacy `/api/datasources` REST API works everywhere now and the K8s transport is a drop-in. (§9.6)         |
| D17 | Warn (never block) when basic auth is enabled but no secret is supplied     | Catches the common pull→push that would silently drop a password; a narrow, type-agnostic signal rather than a maintained allowlist.                                                   |
| D18 | `schemas get` returns a generic envelope schema today                       | No per-plugin config-schema source exists on a stock Grafana yet; the seam swaps in a real source later without changing the command. (§9.7)                                           |
| D19 | Register datasources in the generic resource framework (single GVK adapter) | `gcx resources $VERB datasources` works like every other typed provider; a single, statically-registered `datasource.grafana.app/v0alpha1` is required for discovery routing. (§9.8)   |
| D20 | Normalize per-plugin `*.datasource.grafana.app` groups onto the canonical GVK | Grafana serves per-plugin groups (`{pluginID}.datasource.grafana.app`) but the REST adapter is type-agnostic and registers one descriptor; a generic, provider-registered GVK normalizer (applied at the `Pusher`/`Deleter` routing lookup, so it also covers fetched objects) routes per-plugin manifests without a hard-coded type table. (§9.8) |
| D21 | Converge the resource-framework manifest onto the canonical app-platform shape (top-level `secure`, `spec.title`) via a custom adapter | The canonical `DataSource` carries a top-level `secure` block (`InlineSecureValue`) and a `title`-keyed spec; `TypedCRUD`'s spec-only envelope cannot express the sibling `secure`. A small custom `ResourceAdapter` reuses the `DataSourceManifest` mapping so `gcx resources … datasources` and `gcx datasources` emit the same shape that Grafana actually serves. (§9.8) |
| D22 | `schemas list` discovers plugin types from `/api/plugins?type=datasource`, and `schemas get` validates `--type` against it | `schemas get --type` is otherwise undiscoverable and silently accepts any string; listing installed datasource plugin types surfaces valid `--type`/`spec.type` values, an unknown type now fails, and both errors link to `schemas list`. (§9.7)                                                                                          |

---

### 11. Output & Exit Codes

- **Stdout** carries the result (resource data or operation summary); **stderr**
  carries diagnostics and progress.
- All output flows through the codec system. Default human formats: table for
  `list`/`health`, manifest YAML for `get -o yaml`, status messages for
  mutations. Agent mode defaults to compact JSON.

| Code | Meaning                                                                            |
| ---- | ---------------------------------------------------------------------------------- |
| 0    | Success                                                                            |
| 1    | General / operational failure (no connectivity, nothing matched, unexpected error) |
| 2    | Usage error (bad flags/args, unknown `--kind`)                                     |
| 3    | Auth failure (401/403)                                                             |
| 4    | Partial failure (some UIDs in a batch failed; some datasources unhealthy)          |

#### 11.1 `health`: command failure vs resource failure

`health` deliberately separates _the command working_ from _the resources being
healthy_. This lets a caller tell "a datasource is down" apart from "I couldn't
check".

- **Resource failure → exit 4.** The command ran and produced a full report, but
  one or more targeted datasources reported unhealthy (or an individual health
  probe returned an error). The per-datasource table/JSON is still written to
  **stdout**, with each unhealthy row marked. Exit 4 means: _"I checked, and
  something is unhealthy."_
- **Command failure → exit 1 / 2 / 3.** The command could not produce a verdict
  at all, and writes an error to **stderr** with no health report:
  - **2** — usage error (bad flags/args).
  - **3** — auth failure (401/403); credentials missing or rejected.
  - **1** — operational failure: cannot reach Grafana, cannot list datasources,
    or no datasource matched the UID / `--type` selector.

**Rule of thumb:** exit **4** = the command succeeded, the resources didn't; any
**other** non-zero = the command itself failed. A single datasource's probe
returning an HTTP error counts as a _resource_ failure (exit 4), because the
overall command still completed across the set. The same semantics apply to the
single (`health UID`), all (`health`), and filtered (`health --type X`) forms.

```bash
# Scripting: distinguish the two
gcx datasources health --type prometheus
case $? in
  0) echo "all healthy" ;;
  4) echo "checked, but some are unhealthy" ;;   # resource failure
  *) echo "could not run the health check" ;;    # command failure (1/2/3)
esac
```

---

### 12. Definition of Done

- All commands in §5 implemented following the standard options pattern
  (`opts` + `setup` + `Validate` + constructor), with agent annotations and
  example blocks.
- One shared core: a single `Datasource` type + REST `Client` behind the
  `Transport` seam; commands are thin wrappers (§9.5).
- Secret resolution (`create` / `fromEnv` / `fromFile` / `--secrets-file`) is
  implemented with single-source enforcement and guaranteed non-disclosure.
- `get -o yaml/json` round-trips into `update -f -`.
- Confirmation, `--dry-run`, and partial-failure semantics behave per §7, and
  `health` exit codes per §11.1.
- Agent mode parity: every command is non-interactive and machine-parseable.
- Unit tests cover each command and the secret/diff/round-trip helpers,
  including secret-redaction, missing-source errors, and the `health`
  command-vs-resource exit-code paths; the suite passes with race detection.
- Lint passes; CLI reference and help text are regenerated and drift-free; docs
  build succeeds.

---

### 13. Future Work

- **Query-schema introspection** (`schemas get --kind query`).
- **Offline manifest validation** (`schemas validate`) against config/query
  schemas.
- **Example/skeleton manifest generation** from a plugin schema.
- **Bulk apply** of multiple datasource manifests from a directory.

---

### 14. Open Questions

- Confirm the precedence / error behavior when both a manifest `secure.*` source
  and a `--secrets-file` entry target the same key (proposed: `--secrets-file`
  wins, with a warning).
- **Kubernetes transport:** add the app-platform `Transport` implementation
  behind `NewTransport` once per-plugin `*.datasource.grafana.app` groups are
  served (and auto-select it).
- **Per-plugin schemas:** wire `ConfigSchema` (and `schemas validate`, `--kind
query`) to the real schema source once it is available via Grafana APIs/CDN.
- **Manifest shape is unified (D21).** Both `gcx resources … datasources` and
  `gcx datasources` now emit the same converged envelope — per-plugin apiVersion,
  a top-level `secure` block (`InlineSecureValue` shape), and a `title`-keyed
  spec — matching the canonical app-platform `DataSource`. The dedicated commands
  retain `--secrets-file` and `--dry-run` as a thin superset; the only rendered
  difference is that the resources path stamps `metadata.namespace` (it is
  namespace-aware). Remaining: fold the dedicated commands' secret indirection
  onto the resources path so `gcx resources push` also resolves `fromEnv` /
  `fromFile` (today the custom adapter resolves the `secure` block but the
  `--secrets-file` flag is dedicated-only).
- **Support an `edit` command?** Should we add a dedicated `gcx datasources edit
UID` (`$EDITOR` round-trip)? The `get -o yaml | update -f -` round-trip already
  covers the workflow. If added, it should reuse the shared `editor` primitive
  but route through the datasource get→update path so the `secure` block is
  handled correctly (the generic `gcx resources edit datasource/<uid>` would
  not). Decide whether the convenience justifies the extra command surface.
- **CLI flag-based args as an additional CRUD path?** Should `create`/`update`
  (and possibly `delete`) also accept imperative flags (e.g.
  `create --type … --name … --url …`, `update UID --url …`) alongside the
  file/manifest input? Pros: fast one-liners for humans and a trivial mapping
  from agent tool parameters. Cons: revisits the declarative stance
  (NG1, NG2, D1, D11); cannot express arbitrary nested `jsonData`; and secrets
  must still stay off argv (values via the `secure` block / `fromEnv`, never
  flags). If pursued, build it as a thin convenience over the same manifest
  builder, not a parallel code path.

---

## Part III — Implementation Plan

### 15. List of Tasks

- [x] Define the `DataSourceManifest` envelope + `secure` types and the single
      `Datasource` domain type (with `ResourceIdentity`).
- [x] Implement the REST `Client` (`create`/`get`/`update`/`delete`/`health`)
      behind the `Transport` seam.
- [x] Implement manifest I/O (file/stdin, YAML/JSON) and the manifest ⇄
      `Datasource` mapping (spec.type optional, derived from apiVersion).
- [x] Implement secret resolution (`create` / `fromEnv` / `fromFile` /
      `--secrets-file`) with single-source enforcement and non-disclosure.
- [x] Implement the secret-redacted change summary (diff) used by `--dry-run`.
- [x] Implement the warn-on-missing-secret heuristic (basic auth without a secret).
- [x] `create -f` and `update UID -f` commands (with `--dry-run`).
- [x] `delete UID...` command (confirmation, `--force`/`--yes`, `--dry-run`,
      batch, partial-failure exit 4).
- [x] Round-trippable `get` (`-o yaml/json` emits the apply-ready manifest; rich
      detail under `-o text`).
- [x] `health [UID]` command with command-vs-resource exit-code semantics (§11.1).
- [x] `schemas get --type PLUGIN` (generic envelope schema; `--kind query`
      returns not-supported).
- [x] `schemas list` (installed datasource plugin types; powers `--type` discovery).
- [x] Register all commands under the `gcx datasources` group.
- [x] Unit tests for every command and the mapping/secret/diff helpers
      (redaction, missing-source, health exit codes).
- [x] Update CLI reference, help text, and the `gcx` skill/docs.

### 16. How to test it locally

Two layers: **automated** checks (no live Grafana needed) and a **manual
end-to-end** matrix against a real Grafana.

#### 16.1 Automated checks (no server required)

```bash
# Build the binary to bin/gcx
go build -buildvcs=false -o bin/gcx ./cmd/gcx/

# Unit + httptest command tests (fake Grafana, in-memory store)
go test ./internal/datasources/... ./cmd/gcx/datasources/...

# Lint the touched packages
golangci-lint run ./cmd/gcx/datasources/... ./internal/datasources/...

# Full suite
go test ./...
```

The command tests in `cmd/gcx/datasources/crud_test.go` spin up an `httptest`
server that serves the legacy `/api/datasources` API, exercising the commands
end-to-end without a live Grafana.

#### 16.2 Spin up a local Grafana (Docker) with the Infinity plugin

Use the Grafana Enterprise image with the Infinity plugin preinstalled. The
TestData datasource (`grafana-testdata-datasource`) is built in, so no install is
needed for it.

```bash
# Start Grafana Enterprise with the Infinity plugin preinstalled.
docker run -d --name gcx-grafana -p 3000:3000 \
  -e "GF_INSTALL_PLUGINS=yesoreyeram-infinity-datasource" \
  grafana/grafana-enterprise:latest

# Wait until the HTTP API is ready.
until curl -sf http://localhost:3000/api/health >/dev/null; do sleep 2; done

# Confirm the Infinity plugin is installed (default admin creds: admin/admin).
curl -s -u admin:admin "http://localhost:3000/api/plugins?type=datasource" \
  | grep -o 'yesoreyeram-infinity-datasource'
```

> `GF_INSTALL_PLUGINS` downloads the plugin at startup, so the container needs
> network access. Pin a version with `yesoreyeram-infinity-datasource 3.4.0` if
> you need reproducibility.

**Create a service-account token** (Admin role) for gcx. Via the API using the
default admin credentials:

```bash
# 1. Create an Admin service account.
SA_ID=$(curl -s -u admin:admin -H "Content-Type: application/json" \
  -d '{"name":"gcx","role":"Admin"}' \
  http://localhost:3000/api/serviceaccounts \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')

# 2. Mint a token for it.
TOKEN=$(curl -s -u admin:admin -H "Content-Type: application/json" \
  -d '{"name":"gcx-token"}' \
  http://localhost:3000/api/serviceaccounts/$SA_ID/tokens \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["key"])')

echo "$TOKEN"   # glsa_...
```

Or via the UI: **Administration → Users and access → Service accounts → Add
service account** (role **Admin**) → **Add service account token**.

#### 16.3 Point gcx at the instance

The transport is the legacy `/api/datasources` REST API (§9.6), so any Grafana
works. On-prem/OSS needs an org id; Grafana Cloud uses a stack id instead.

```bash
export GRAFANA_SERVER="http://localhost:3000"
export GRAFANA_TOKEN="$TOKEN"        # from §16.2 (needs Editor/Admin for writes)
export GRAFANA_ORG_ID="1"            # on-prem/OSS; omit for Cloud
export GCX_AGENT_MODE=false          # human-readable output

./bin/gcx datasources list           # smoke test (read path)
```

> The service account must have `datasources:create/write/delete` for the
> mutation tests (Editor or Admin). Read/health/schemas work with Viewer.

#### 16.4 Manual end-to-end matrix

Each step notes the **expected result**. Secrets are passed via env vars, never
on the command line. `yesoreyeram-infinity-datasource` exercises rich `jsonData`
plus `secureJsonData` secrets; `grafana-testdata-datasource` is a built-in,
always-healthy datasource with no secrets.

> Grafana custom HTTP headers use a flat convention, not a nested object: the
> header **name** goes in `jsonData.httpHeaderName{N}` and the header **value**
> (a secret) in `secure.httpHeaderValue{N}` (stored under `secureJsonData`).

```bash
# 1. create Infinity — dry-run (no write); secret-redacted diff, never the value
INFINITY_TOKEN=bearer-xyz-123 HDR_VAL=tenant-42 ./bin/gcx datasources create -f - --dry-run <<'JSON'
{"apiVersion":"yesoreyeram-infinity-datasource.datasource.grafana.app/v0alpha1",
 "kind":"DataSource","metadata":{"name":"gcx-infinity"},
 "spec":{"type":"yesoreyeram-infinity-datasource","title":"gcx infinity","access":"proxy","url":"https://api.example.com",
   "jsonData":{"auth_method":"bearerToken","tlsSkipVerify":true,"timeoutInSeconds":30,"httpHeaderName1":"X-Scope-OrgID"}},
 "secure":{"bearerToken":{"fromEnv":"INFINITY_TOKEN"},"httpHeaderValue1":{"fromEnv":"HDR_VAL"}}}
JSON
#   → exit 0; shows `bearerToken: set` and `httpHeaderValue1: set`; values never printed

# 2. create Infinity — real (secrets resolved from env → secureJsonData)
INFINITY_TOKEN=bearer-xyz-123 HDR_VAL=tenant-42 ./bin/gcx datasources create -f - <<'JSON'
{"apiVersion":"yesoreyeram-infinity-datasource.datasource.grafana.app/v0alpha1",
 "kind":"DataSource","metadata":{"name":"gcx-infinity"},
 "spec":{"type":"yesoreyeram-infinity-datasource","title":"gcx infinity","access":"proxy","url":"https://api.example.com",
   "jsonData":{"auth_method":"bearerToken","tlsSkipVerify":true,"timeoutInSeconds":30,"httpHeaderName1":"X-Scope-OrgID"}},
 "secure":{"bearerToken":{"fromEnv":"INFINITY_TOKEN"},"httpHeaderValue1":{"fromEnv":"HDR_VAL"}}}
JSON
#   → "Created datasource ..."; exit 0

# 3. create TestData — built-in, no secret
./bin/gcx datasources create -f - <<'JSON'
{"apiVersion":"grafana-testdata-datasource.datasource.grafana.app/v0alpha1",
 "kind":"DataSource","metadata":{"name":"gcx-testdata"},
 "spec":{"type":"grafana-testdata-datasource","title":"gcx testdata","access":"proxy"}}
JSON
#   → "Created datasource ..."; exit 0

# 4. get — human detail (text) and apply-ready manifest (yaml)
./bin/gcx datasources get gcx-testdata                # → FIELD/VALUE table
./bin/gcx datasources get gcx-infinity -o yaml        # → manifest: jsonData round-trips
#     (incl. httpHeaderName1); secure.bearerToken and secure.httpHeaderValue1
#     rendered as {name: ...} placeholders (no values)

# 5. round-trip — get | edit jsonData | apply (secret left unchanged)
./bin/gcx datasources get gcx-infinity -o yaml \
  | sed 's/timeoutInSeconds: 30/timeoutInSeconds: 90/' \
  | ./bin/gcx datasources update gcx-infinity -f -
#   → "Updated ..."; timeout now 90; bearerToken preserved (no value re-supplied)

# 6. update Infinity — dry-run (rotate secret + change jsonData), nothing applied
NEW=bearer-ROTATED ./bin/gcx datasources update gcx-infinity -f - --dry-run <<'JSON'
{"apiVersion":"x","kind":"DataSource",
 "spec":{"type":"yesoreyeram-infinity-datasource","title":"gcx infinity","access":"proxy","url":"https://api.example.com",
   "jsonData":{"auth_method":"bearerToken","tlsSkipVerify":false,"timeoutInSeconds":60,"httpHeaderName1":"X-Scope-OrgID"}},
 "secure":{"bearerToken":{"fromEnv":"NEW"}}}
JSON
#   → exit 0; shows `tlsSkipVerify: changed`, `bearerToken: changed`; no write

# 7. health — TestData is always healthy
./bin/gcx datasources health gcx-testdata             # → STATUS OK; exit 0

# 8. delete — dry-run, then batch with a missing UID (partial failure)
./bin/gcx datasources delete gcx-infinity --dry-run   # → "would delete"; exit 0
./bin/gcx datasources delete gcx-infinity no-such-ds --yes
#   → table: gcx-infinity deleted, no-such-ds failed; exit 4

# 9. schemas — generic envelope schema (per-plugin schema deferred) and query
./bin/gcx datasources schemas get --type yesoreyeram-infinity-datasource
#   → a generic envelope schema (apiVersion/kind/metadata/spec)
./bin/gcx datasources schemas get --type yesoreyeram-infinity-datasource --kind query
#   → "query schema not yet supported"; exit 2

# 10. agent mode — machine-readable JSON, prompts auto-handled
GCX_AGENT_MODE=true ./bin/gcx datasources list
```

Check exit codes with `echo $?` after each command (mind shell pipelines — the
exit code is the last command in a pipe). Useful assertions:

```bash
# Point a datasource at an unreachable URL to see a resource (not command) failure.
INFINITY_TOKEN=x ./bin/gcx datasources create -f - >/dev/null <<'JSON'
{"apiVersion":"x","kind":"DataSource","metadata":{"name":"gcx-bad"},
 "spec":{"type":"yesoreyeram-infinity-datasource","title":"bad","access":"proxy","url":"http://127.0.0.1:9"},
 "secure":{"bearerToken":{"fromEnv":"INFINITY_TOKEN"}}}
JSON
./bin/gcx datasources health gcx-bad; echo "exit=$?"
#   → exit 4 if unhealthy (resource failure); the report is still printed
./bin/gcx datasources get does-not-exist; echo "exit=$?"
#   → exit 1 (not found / command failure); exit 3 on auth failure
```

#### 16.5 Cleanup

```bash
./bin/gcx datasources delete gcx-infinity gcx-testdata gcx-bad --yes 2>/dev/null || true
./bin/gcx datasources list      # confirm test datasources are gone

docker rm -f gcx-grafana        # tear down the local Grafana container
```
