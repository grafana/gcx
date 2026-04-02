---
type: feature-spec
title: "Faro Provider (Frontend Observability)"
status: draft
beads_id:
created: 2026-04-02
---

# Faro Provider (Frontend Observability)

## Problem Statement

Faro (Frontend Observability) app management exists in the legacy gcx CLI but
has not been ported to gcx. Users cannot manage Faro apps or source maps
through the new tool. This blocks migration off the legacy CLI for teams using
Frontend Observability.

## Scope

### In Scope

- 5 CRUD commands on FaroApp: `faro apps list`, `get`, `create`, `update`, `delete`
- Full `TypedCRUD[FaroApp]` adapter registration (`resources get faroapps` path)
- 3 sourcemap sub-resource commands: `show-sourcemaps`, `apply-sourcemap`, `remove-sourcemap`
- Schema and example registration for FaroApp
- Table + wide codecs with slug-ID naming
- K8s envelope wrapping for json/yaml output
- httptest-based client tests, adapter round-trip tests, command tests

### Out of Scope

- `faro apps open` (browser link) — trivial, no API calls, low priority
- Faro-specific dashboards or alerting integration
- Source map processing/symbolication (server-side concern)

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Command structure | `faro apps {verb}` | Multi-resource provider (apps + sourcemaps), CONSTITUTION § CLI Grammar | ADR-001 Stage 1A |
| Sourcemaps as sub-resource | Verbs under `apps` (`show-sourcemaps`, etc.) | Requires parent app-id, CONSTITUTION § Sub-resources | ADR-001 Stage 1A |
| Sourcemap verbs | `show-`/`apply-`/`remove-` not `list`/`create`/`delete` | Not adapter-registered, CONSTITUTION § Provider-only resources | ADR-001 Stage 1A |
| Auth model | Empty ConfigKeys, standard Grafana SA token | Plugin proxy API, no separate auth | ADR-001 Stage 1C |
| HTTP client | `rest.HTTPClientFor` (not `ExternalHTTPClient`) | Calls go through Grafana server | ADR-001 Stage 1C |
| Package layout | Flat (no subpackages) | Single resource type | ADR-001 Stage 1D |
| Client pattern | Explicit `http.Client` + `host` fields | Recipe Step 3, not embedded `grafana.Client` | ADR-001 Stage 1D |

## Functional Requirements

- FR-001: The system MUST provide `gcx faro apps list` that lists all Faro apps with table/wide/json/yaml output.
- FR-002: The system MUST provide `gcx faro apps get [slug-id]` that retrieves a single Faro app by slug-id or bare numeric ID.
- FR-003: The system MUST provide `gcx faro apps get --name <name>` that retrieves a Faro app by name (client-side filter from list).
- FR-004: The system MUST provide `gcx faro apps create -f <file>` that creates a Faro app from a YAML/JSON manifest.
- FR-005: The system MUST provide `gcx faro apps update [slug-id] -f <file>` that updates a Faro app from a manifest.
- FR-006: The system MUST provide `gcx faro apps delete <slug-id>` that deletes a Faro app.
- FR-007: The system MUST register FaroApp as a `TypedCRUD[FaroApp]` adapter so that `resources get faroapps`, `resources push`, `resources pull`, and `resources delete` all work.
- FR-008: The system MUST register a non-nil Schema and Example on the adapter Registration struct.
- FR-009: FaroApp MUST implement `ResourceIdentity` (`GetResourceName`/`SetResourceName`) using slug-id convention (`adapter.SlugifyName`/`ExtractIDFromSlug`).
- FR-010: List and get commands MUST default to `text` format with table + wide codecs registered.
- FR-011: List and get commands MUST wrap json/yaml output in K8s envelope manifests (`apiVersion`/`kind`/`metadata`/`spec`).
- FR-012: Table output MUST show `NAME` column (slug-id), not bare numeric ID.
- FR-013: The system MUST provide `gcx faro apps show-sourcemaps <slug-id>` that lists source map bundles for an app.
- FR-014: The system MUST provide `gcx faro apps apply-sourcemap <slug-id> -f <file>` that uploads a source map bundle.
- FR-015: The system MUST provide `gcx faro apps remove-sourcemap <slug-id> <bundle-id>` that deletes a source map bundle.
- FR-016: Sourcemap commands MUST NOT be registered as typed adapters.
- FR-017: Sourcemap commands MUST use alternative verbs (`show-`/`apply-`/`remove-`), never standard CRUD verbs.
- FR-018: The provider MUST have exactly one `init()` function with a single `providers.Register()` call.
- FR-019: The provider MUST use `providers.ConfigLoader` for config/auth resolution.
- FR-020: Create MUST strip `ExtraLogLabels` and `Settings` from the API payload (Faro API bugs).
- FR-021: Create MUST re-fetch the created app via List to obtain complete fields (`collectEndpointURL`, `appKey`).
- FR-022: Update MUST include the numeric ID in both the URL path and request body.
- FR-023: Update MUST strip `Settings` from the API payload.
- FR-024: The client MUST convert `ExtraLogLabels` between map (Go) and array-of-`{key,value}` (wire format).
- FR-025: The client MUST convert `ID` between string (Go) and int64 (wire format).

