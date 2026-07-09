---
type: feature-plan
title: "Assistant Provider + MCP Servers as Resources"
status: draft
spec: spec.md
created: 2026-07-07
---

# Plan: Assistant Provider + MCP Servers as Resources

## Pipeline Architecture

How the assistant provider slots into the existing registration flow and the resources pipeline. The current hand-mount is removed; the provider is mounted by the same registry loop as `slo`/`irm`/etc.

```
                         cmd/gcx/root/command.go
                        ┌───────────────────────────────────────────────┐
   blank-import         │  import _ "internal/providers/assistant"        │  ← FR-004 (replaces
   (startup)            │  import _ "internal/providers/slo" ... (peers)  │     hand-mount at :251)
                        └───────────────────────┬───────────────────────┘
                                                 │
                                    providers.Register(&AssistantProvider{})   ← FR-001 (single init)
                                                 │  (registry.go: appends provider +
                                                 │   loops TypedRegistrations() → adapter.Register)
                        ┌────────────────────────┴────────────────────────┐
                        │              AssistantProvider                    │
                        │  Name/ShortDesc/Validate/ConfigKeys               │  ← FR-002, FR-006
                        ├───────────────────────┬───────────────────────────┤
                        │      Commands()        │     TypedRegistrations()   │
                        │  (lift-and-shift)      │   (exactly one entry)      │  ← FR-003, FR-005
                        └───────────┬────────────┴──────────────┬────────────┘
                                    │                            │
        ┌───────────────────────────┴──────┐          ┌──────────┴───────────────────────┐
        │  assistant command tree           │          │  adapter.Registration (MCPServer) │
        │  (requireGrafanaCloud guard)      │          │  GVK assistant.ext.grafana.app/   │
        │   prompt   ─┐                      │          │      v1alpha1, Kind MCPServer      │  ← FR-008
        │   dashboard ├─ A2A / OAuth-PKCE    │          │  Schema + Example (non-nil)        │  ← FR-009
        │   conversation ─┘ (cmdconfig.Opts) │          │  TypedCRUD[MCPServer]              │  ← FR-007
        │   investigations ─┐ providers.     │          │  MetadataFn → {scope}-{slug(name)} │  ← FR-011
        │   mcp-servers    ─┘ ConfigLoader   │          │  RegisterNaturalKey(scope,name,url)│  ← FR-013
        └───────────┬───────────────────────┘          └──────────────┬────────────────────┘
                    │                                                  │
                    │   both paths share ONE data path (FR-022)        │
                    └───────────────────────┬──────────────────────────┘
                                            │
                             internal/assistant/mcpservers.Client
                        ┌────────────────────┴─────────────────────────┐
                        │  List(offset paging) ── adapter: exhaust all   │  ← FR-015
                        │                     └─ human list: bounded+hint │  ← FR-016
                        │  Get / Create / Update(full header list) /Delete│  ← FR-017
                        │  header write-intent: overwrite/preserve/remove │  ← FR-018/019
                        │  fromEnv/fromFile resolution; read → redacted   │  ← FR-020/021
                        └────────────────────┬─────────────────────────┘
                                            │  (closed-source management API — unchanged)
                                            ▼
                                   Grafana Assistant backend
```

`gcx resources get/pull/push/delete` reaches the same `TypedCRUD[MCPServer]` the `mcp-servers` commands use, guaranteeing identical structured output (CONSTITUTION §110-121). Push resolves create-vs-update via the natural key before applying per-header write intent.

## Design Decisions

