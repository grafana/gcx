---
type: feature-spec
title: "Assistant Provider + MCP Servers as Resources"
status: approved
adr: docs/adrs/assistant-provider/001-assistant-provider-and-mcp-servers-as-resources.md
created: 2026-07-07
---

# Assistant Provider + MCP Servers as Resources

## Problem Statement

`gcx assistant` is mounted by hand (`cmd/gcx/root/command.go:251`, `rootCmd.AddCommand(assistantcmd.Command())`), **outside** the provider-registration loop at `command.go:267`. There is no `internal/providers/assistant` package and the assistant satisfies none of the `providers.Provider` interface. Because an adapter self-registers *only* through a provider's `TypedRegistrations()` (CONSTITUTION §28-32: no `adapter.Register()` outside `providers.Register()`), the assistant's MCP-server configuration **cannot reach the `gcx resources get/pull/push/delete` pipeline** and cannot participate in GitOps.

MCP servers are *configuration* — which remote MCP endpoints a stack trusts, their scope, and their auth — exactly the versionable configuration VISION's GitOps model targets ("pull to files, version in git, push back; same manifests across `--context`"). Investigations, the sibling group, are *outputs* (run artifacts) and stay command-only. Today the only way to manage MCP-server config is the hand-mounted `gcx assistant mcp-servers` command tree added in PR #747, which is disconnected from the resources pipeline, cannot fan out across stacks, and carries two latent bugs (list truncation; tenant-update header drop).

**Who is affected:** operators and CI pipelines that want MCP-server configuration under source control and synced across stacks; agents that expect uniform `gcx resources` behavior for every configuration surface.

**Current workaround:** manual, per-stack `gcx assistant mcp-servers` invocations with no GitOps round-trip and no cross-stack fanout.

## Scope

### In Scope