## Acceptance Criteria

- AC-01: GIVEN a configured Grafana context
  WHEN `gcx faro apps list` is run
  THEN all Faro apps are listed in table format with NAME, AppKey, CollectEndpointURL columns.

- AC-02: GIVEN a configured Grafana context
  WHEN `gcx faro apps list -o json` is run
  THEN output is a K8s-wrapped list with `apiVersion`, `kind`, `items[]` containing `metadata`/`spec`.

- AC-03: GIVEN a Faro app exists with slug-id `my-app-123`
  WHEN `gcx faro apps get my-app-123` is run
  THEN the app is displayed in table format.

- AC-04: GIVEN a Faro app exists with name `MyApp`
  WHEN `gcx faro apps get --name MyApp` is run
  THEN the app is retrieved via client-side filter and displayed.

- AC-05: GIVEN a valid FaroApp manifest file
  WHEN `gcx faro apps create -f app.yaml` is run
  THEN the app is created and a success status message is shown with the app name.

- AC-06: GIVEN a Faro app exists
  WHEN `gcx faro apps update my-app-123 -f app.yaml` is run
  THEN the app is updated and a success status message is shown.

- AC-07: GIVEN a Faro app exists
  WHEN `gcx faro apps delete my-app-123` is run
  THEN the app is deleted and a success status message is shown.

- AC-08: GIVEN a configured Grafana context
  WHEN `gcx resources get faroapps` is run
  THEN Faro apps are listed via the adapter path.

- AC-09: GIVEN a configured Grafana context
  WHEN `gcx resources schemas` is run
  THEN FaroApp schema appears in the output.

- AC-10: GIVEN a Faro app exists with sourcemaps
  WHEN `gcx faro apps show-sourcemaps my-app-123` is run
  THEN source map bundles are listed.

- AC-11: GIVEN a valid source map bundle file
  WHEN `gcx faro apps apply-sourcemap my-app-123 -f bundle.json` is run
  THEN the bundle is uploaded and a success message is shown.

- AC-12: GIVEN a source map bundle exists
  WHEN `gcx faro apps remove-sourcemap my-app-123 bundle-456` is run
  THEN the bundle is deleted and a success message is shown.

- AC-13: GIVEN any faro apps command with `-o json` or `-o yaml`
  WHEN output is produced
  THEN json/yaml output from provider commands and `resources get` is identical.

- AC-14: GIVEN the provider is registered
  WHEN `gcx providers` is run
  THEN `faro` appears in the provider list.

- AC-15: GIVEN no code changes outside `internal/providers/faro/` and `cmd/gcx/root/command.go`
  WHEN `GCX_AGENT_MODE=false make all` is run
  THEN it exits 0 with no lint errors and all tests passing.

## Negative Constraints

- NEVER infer response envelope shapes — copy deserialization from gcx source verbatim.
- NEVER send `ExtraLogLabels` on create requests (API returns 409).
- NEVER send `Settings` on create or update requests (API returns 500).
- NEVER register sourcemap operations as typed adapters.
- NEVER use standard CRUD verbs (`list`, `get`, `create`, `update`, `delete`) for sourcemap commands.
- NEVER embed `grafana.Client` — use explicit `http.Client` + `host` fields.
- NEVER use `ExternalHTTPClient()` — Faro goes through the Grafana plugin proxy.
- NEVER construct HTTP clients outside `ConfigLoader` resolution.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Faro API envelope shape differs from gcx source | Wrong deserialization, silent data loss | Copy `DecodeResponse` pattern verbatim, verify with smoke tests |
| Slug-ID mapping fails for edge cases | Get/update/delete by slug-id breaks | Test with numeric-only names, special characters |
| Create re-fetch via List is racy | Wrong app returned if name collision | Match by name exactly, same pattern as gcx |
| Plugin proxy auth changes | 401/403 on all operations | Standard Grafana SA token, same as KG/incidents |

## Open Questions

- [RESOLVED]: Should sourcemaps be a sibling or sub-resource of apps? — Sub-resource (requires parent app-id, CONSTITUTION § Sub-resources)
- [RESOLVED]: Should sourcemaps use CRUD verbs? — No, alternative verbs per CONSTITUTION § Provider-only resources
- [DEFERRED]: Should `faro apps open` be included? — No, deferred (browser link, no API calls)
