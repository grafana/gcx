# Proposal

## Why

Time intervals and template groups currently use provisioning payloads through dedicated commands, while their native resources are hidden from `gcx resources`. Exposing them with the same manifests on both surfaces, and fixing create requests for server-derived names, enables ordinary pull/edit/push and cross-stack transfer. This is [#1509](https://github.com/grafana/gcx/issues/1509), part of [#1473](https://github.com/grafana/gcx/issues/1473), following the routing-trees slice of [RFC 001](../../../docs/rfcs/001-alerting-provider-refactor.md).

## What Changes

- Add `gcx alert time-intervals` and `gcx alert template-groups`, each with `list`, `get <title>`, `create -f`, `update <title> -f`, and `delete <title>`. Read and write native `TimeInterval` and `TemplateGroup` manifests through `native.Bind`.
- Resolve dedicated command arguments by listing and exact title matching. Human tables and mutation receipts show the title and server-derived `metadata.name`; JSON/YAML reads preserve the server manifest.
- Expose `timeintervals` and `templategroups` in the notifications discovery allowlist, alongside `routingtrees`. Receivers and inhibition rules remain hidden.
- Add a resource-name policy shared by dedicated creates and generic push, keyed by resource kind rather than API version. Match the logical identity before a generic create, clear the server-derived name and source concurrency metadata on a copy, and retain the destination identity and current version on update. Accommodate historical template group name encodings within `v0alpha1`; ordinary resources retain their current name behavior.
- Dedicated `update <current-title>` selects an existing object and accepts a desired title change in its manifest, using the current API name and the caller's resource version. Template kind remains unchanged. Generic push instead matches the desired title/kind, creating or updating that identity while leaving any old identity untouched. GCX never infers a push rename from encoded metadata, rewrites local files, or recreates backend reference handling. See design D3 for the contract and UX examples.
- Share read-only classification for both native kinds: `spec.kind: mimir` or provenance `system`, `converted_prometheus`, or `file`. Reads retain them and tables mark their status; generic pull retains them by default with optional `--skip-read-only` exclusion; push/delete count and skip them, edit refuses them before the editor opens, and dedicated mutations reject them. A writable local identity matching a read-only destination fails before update. Unknown server template kinds remain readable and are ignored by logical matching; local manifests stay strict. Optional `--kind grafana|mimir` on get/delete disambiguates equal titles; update selects Grafana-managed groups.
- Require real API names for generic delete files; unnamed writable native files fail before any request for that file and point to dedicated title-based delete. Add optional failure `source` paths to mutation batch schema version 1.
- Retry only Kubernetes Conflict responses on TimeInterval/TemplateGroup dynamic-client mutations, inside the dry-run guard, without a serial alerting phase or adapter changes.
- Keep provisioning exports on `gcx alert mute-timings export --name <title> --format yaml|json|hcl`; native time intervals and template groups have no duplicate export leaf. Export covers Grafana-managed intervals only.
- Keep alerting kinds on the existing nonmutating client-side dry-run path and add regression tests for both API versions.
- Record public-source Grafana 12.x version evidence in RFC 001, distinguishing API registration from runtime verification.
- Keep `mute-timings` and `templates` unchanged; their deprecation belongs to #1512. No legacy fallback or new adapter registrations.

## Capabilities

### New Capabilities

- `alerting/time-intervals`: native time interval CRUD by title, provisioning export, and generic resource interoperability.
- `alerting/template-groups`: native template group CRUD by title, complete enumeration including imported groups, and read-only behavior.
- `resources/server-derived-names`: portable create/upsert behavior, identity validation, and safe preview for native kinds whose names are derived by the server.

### Modified Capabilities

- `alerting/routing-trees`: revise the generic discovery requirement to allow the two newly exposed notification kinds; routing-tree command behavior is unchanged.

## Impact

- **Code:** `internal/providers/alert/` command factories, title resolution, tables, and validation; shared helpers in `internal/providers/native/` only where independently reusable; resource-name policy under `internal/resources/`; `internal/resources/remote/pusher.go`; discovery allowlist; dry-run regression tests.
- **CLI metadata:** output protocol classes, token costs, hints, examples, and generated CLI reference for ten new CRUD leaves. CRUD is finite structured output; existing provisioning export remains raw output.
- **Docs:** RFC 001's version matrix and slice contract, relevant architecture/resource pipeline docs and package map as required by the doc-maintenance gate. No new glossary terms are needed.
- **Compatibility:** prefer the version advertised by discovery, with `v1beta1` preferred where served and verified `v0alpha1` support on older targets. No new dependencies, configuration keys, constitutional exceptions, or changes to existing command paths.
- **Verification:** unit/request-mapping tests, strict OpenSpec validation, required quality gates, and local/throwaway-context E2E with every command and result recorded before PR publication. Original implementation verification is recorded in tasks; review fixes require renewed gates and live checks before completion.
