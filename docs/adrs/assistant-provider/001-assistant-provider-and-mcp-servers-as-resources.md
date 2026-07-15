# ADR-021: Assistant Provider + MCP Servers as Resources

**Created**: 2026-07-07
**Status**: proposed
**Origin**: PR #747 (`gcx assistant mcp-servers` CRUD, command-only)

## Context

`gcx assistant` is mounted by hand and is **not** a registered provider — no
`internal/providers/assistant` package, none of the `Provider` interface.
Consequently its **MCP-server configuration cannot flow through
`gcx resources get/pull/push/delete`** and cannot participate in GitOps.

MCP servers are *configuration* — which remote MCP endpoints a stack trusts,
their scope, and their auth — exactly what VISION's GitOps model is for ("pull to
files, version in git, push back; same manifests across `--context`").
Investigations, the sibling group, are *outputs* (run artifacts). Making MCP
servers manageable as resources closes a real gap, and the Assistant should be a
first-class provider like every other surface ("new products are added as
providers").

**Why this is one design.** An adapter self-registers *only* through a provider's
`TypedRegistrations()` (CONSTITUTION: no `adapter.Register()` outside
`providers.Register()`). So **making `assistant` a provider is the prerequisite**
for MCP-servers-as-resources — the two are inseparable.

### Scope

**In:** register `assistant` as a provider (the `slo`/`irm` shape — `Commands()`
+ `TypedRegistrations()`); one resource adapter for MCP servers; the identity,
secret, and pagination decisions that make the bridge correct.

**Out:** the human `gcx assistant mcp-servers` command surface (verbs stay);
OAuth-flow changes; the A2A `prompt`/`dashboard` path; any backend API change.

### API behaviour (relevant properties)

The MCP-server management API (the one already behind `gcx assistant
mcp-servers`) has a few properties that shape the design. Only the
design-relevant behaviour is captured — these resolved what were open questions:

- **Server names are not unique.** Each server has a server-assigned opaque ID;
  two servers can share a name, even within one scope.
- **Scope (`user` | `tenant`) is a required, immutable attribute** of each server
  and participates in every scope-qualified operation. The same name may exist in
  both scopes.
- **Auth-header values are write-only** — never returned on read. The update path
  supports explicit per-header write intent, so a caller can overwrite a header,
  preserve an existing secret without resending it, or remove a header. This is
  what makes deterministic GitOps round-trips possible (Decision 5).
- **Listing is paginated and returns a total count.**

## Decision summary

| # | Decision | Approach |
|---|---|---|
| 1 | Provider shape | One `assistant` provider; `Commands()` = existing tree; `TypedRegistrations()` = MCPServer only |
| 2 | Non-adapter groups | Investigations + A2A stay exactly as-is (aligned with the unified verb-vocabulary direction) |
| 3 | Identity | `metadata.name = {scope}-{slug(name)}`; scope/url/name in `spec`; server ID as annotation; natural key (scope, name, url) |
| 4 | Adapter | `TypedCRUD[MCPServer]` with headers in `spec`; GVK `assistant.ext.grafana.app/v1alpha1` |
| 5 | Secrets | Map the manifest header list onto the API's per-header write intent (overwrite / preserve / remove); `fromEnv`/`fromFile` for CI |
| 6 | Prerequisites | Client offset-pagination (adapter exhausts; human `list` stays bounded + hint); fix update header handling |

## Decision 1 — One `assistant` provider

Create `internal/providers/assistant` following the `slo`/`irm` template (one
provider that both mounts commands and registers adapters). `Commands()` returns
the existing assistant tree unchanged (`prompt`, `dashboard`, `conversation`,
`investigations`, `mcp-servers`); `TypedRegistrations()` returns exactly one entry
— MCPServer. Register in `init()` via `providers.Register`, blank-import it at
startup, and drop the current hand-mount. `ConfigKeys()` declares the existing
`providers.assistant.*` capability-cache keys (non-secret) so `config view`
doesn't redact them.

The existing `requireGrafanaCloud` guard and per-subcommand loader wiring move
*with* the command tree (they stay on the assistant parent command built inside
`Commands()`, exactly as `slo`/`irm` chain the root pre-run). Lift-and-shift, not
a rewrite.

**Rejected:** adapter-only provider with commands mounted separately (the
datasources shape) — leaves the rich command tree outside the registry and
forfeits first-class provider status. Two providers — fragments config for no gain.

## Decision 2 — Investigations and A2A stay as-is

Making `assistant` a provider brings its other command groups under provider
rules. Both stay exactly as shipped:

- **Investigations** keep `list`/`get`/`create` + run-lifecycle verbs. This is
  aligned with the current direction that **configuration and ephemeral resources
  should share one verb vocabulary and stay as UX-close as possible** — so
  investigations keeping the standard verbs is correct, not an exception.
  (`conversation` likewise.)
- **A2A `prompt`/`dashboard`** keep their OAuth-PKCE streaming client — a distinct
  auth mechanism/transport from the SA-token plugin-REST path, out of scope here.

No verb renames, no config retrofitting.

## Decision 3 — Identity

Because server names are not unique and each server has a server-assigned opaque ID:

- `metadata.name = {scope}-{slug(name)}` — human-readable, unique per scope in the
  common case.
- `spec` materializes `scope`, `url`, and `name`, so every operation is
  self-contained — scope is read from `spec` (never parsed from the name) and
  drives the scope-qualified update/delete calls.
- The server-assigned ID rides as an annotation for within-stack addressing;
  ignored cross-stack (not portable).
- Cross-stack `push` matches on the natural key **(scope, name, url)** — the tuple
  that actually distinguishes servers — so push is idempotent regardless of the
  server-assigned ID.
- Because duplicate `(scope, name)` with different URLs is legal, a name collision
  (on `get`, or on the `pull` filename) is **surfaced as an error listing
  candidates**, never silently resolved.

**Rejected:** scope→namespace (blocked — namespace is context-global and rewritten
on push); server ID as `metadata.name` (not portable); name-only key (the current
ambiguity).

## Decision 4 — `TypedCRUD[MCPServer]`, headers in `spec`

Register via `TypedCRUD[MCPServer]`, not a hand-rolled adapter. datasources
hand-rolls only because the app-platform DataSource forces a write-only block
*outside* `spec`; we author the MCPServer manifest, so headers live *inside*
`spec` and the standard `{metadata, spec}` envelope fits. The `mcp-servers`
mutation verbs (`create`, `update`, `delete`) route their data access through
this one `TypedCRUD[MCPServer]`, the same code path `gcx resources` uses — so
both paths share create-vs-update natural-key resolution and per-header write
intent, satisfying the TypedCRUD invariant (CONSTITUTION §37-41). The read
verbs (`get`, `list`) fetch via the raw client — for id-or-name ref resolution
and bounded pagination, capabilities `TypedCRUD` does not expose — and reuse
the adapter only via `ToUnstructured` to emit the identical
`{apiVersion, kind, metadata, spec}` JSON/YAML envelope as the `resources`
path. This mirrors the repo's own precedent: `irm` incidents/oncall do the
same raw-client-read-with-adapter-shaped-output pattern.

- **GVK:** `assistant.ext.grafana.app/v1alpha1`, Kind `MCPServer`, plural
  `mcpservers` — follows the repo's `<area>.ext.grafana.app` convention and is
  forward-compatible with future assistant resources.
- Domain type is a dedicated manifest struct (distinct from the client's read/write
  types); `Schema` and `Example` both non-nil (writable).
- `validate` and OAuth stay as extension verbs on the command tree, not adapter ops —
  after `create`/`update`, the command drives the OAuth validate/initiate step via
  the raw client because it is not a CRUD data-access operation.
- Because the adapter resolves servers by the `(scope, name, url)` natural key,
  those three fields are the server's immutable identity: `update` overlays only
  non-identity fields and rejects a scope/name/url change with an actionable
  error (delete and recreate to change identity), consistent with scope being
  required + immutable (Decision 3).

## Decision 5 — Secret round-trip via explicit per-header write intent

The manifest carries headers as a write-only list under `spec` (name + optional
value, sourced inline or via `fromEnv`/`fromFile` for CI). The adapter maps the
manifest's header list onto the API's per-header write intent:

- header with a supplied value → **overwrite**;
- header with a name but no value (as `pull` produces, since reads redact) →
  **preserve** the stored secret — *update of an existing server only* (see below);
- header absent from the manifest → **remove**.

**Preserve only works when the secret already exists.** The name-only → preserve
mapping covers the common in-place edit: `push` back to the same stack marks the
header "preserve" and never needs the value — the secret stays server-side. But on
**create** — a brand-new server, including first-time **cross-stack** sync — there
is nothing to preserve; a name-only header cannot be materialised. There the value
*must* be supplied (inline / `fromEnv` / `fromFile`). Since `push` is
create-or-update, the adapter already knows which path it is on (it matches by
natural key first), so it can enforce this: a name-only header on a create path
**errors** with an actionable message ("header `X` has no value — supply it via
`fromEnv`/`fromFile`") rather than silently creating a valueless header.

Deterministic, no merge assumption — the API's explicit per-header write intent is
exactly what GitOps needs, and `fromEnv`/`fromFile` make CI-sourced secrets
first-class (VISION). This is cleaner than datasources, which leans on implicit
merge to avoid wiping unspecified secrets.

```yaml
apiVersion: assistant.ext.grafana.app/v1alpha1
kind: MCPServer
metadata:
  name: tenant-github                              # {scope}-{slug(name)}
  annotations:
    assistant.ext.grafana.app/id: <server-id>     # within-stack only
spec:
  name: GitHub
  scope: tenant                                    # drives scope-qualified update/delete
  url: https://api.githubcopilot.com/mcp/
  enabled: true
  headers:
    - name: Authorization
      fromEnv: GITHUB_MCP_TOKEN                    # sourced at push; name-only on pull → preserved
```

## Decision 6 — Prerequisites

- **Client offset-pagination.** Add offset paging to the MCP-servers client. The
  **adapter `List` (used by `pull`) exhausts all pages** so large stacks aren't
  truncated. The **human `mcp-servers list` stays bounded** to a default page and
  prints a "showing N of TOTAL — use `--limit` for more" hint, mirroring
  `gcx irm oncall alert-groups list`.
- **Fix update header handling** (PR #747 Major #1). `Update` must send the full
  desired header list per Decision 5; this corrects the current
  drop-on-tenant-update.

**Phasing fallback:** if composite-name identity proves awkward, a first cut can
bridge `user` scope only and leave `tenant` command-only, widening once proven.

## Consequences

- `gcx resources get/pull/push/delete` works for MCP servers; GitOps + cross-stack
  sync via `--context`.
- `assistant` becomes a first-class provider (config entry, redaction, `gcx providers`).
- Commands and adapter share one path → identical structured output.
- Secret round-trips are deterministic (explicit per-header write intent) and CI-friendly.
- Two PR #747 bugs (pagination truncation, update header handling) are fixed.
- The rare duplicate-name case surfaces as an error, not silent data loss.

## Follow-up

- `/plan-spec` this ADR (provider refactor + adapter; prerequisites first).
- On acceptance: update `CLAUDE.md` package map (`internal/providers/assistant`)
  and regenerate `mise run reference`.