| Decision | Rationale | Traces to |
|---|---|---|
| Single `internal/providers/assistant` package with one `init()` → `providers.Register()`; blank-import at startup; drop the hand-mount. | Adapter registration is legal only through `providers.Register()` (CONSTITUTION §28-32); this is the inseparable prerequisite for MCP-servers-as-resources. Matches `slo`/`irm`. | FR-001, FR-004 |
| `Commands()` lift-and-shifts the existing tree verbatim, keeping `requireGrafanaCloud` and the per-subcommand loader wiring. | ADR directs no rewrite; the existing `investigations`/`mcp-servers` loaders already use `providers.ConfigLoader` (§157-160-compliant). Minimizes regression surface for the self-hosted guard and OAuth path. | FR-003, FR-024 |
| `TypedRegistrations()` returns exactly one entry (MCPServer); investigations/conversation/A2A are command-only. | Investigations/conversation are outputs on the standard verb vocabulary; A2A is a distinct OAuth transport. Only MCP-server *configuration* belongs in the resources pipeline. | FR-005 |
| `TypedCRUD[MCPServer]` over a hand-rolled adapter; headers live inside `spec`. | Unlike datasources (write-only block sibling of `spec`), we author the manifest, so the strict `{metadata, spec}` envelope fits — keeping both paths on one code path and satisfying the TypedCRUD invariant without an exception. | FR-007, FR-022 |
| Dedicated `MCPServer` manifest domain type distinct from the client's read type `Server` / write type `ServerInput`. | The manifest carries write-intent header semantics and materialized identity fields that neither client type expresses; `Server` redacts values, `ServerInput` has no preserve/remove/`fromEnv` concept. | FR-007, FR-010, FR-018 |
| `metadata.name = {scope}-{slug(name)}`; scope/url/name in `spec`; ID as annotation; `RegisterNaturalKey(SpecFieldKey("scope","name","url"))`. | Names are not unique and scope is required+immutable; a synthesized name + materialized `spec` + `(scope,name,url)` natural key make the adapter well-behaved despite CONSTITUTION §122-129's caution about composite/scope keys (deviation approved via ADR). | FR-011, FR-012, FR-013 |
| Collision on `(scope, name)` with differing URLs is an error listing candidates, never silent resolution. | Duplicate `(scope, name)` is legal; silent resolution risks GitOps data loss. | FR-014 |
| Offset pagination: adapter `List` exhausts the **underlying** integration list (not the MCP-filtered subset); human `list` bounded + "showing first N" STDERR hint. | `pull` must not truncate; because MCP is filtered in gcx's client, a page can hold zero MCP servers while more exist later, so exhaustion keys off the raw pages. The list total spans all integrations, so it is never shown as an MCP count (verified 2026-07-08). | FR-015, FR-016 |
| `Update` sends the full desired header list; per-header write intent overwrite/preserve/remove; create-path name-only header errors. | Fixes the tenant-update header drop (PR #747 Major #1) and gives deterministic, merge-free GitOps round-trips. Preserve is only sound when the secret already exists. | FR-017, FR-018, FR-019 |
| Header values sourced inline / `fromEnv` / `fromFile`, resolved at push, redacted on read/pull. | CI-sourced secrets are first-class (VISION); pulled manifests never carry secrets. Mirrors the datasources `secure`-block sourcing pattern. | FR-020, FR-021 |
| `ConfigKeys()` declares the `providers.assistant.*` capability-cache keys as non-secret. | Prevents `config view` from redacting the (non-secret) capability cache once assistant becomes a registered provider. | FR-006 |
| Reconcile `mcp-servers` header-preservation help text with the write-intent model; update the masking test. | The current help promises preservation the code does not fully deliver; the test masks the header-drop bug. Docs and tests must match the fixed behavior. | FR-023, FR-017 |

## Compatibility

**Keeps working unchanged**
- All existing `gcx assistant mcp-servers` verbs (`list`, `get`, `create`, `update`, `delete`, `validate`, OAuth) — same UX; now backed by the same `TypedCRUD` data path.
- `investigations` and `conversation` command trees and verbs.
- The A2A `prompt`/`dashboard` OAuth-PKCE streaming path, its client, transport, and agents.
- The `requireGrafanaCloud` self-hosted guard and the existing `--config`/`--context` flag behavior on the assistant tree.

**Newly available**
- `gcx resources get/pull/push/delete mcpservers` for both `user` and `tenant` scope.
- GitOps round-trip and cross-stack fanout of MCP-server configuration via `--context`, matched on the `(scope, name, url)` natural key.
- `assistant` shown by `gcx providers`; MCPServer schema/example via the `schemas` command; non-redacted `providers.assistant.*` config keys in `config view`.
- Correct large-stack pagination on `pull`; a bounded + hinted human `list`.

**Deprecated / removed**
- Nothing is deprecated. The only removal is the internal hand-mount wiring at `cmd/gcx/root/command.go:251`, replaced by provider-registry mounting — a no-op to the user-facing command surface.

## Implementation Sequence

Ordered so the prerequisite client correctness lands before the provider/adapter build on top of it. Each step is independently reviewable.

1. **Client offset-pagination + exhaustion.** Add offset paging to the MCP-servers client and decode the list's pagination (offset/limit/total), which the client currently drops. Give the adapter path a full-exhaustion `List` that pages the **underlying** integration list until a short raw page (or offset ≥ total) and filters to MCP — it MUST NOT stop on a short *filtered* page. Keep a bounded variant for the human `list` with a "showing first N" hint (the total spans all integrations, so it is never shown as an MCP count) (FR-015, FR-016). Total-count Open Question resolved 2026-07-08.
2. **`Update` header-handling fix + masking-test fix + help reconcile.** Make `Update` send the full desired header list; add per-header write intent at the client boundary. Update the masking test (`client_test.go:338-387`) to assert headers are present and add a tenant-update-preserves-header case; reconcile the header-preservation help text (`commands.go:279-280`) (FR-017, FR-018, FR-023).
3. **Provider package skeleton.** Create `internal/providers/assistant`: `init()` → `providers.Register`; `Commands()` lift-and-shifts the existing tree (guard + per-subcommand loaders intact); `ConfigKeys()` declares `providers.assistant.*`; blank-import at startup and drop the hand-mount (FR-001, FR-003, FR-004, FR-006).
4. **MCPServer manifest type.** Add the dedicated `MCPServer` domain struct (materialized `scope`/`url`/`name`/`enabled`/`description`/`applications`/`config`/`headers`) implementing `ResourceIdentity`; hand-build `Schema` (`adapter.SchemaFromType`) and `Example` (FR-007, FR-009, FR-010).
5. **`TypedCRUD[MCPServer]` wiring.** Implement `List` (exhausting), `Get`, `Create`, `Update`, `Delete`; `MetadataFn` → `{scope}-{slug(name)}` and the ID annotation; `RegisterNaturalKey(SpecFieldKey("scope","name","url"))`; register via `TypedRegistrations()` (FR-005, FR-008, FR-011, FR-012, FR-013).
6. **Secret write-intent mapping.** Map the manifest header list onto overwrite/preserve/remove; add `fromEnv`/`fromFile` resolution at push; enforce redaction on read so pulls carry no values (FR-018, FR-020, FR-021).
7. **Create-path guard + collision-as-error.** Determine create-vs-update via natural-key match; error on a name-only header on create; error (listing candidates) on ambiguous `(scope, name)` lookups on `get`/`pull` (FR-014, FR-019).
8. **Docs + reference regen.** Update `CLAUDE.md` package map (add `internal/providers/assistant`), refresh `docs/architecture/` per the doc-maintenance checks, and run `GCX_AGENT_MODE=false mise run reference` to regenerate CLI/config/env docs.

## Risks (architectural)

| Risk | Impact | Mitigation |
|---|---|---|
| The list total spans all integrations, not just MCP servers (MCP filtered client-side); a page can hold zero MCP servers while more exist later. | A loop that stops on a short *filtered* page truncates `pull`; a "N of TOTAL" hint overstates the MCP count. | Exhaust on the underlying raw pages, not the filtered subset; human hint is "showing first N", never the integration total. Verified 2026-07-08. |
| Lift-and-shift of the bespoke config wiring (`cmdconfig.Options` + manual root-hook chaining + `requireGrafanaCloud`) into a provider `Commands()`. | Could break the self-hosted guard or the OAuth path; potential §157-160 friction on the A2A path. | Port verbatim; keep the adapter/`mcp-servers` path on `providers.ConfigLoader`; treat A2A `cmdconfig.Options` as a documented carve-out pending the Open Question. |
| Write-intent + `fromEnv`/`fromFile` is net-new with no direct analog in this client. | Secret leakage into pulled files or accidental wipe on update. | Mirror the datasources `secure`-block sourcing; round-trip tests for preserve-on-update, error-on-create, and redaction-on-read. |
| CONSTITUTION §122-129 deviation (adapter for a scope/composite-key resource). | Reviewer pushback / invariant tension. | Documented Key Decision; materialized `spec` + synthesized name + natural key make it well-behaved; approved via the ADR. |