- Register `assistant` as a first-class provider following the `slo`/`irm` template — one provider whose `Commands()` mounts the existing assistant command tree and whose `TypedRegistrations()` returns exactly one adapter (MCPServer). Blank-import it at startup; drop the current hand-mount.
- A dedicated `MCPServer` manifest domain type registered via `TypedCRUD[MCPServer]`, so MCP-server configuration flows through `gcx resources get/pull/push/delete` for **both** `user` and `tenant` scope.
- Composite identity: `metadata.name = {scope}-{slug(name)}`; `spec` materializes `scope`, `url`, `name`, `enabled`, `description`, `applications`, `config`, and `headers`; the server-assigned opaque ID rides as an annotation (within-stack only); cross-stack matching uses the natural key `(scope, name, url)`.
- Secret round-trip via explicit per-header write intent (overwrite / preserve-existing / remove), with `fromEnv`/`fromFile` sourcing for CI and a create-path error when a name-only header has no value.
- Two prerequisite client fixes, bundled as early tasks of this deliverable:
  1. Offset pagination — the adapter `List` (used by `pull`) exhausts all pages; the human `mcp-servers list` stays bounded to a default page and prints a hint.
  2. Correct `Update` header handling — `Update` sends the full desired header list, fixing the tenant-update header drop (PR #747 Major #1).
- `ConfigKeys()` declaring the existing non-secret `providers.assistant.*` capability-cache keys so `config view` does not redact them.
- Documentation updates: `CLAUDE.md` package map (add `internal/providers/assistant`) and regenerated CLI/config/env reference via `mise run reference`.

### Out of Scope

- **The human `gcx assistant mcp-servers` command verbs** — they stay; this feature routes them through the adapter code path but does not rename or remove them.
- **Investigations and `conversation`** — keep their existing verbs and behavior unchanged (they are outputs, not configuration; aligned with the unified verb-vocabulary direction). No adapter registration for them.
- **The A2A `prompt`/`dashboard` path** — its OAuth-PKCE streaming client and transport are unchanged. No new agents, no streaming changes.
- **OAuth-flow changes** for MCP servers — `validate` and OAuth initiation stay as extension verbs on the command tree, not adapter operations.
- **Any change to the closed-source Assistant backend/management API** — this feature only changes gcx-side client, provider, and adapter code.
- **A separate `adapter.Register()` call** — registration happens exclusively via `providers.Register()`.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|---|---|---|---|
| Provider shape | One `assistant` provider; `Commands()` = existing tree, `TypedRegistrations()` = MCPServer only | Adapter registration is only legal via `providers.Register()` (CONSTITUTION §28-32); making assistant a provider is the inseparable prerequisite. Follows `slo`/`irm`. | ADR Decision 1 |
| Non-adapter groups | Investigations + A2A + `conversation` stay exactly as shipped | Investigations/conversation are outputs sharing the standard verb vocabulary; A2A OAuth transport is a distinct mechanism, out of scope. | ADR Decision 2 |
| Identity | `metadata.name = {scope}-{slug(name)}`; scope/url/name materialized in `spec`; server ID as annotation (within-stack only); natural key `(scope, name, url)` | Server names are not unique and scope is required + immutable, so name alone is ambiguous; scope must be self-contained in `spec` to drive scope-qualified operations. | ADR Decision 3 |
| Adapter | `TypedCRUD[MCPServer]` with headers inside `spec`; GVK `assistant.ext.grafana.app/v1alpha1`, Kind `MCPServer`, plural `mcpservers` | Headers live inside `spec`, so the strict `{metadata, spec}` envelope fits — unlike datasources (write-only block sibling of `spec`). Keeps command and resources paths on one code path (CONSTITUTION §110-121). | ADR Decision 4 |
| Secrets | Manifest header list mapped onto per-header write intent (overwrite / preserve / remove); `fromEnv`/`fromFile` for CI; create-path name-only header errors | Header values are write-only (redacted on read); explicit intent gives deterministic GitOps round-trips without an implicit-merge assumption. | ADR Decision 5 |
| Prerequisites | Client offset-pagination (adapter exhausts; human list bounded + hint) + `Update` header-handling fix | Adapter `pull` must not truncate large stacks; the tenant-update header drop is a real PR #747 bug. | ADR Decision 6 |
| Scope coverage | Full `user` + `tenant` in one deliverable (composite identity now) | The phasing fallback (user-scope-only first) is explicitly NOT taken; both scopes are specified together. | User (planning) |
| Bundling | Prerequisite client fixes are early internal tasks of this single deliverable, sequenced first | The two fixes are inseparable from the provider refactor and the adapter's correctness. | User (planning) |
| Total-count | Human `list` uses a "showing first N" hint; the adapter exhausts on the underlying paginated list | Verified (2026-07-08): the API is offset-paginated and returns a total, but that total spans **all** assistant integrations — MCP servers are narrowed in gcx's client (`internal/assistant/mcpservers/client.go`), not by the list request — so it is not a truthful MCP-server count and a "N of TOTAL" MCP hint is not offered. | Verification (2026-07-08) |

## Functional Requirements

**Provider registration**

- **FR-001** — The system MUST introduce an `internal/providers/assistant` package that registers the assistant via a single `init()` calling `providers.Register(...)`, with no `adapter.Register()` call anywhere outside `providers.Register()` (CONSTITUTION §28-32).
- **FR-002** — `assistant` MUST appear in `gcx providers` output with a name and short description.
- **FR-003** — `Commands()` MUST return the existing assistant command tree unchanged in surface: `prompt`, `dashboard`, `conversation`, `investigations`, and `mcp-servers`, preserving the `requireGrafanaCloud` guard and the existing per-subcommand config wiring (lift-and-shift, not rewrite).
- **FR-004** — The current hand-mount at `cmd/gcx/root/command.go:251` MUST be removed and replaced by a blank-import of the new provider package at the startup import site (`cmd/gcx/root/command.go:36-53`), so the provider is mounted by the same registry loop as every other provider.
- **FR-005** — `TypedRegistrations()` MUST return exactly one `adapter.Registration` — the MCPServer adapter. Investigations, `conversation`, and A2A MUST NOT be registered as adapters.
- **FR-006** — `ConfigKeys()` MUST declare the existing non-secret `providers.assistant.*` capability-cache keys (e.g. the Lodestone-v2 capability flag) with `Secret: false`, so `config view` does not redact them.

**Adapter shape & schema**

- **FR-007** — The MCPServer adapter MUST use `TypedCRUD[MCPServer]` (not a hand-rolled `ResourceAdapter`). The domain type MUST implement `ResourceIdentity` (`GetResourceName`/`SetResourceName`) per CONSTITUTION §33-36.
- **FR-008** — The registration MUST use GVK `assistant.ext.grafana.app/v1alpha1`, Kind `MCPServer`, plural `mcpservers`.
- **FR-009** — The `adapter.Registration` MUST carry a non-nil `Schema` and, because MCPServer is writable, a non-nil `Example` (CONSTITUTION §42-47). Both MUST be set on the `Registration` struct (not relied upon via `AsAdapter()`, which does not propagate them).
- **FR-010** — The MCPServer manifest `spec` MUST round-trip all user-editable fields losslessly: `name`, `scope`, `url`, `enabled`, `description`, `applications`, `config`, and `headers`. A `pull` followed by an unmodified `push` back to the same stack MUST be a no-op-equivalent update that preserves every field.

**Identity & cross-stack**

- **FR-011** — `metadata.name` MUST be computed as `{scope}-{slug(name)}`. Scope MUST be read from `spec.scope` for every scope-qualified operation and MUST NOT be parsed back out of `metadata.name`.
- **FR-012** — The server-assigned opaque ID MUST be written as a `metadata.annotations` entry for within-stack addressing and MUST be ignored for cross-stack matching.
- **FR-013** — The adapter MUST register a natural key of `(scope, name, url)` via `adapter.RegisterNaturalKey(gvk, adapter.SpecFieldKey("scope","name","url"))`, so `push` matches an existing server on that tuple and is idempotent regardless of the server-assigned ID.
- **FR-014** — When a lookup (`get`, or the `pull` filename derivation) resolves to more than one server sharing `(scope, name)` with differing URLs, the system MUST return an error that lists the candidate servers and MUST NOT silently pick one.

**Pagination (prerequisite)**

- **FR-015** — The MCP-servers client MUST support offset pagination, decoding the list's own pagination cursor/total (which the client currently drops). The adapter `List` (used by `resources pull`/`get`) MUST exhaust all pages so large stacks are never truncated. Because MCP servers are narrowed in gcx's client (not by the list request), exhaustion MUST be driven by the **underlying** paginated integration list — paging until the raw page is short (or the reported offset/total is reached) — and MUST NOT stop when a *filtered* page yields few or zero MCP servers (a page can contain zero MCP servers while more exist on later pages).
- **FR-016** — The human `gcx assistant mcp-servers list` MUST stay bounded and MUST print a hint to STDERR indicating more results may exist (CONSTITUTION §90-92). Because the list total is not MCP-specific (see FR-015), the hint SHALL read as "showing first N — use `--limit` for more" (alert-groups style); it MUST NOT present the integration total as an MCP-server count. The bounded human page MAY under-represent MCP servers when a page is dominated by non-MCP integrations — acceptable for the human path, since agents and GitOps use the exhausting adapter path (FR-015).

**Update / secret write intent**

- **FR-017** — `Update` MUST send the full desired header list derived from the manifest, fixing the current tenant-update header drop (PR #747 Major #1). A tenant server's stored auth header MUST NOT be wiped by an update that does not intend to change it.
- **FR-018** — The adapter MUST map the manifest header list onto per-header write intent: a header with a supplied value → **overwrite**; a header with a name but no value → **preserve** the stored secret (update-of-existing only); a header absent from the manifest → **remove**.
- **FR-019** — On a **create** path (new server, including first-time cross-stack sync), a name-only header (no value) MUST produce an actionable error instructing the user to supply the value via `fromEnv`/`fromFile`. The adapter MUST determine create-vs-update by natural-key match before applying write intent.
- **FR-020** — The manifest header list MUST support inline value, `fromEnv` (environment variable name), and `fromFile` (path) sourcing. Header values sourced via `fromEnv`/`fromFile` MUST be resolved at `push` time and MUST NOT be persisted into pulled manifests.
- **FR-021** — On read/`pull`, header values MUST remain redacted — the pulled manifest MUST carry header names (marking them for preserve) but MUST NOT contain any secret value.

**Output parity & config**

- **FR-022** — The JSON and YAML output for an MCP server MUST be identical between the `gcx assistant mcp-servers` command path and the `gcx resources` path, because both MUST use the same registered `TypedCRUD[MCPServer]` for data access (CONSTITUTION §110-121). Table/wide output MAY diverge.
- **FR-023** — The command help that today promises header preservation (`cmd/gcx/assistant/mcpservers/commands.go:279-280`) MUST be reconciled with the explicit write-intent model so the documented behavior matches the implemented behavior.
- **FR-024** — Provider config and auth for the MCP-servers/adapter path MUST be resolved through `providers.ConfigLoader` (CONSTITUTION §157-160); the provider MUST NOT construct HTTP clients or load credentials independently for that path.

## Acceptance Criteria

**Provider surface**

- **AC-001** (FR-001, FR-002)
  - GIVEN a built `gcx` binary
  - WHEN the user runs `gcx providers`
  - THEN `assistant` appears in the list with its short description, and the binary contains no `adapter.Register()` call outside `providers.Register()`.

- **AC-002** (FR-003, FR-004)
  - GIVEN the assistant is registered as a provider
  - WHEN the user runs `gcx assistant --help`
  - THEN `prompt`, `dashboard`, `conversation`, `investigations`, and `mcp-servers` are all present, the self-hosted guard still blocks non-Cloud contexts, and the tree is mounted by the provider registry loop (not the removed hand-mount).

- **AC-003** (FR-006)
  - GIVEN a config context whose `providers.assistant.*` capability-cache keys are set
  - WHEN the user runs `gcx config view`
  - THEN the `providers.assistant.*` capability-cache values are shown in cleartext (not redacted).

**Resources path — both scopes**

- **AC-004** (FR-005, FR-007, FR-008, FR-015)
  - GIVEN a stack with at least one `user`-scoped and one `tenant`-scoped MCP server
  - WHEN the user runs `gcx resources get mcpservers` (or `gcx resources pull mcpservers`)
  - THEN both servers are returned as `assistant.ext.grafana.app/v1alpha1` `MCPServer` objects, regardless of scope.

- **AC-005** (FR-011, FR-010)
  - GIVEN a `tenant`-scoped server named `GitHub`
  - WHEN the user pulls it
  - THEN `metadata.name` is `tenant-github`, and `spec` contains `name: GitHub`, `scope: tenant`, `url`, `enabled`, and (where set) `description`, `applications`, `config`, `headers`.

- **AC-006** (FR-022)
  - GIVEN one MCP server
  - WHEN the user retrieves it once via `gcx assistant mcp-servers get <ref> -o json` and once via `gcx resources get mcpservers/<name> -o json`
  - THEN the two JSON documents are byte-identical (and likewise for `-o yaml`).

**Identity & collision**

- **AC-007** (FR-012, FR-013)
  - GIVEN a server pulled from stack A that is pushed to stack B via `--context` (where B does not yet have it)
  - WHEN `gcx resources push` runs against B
  - THEN the server is matched on `(scope, name, url)` and created on B, and the stack-A server-assigned ID annotation does not affect the match.

- **AC-008** (FR-014)
  - GIVEN two `tenant`-scoped servers both named `GitHub` with different URLs
  - WHEN the user runs `gcx resources get mcpservers/tenant-github`
  - THEN the command exits non-zero with an error listing both candidate servers, and does not return either one silently.

**Pagination**

- **AC-009** (FR-015)
  - GIVEN a stack whose MCP servers span multiple underlying pages, including at least one page that contains no MCP servers (only other integration types)
  - WHEN `gcx resources pull mcpservers` runs
  - THEN every MCP server across all pages is written locally (no truncation), and paging does not stop early at the MCP-empty page.

- **AC-010** (FR-016)
  - GIVEN a stack with more MCP servers than the default page size
  - WHEN the user runs `gcx assistant mcp-servers list`
  - THEN the output stays bounded and a hint is printed to STDERR reading "showing first N — use `--limit` for more", and the hint does NOT present the integration total as an MCP-server count.

**Update / secrets**

- **AC-011** (FR-017)
  - GIVEN a `tenant`-scoped server with a configured auth header
  - WHEN the user edits an unrelated field (e.g. `enabled`) locally and runs `gcx resources push` (update path), leaving the header name-only
  - THEN after the update the server still has its auth header configured (the stored secret is preserved, not wiped).

- **AC-012** (FR-018, FR-021)
  - GIVEN a pulled manifest whose header is name-only (value redacted on read)
  - WHEN it is pushed back to the same stack (update-of-existing)
  - THEN the header is marked "preserve" and no secret value is sent, and the pulled manifest on disk contains no secret value.

- **AC-013** (FR-019)
  - GIVEN a manifest for a not-yet-existing server (create path) whose header is name-only
  - WHEN `gcx resources push` runs
  - THEN the push fails with an actionable error naming the header and instructing the user to supply its value via `fromEnv`/`fromFile`, and no valueless header is created.

- **AC-014** (FR-020)
  - GIVEN a manifest header sourced via `fromEnv: GITHUB_MCP_TOKEN` and the env var set
  - WHEN `gcx resources push` runs (create path)
  - THEN the header is created with the resolved value, and a subsequent `pull` writes the header back name-only (no value on disk).

- **AC-015** (FR-018)
  - GIVEN a server that has header `X` configured
  - WHEN a manifest that omits header `X` entirely is pushed (update path)
  - THEN header `X` is removed from the server.

**Round-trip & help**

- **AC-016** (FR-010)
  - GIVEN any MCP server
  - WHEN the user runs `gcx resources pull mcpservers` then `gcx resources push` of the unmodified files back to the same stack
  - THEN `name`, `scope`, `url`, `enabled`, `description`, `applications`, and `config` are unchanged, and configured headers remain configured.

- **AC-017** (FR-023)
  - GIVEN the updated `mcp-servers` command help
  - WHEN a reviewer reads the header-handling help text
  - THEN it describes the explicit write-intent model (overwrite / preserve / remove) and no longer promises a behavior the implementation does not perform.

## Negative Constraints

- **NEVER** call `adapter.Register()` for MCPServer (or any type) outside `providers.Register()`; registration happens solely via the provider's `init()` → `providers.Register()` (CONSTITUTION §28-32).
- **NEVER** rename or remove the existing `investigations`, `conversation`, `mcp-servers`, `prompt`, or `dashboard` verbs.
- **NEVER** change the A2A OAuth-PKCE streaming client, its transport, or its agents.
- **NEVER** change the closed-source Assistant backend/management API.
- **NEVER** parse `scope` out of `metadata.name`; scope MUST be read from `spec.scope`.
- **NEVER** return a stored header secret on read or write any secret value into a pulled manifest on disk.
- **NEVER** silently resolve a duplicate `(scope, name)` collision — it MUST surface as an error listing candidates.
- **NEVER** silently create a valueless header on a create path — it MUST error with an actionable message.
- **NEVER** truncate the adapter `List` used by `pull`; it MUST exhaust all pages.
- **NEVER** treat the server-assigned ID annotation as a cross-stack identity key.
- **DO NOT** introduce a second `init()`/`providers.Register()` for the assistant, or split it into two providers.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| The list total spans ALL assistant integrations, not just MCP servers (MCP is filtered in gcx's client), so a naive loop that stops on a short *filtered* page truncates, and a "N of TOTAL" hint would overstate the MCP count. | Silent truncation of `pull`; misleading human count. | Drive exhaustion off the underlying paginated list, not the filtered subset (FR-015); use a "showing first N" human hint that never presents the integration total as an MCP count (FR-016). Verified 2026-07-08. |
| The `Update` header-drop bug is masked by an existing test that never asserts headers are present (`internal/assistant/mcpservers/client_test.go:338-387`). | A fix could pass CI while the bug persists, or a regression could reappear silently. | The fix task MUST update that test to assert the full desired header list is sent, and add a tenant-update-preserves-header case. |
| CONSTITUTION §122-129 discourages adapters for resources with composite keys / scope-required lookups (names MCPServer's exact shape). | Registering MCPServer as an adapter is a deliberate deviation from an invariant-adjacent rule. | Materialize scope/url/name into `spec`, synthesize a stable `metadata.name`, and register a `(scope, name, url)` natural key — making it a well-behaved adapter. Deviation is human-approved via this ADR and recorded as a Key Decision. |
| The per-header write-intent + `fromEnv`/`fromFile` model is net-new (no existing analog in the MCP client; closest is the datasources `secure` block). | Risk of leaking secrets into pulled files or wiping stored secrets on update. | Mirror the datasources `secure`-block sourcing pattern; enforce redaction on read (FR-021), preserve-on-name-only for updates, and error-on-create for name-only headers. Cover with round-trip tests. |
| Command-tree lift-and-shift friction: the assistant parent binds `cmdconfig.Options` + bespoke root-hook chaining + `requireGrafanaCloud`, while `slo`/`irm` bind a single `providers.ConfigLoader`; investigations/mcp-servers each build their own loader. | Mechanical port could break the self-hosted guard or the config wiring, or violate CONSTITUTION §157-160 for the A2A path. | Lift-and-shift the tree verbatim (ADR directive); keep the existing per-subcommand `ConfigLoader` wiring for the adapter path (already §157-160-compliant). Treat A2A's `cmdconfig.Options`/OAuth usage as an Open Question (documented carve-out vs converge). |
| Slug collision: two distinct servers can yield the same `{scope}-{slug(name)}` `metadata.name`. | Ambiguous local filename / `get` target. | Handled by the collision-as-error requirement (FR-014); natural key `(scope, name, url)` still disambiguates on push. |

## Open Questions

- **[RESOLVED 2026-07-08]** Does the management API's list expose a reliable MCP-server total? Verified against the assistant API: listing is offset-paginated and DOES return a total, but the total spans **all** assistant integrations, not just MCP servers — MCP servers are narrowed in gcx's client (`internal/assistant/mcpservers/client.go`), not by the list request, so the total is not an MCP-server count and a page may hold zero MCP servers while more exist on later pages. Consequences baked into the spec: adapter exhaustion is driven by the underlying paginated list, not the filtered subset (FR-015); the human hint is "showing first N", never a false "N of TOTAL" (FR-016). This refines the ADR's "returns a total count" note (the total is not MCP-specific).
- **[DEFERRED — effort assessed 2026-07-08]** Should the A2A path (`prompt`/`dashboard`/`conversation`) converge onto `providers.ConfigLoader`? Investigated: `providers.ConfigLoader` / `NewNamespacedRESTConfig` **already supports OAuth-PKCE** (proxy mode + refresh transport + token persistence) — the "ConfigLoader is SA-token only" assumption is false. Two options. **Shape A** — config-resolution convergence: thread the loader for `--config`/`--context`, source config via `LoadFullConfig`, keep the existing OAuth streaming transport; adds one small public loader accessor → **effort ≈ M (~0.5–1 day)**. **Shape B** — full transport convergence: drop the bespoke OAuth machinery (as `investigations` did), which needs a config-layer change to expose the A2A base URL without the plugin-proxy suffix plus an SSE/approval refactor → **effort ≈ L (>2 days), higher blast radius**. Cleanly separable from this deliverable (only lightly co-edits `cmd/gcx/assistant/command.go`). Out of scope here regardless (ADR directs lift-and-shift); decision deferred to a follow-up. Recommendation on the table: **Shape A**.
- **[RESOLVED]** Scope → namespace mapping is rejected (namespace is context-global and rewritten on push); scope is materialized into `spec` instead (ADR Decision 3).
- **[RESOLVED]** Server ID as `metadata.name` is rejected (not portable); ID rides as a within-stack annotation only (ADR Decision 3).
- **[RESOLVED]** Both `user` and `tenant` scope are delivered together with composite identity in this single deliverable (user planning decision).
- **[DEFERRED]** The ADR's phasing fallback (bridge `user` scope first, keep `tenant` command-only) is explicitly NOT taken; retained only as a contingency if composite-name identity proves unexpectedly awkward during implementation.
