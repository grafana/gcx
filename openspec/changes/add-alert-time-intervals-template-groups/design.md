# Design

## Context

See [proposal.md](proposal.md) for motivation and the capability deltas under `specs/` for behavior. The routing-tree implementation establishes the patterns to reuse:

- `internal/providers/native/native.go` resolves descriptors through filtered discovery and provides an unstructured dynamic client. `native.ReadManifest` already supports JSON/YAML and injected stdin.
- `internal/providers/alert/routing_trees_commands.go` owns validation, optimistic updates, mutation receipts, and delete confirmation. The alert provider can mount two additional native groups without adapter registrations.
- `partiallyExposedGroups` currently exposes only `routingtrees` in the notifications group. Adding commands alone would leave `native.Bind` unable to resolve the two new kinds.
- `remote.Pusher.upsertResource` first gets by metadata name, then tries adapter-backed natural-key matching before create. Existing updates use the destination resource version. Neither native kind has a natural-key extractor, and create still carries the source name.
- `dryrun_allowlist.go` is default-deny. Both new kinds already fall outside the trusted server-dry-run set; `dryrun_guard.go` blocks their mutation requests and reports unverified/skipped preview.

The public tagged-source check found a material compatibility detail: Grafana 12.0–12.3 TemplateGroup has title/content and a title-only encoded name. Grafana 12.4 adds `spec.kind` and changes the name to encoded `kind|title`, **within the same `v0alpha1` API version**. See RFC 001's version matrix for public sources. The current contract supplied for #1509 serves `v0alpha1` and preferred `v1beta1` with the newer schema. Do not infer one identity encoding from an API version alone.

## Goals / Non-Goals

**Goals:**

- Preserve unknown native fields and metadata in reads; use the same manifests across both command surfaces.
- Separate logical title/kind matching from opaque API names, and make generic push portable and repeatable across the observed encoding change.
- Put server-name handling below providers so receivers can opt into it in #1510 without duplicating push logic or introducing provider-to-provider imports.

**Non-Goals:**

- Backend fallback, typed native structs, descriptor/schema registrations, a command generator, reference migration, dependency ordering, or multi-resource transactions.
- Exposing receivers/inhibition rules, altering released provisioning commands, or applying their deprecation before #1512.
- Inferring rename intent from generic push, local-file rewriting after a mutation, or filtering imported objects out of read inventories.

## Decisions

The decisions below are settled for this change. Implementation remains a separate step, tracked in `tasks.md`; source-confirmed compatibility findings still require the listed runtime verification.

### D1. Two groups reuse the native binding and existing command conventions

Bind `timeintervals` and `templategroups` once in the alert provider's command factories. Every leaf uses opts/setup/Validate, argument validation, `cmd.InOrStdin()`, and the output codecs. Load only after local validation; delete resolves and mutates only after confirmation has been granted. Reuse the existing table/list shape and mutation receipt machinery, with small alert-local helpers for repeated title resolution and version validation. Keep native package additions free of direct CLI imports.

| Surface | Subject/input | Human default | Version selection |
|---|---|---|---|
| `list` | No arguments | Table | Discovery preferred, or `--api-version` |
| `get <title>` | Exact, nonempty string | YAML, preserving the server object | Discovery preferred, or flag |
| `create -f <file>` | One native JSON/YAML object; `-` means injected stdin | Mutation summary | Manifest `apiVersion` |
| `update <current-title> -f <file>` | Current manifest, retaining its version; spec supplies the desired title | Mutation summary | Manifest `apiVersion` |
| `delete <title>` | Exact title, confirmation/`--force` | Mutation summary | Discovery preferred, or flag |

Explicitly empty filename, API-version, kind, and export-format flags fail with usage status instead of silently selecting defaults. Wrong group/kind, conflicting manifest/flag versions, malformed identity fields, and missing update version fail before network I/O. Empty `spec.kind` is invalid when present; absent historical kind is normalized to `grafana` for matching only. Do not add undocumented defaulting to structured reads.

CRUD leaves are `finite`; provisioning export stays on the existing raw `mute-timings export` leaf. Register eleven output-class and agent-metadata entries. List/get worst-case cost is medium because interval inventories and template content are unbounded; hints show title-scoped get and field selection. Mutation receipts are small and contain the resource identity and resulting version. Use `gcx commands --flat -o json` and `help-tree` to review the rendered routing metadata.

### D2. Dedicated lookup lists and matches titles, with no encoded-name shortcut

Read all pages through the existing dynamic client, then match `spec.name` for time intervals or `spec.title` for template groups, case-sensitively. No decoding or synthesized GET path replaces the user's chosen title contract. After resolution, get issues GET with the returned `metadata.name`; update/delete use that identity. List/get remain format-agnostic and return native objects.

Zero matches produce a typed not-found error for the title. Multiple matches fail clearly and identify candidates; neither list order nor a preference for editable groups chooses an object. Template `get` and `delete` accept optional `--kind grafana|mimir`; update accepts only Grafana-managed manifests and resolves the Grafana-managed group with the argument title, treating absent historical kind as `grafana`. An imported manifest is rejected as read-only; if the argument title exists only on an imported group, a Grafana-managed manifest gets not found. This makes equal-title imports usable without changing positional arguments to encoded names. Unknown server template kinds remain raw in list output with `READ-ONLY: unknown`; a known-kind filter ignores them, while unfiltered title lookup includes them as candidates. Local manifests still reject unknown kinds.

Time interval tables show `TITLE`, `API_NAME`, interval count (`spec.time_intervals`), and `READ-ONLY`; wide adds provenance. Template tables show `TITLE`, `API_NAME`, `KIND`, and `READ-ONLY`; wide adds provenance. The column label distinguishes the opaque server address from the title users supply. Mutation receipts use the returned name as the API identity and include a separate title detail, with old and new titles when update renames an object. The imported marker is presentation only; no invented field is inserted into JSON/YAML.

### D3. Update the explicit subject; push the desired identity

The command argument and the manifest have different jobs: `update <current-title>` identifies an existing object; its spec describes the desired result. Generic push has only the desired result and no explicit old subject. Keeping this distinction removes the need to infer rename intent from encoded names or to introduce a GCX-specific manifest schema.

**Dedicated update allows an explicit rename.** Accept only Grafana-managed manifests and resolve the argument against the current Grafana-managed title, independently of the desired spec title. An imported manifest is rejected as read-only; a Grafana manifest whose argument title exists only as an import gets not found. Require any supplied `metadata.name` to equal the resolved object's API name; if omitted, fill it from the resolved object. Keep the caller's resource version; never substitute the just-read current version. Send one update to the resolved current API name, retaining that name in the request while allowing the spec title to change. Template kind remains unchanged, and imported groups remain read-only. Preserve stale-version, provenance, and destination-title conflict errors; do not turn a missing update subject into create or overwrite another object.

Both native backends support rename and return the new derived API name. The tagged Grafana 13.2 [time interval service](https://github.com/grafana/grafana/blob/v13.2.0/pkg/services/ngalert/provisioning/mute_timings.go) also updates its dependent notification-policy and rule notification references, subject to provenance checks; the [template service](https://github.com/grafana/grafana/blob/v13.2.0/pkg/services/ngalert/provisioning/templates.go) rejects kind changes and removes the old entry on title change. These are source-confirmed backend behaviors, not a client guarantee for every server version. GCX forwards the operation and reports its outcome; it neither recreates server reference logic nor rewrites local files. Runtime compatibility is verified during implementation.

**Generic push matches the desired identity.** The title/kind in the manifest defines the desired identity. A supplied metadata name is only a lookup hint: use a GET match only if its logical identity agrees; otherwise list-match the desired identity. An edited title creates or updates the new identity and leaves the old object untouched. The same file remains repeatable without rewriting its old metadata name. Historical title-only and current kind-qualified template names overlap, so an old encoded name cannot reliably establish source encoding or rename intent.

UX, using a manifest fetched for `weekends` and retaining its version:

```sh
# After editing spec.name to holidays, rename the explicit existing subject.
gcx alert time-intervals update weekends -f interval.yaml

# With the same desired title in a resource directory, ensure holidays exists.
# Push does not remove or rename weekends if it is present on that target.
gcx resources push -p ./resources
```

Help states: "The argument selects the current title; the manifest supplies the desired title." A rename receipt shows the old and new titles and the returned API name. To continue managing a renamed object locally, fetch its new title or pull again; local-file synchronization is explicit. Clearing source metadata for generic push is optional and never means rename or prune.

**Alternatives considered:** rejecting all dedicated renames would avoid a capability the API already supports despite the caller supplying an unambiguous old subject. Inferring rename during push would need an explicit old identity or persisted client state, because a portable desired manifest alone cannot distinguish rename from creating another object. Neither is needed for this slice. The explicit update subject supplies rename intent while declarative push remains portable and repeatable.

### D4. Generic server-name policy and push integration

Add a small pure policy in `internal/resources/`, selected by group/kind independent of API version. It describes logical identity extraction, read-only classification where relevant, and create preparation. It does not compute or validate an encoded name. Initially opt in only TimeInterval and TemplateGroup. A later receiver policy can use the same create preparation and matching path; receivers remain hidden now. Avoid a new plugin registry, callback-rich descriptor type, or native adapter registration.

Reuse `remote.findByNaturalKey` and `naturalKeyCache`: consult the native policy when no adapter extractor exists, match full logical identity, and reject ambiguity for the opted-in native kinds. Ignore remote items whose identities cannot be parsed, logging at debug, while retaining strict local validation. Preserve existing adapter matching behavior. Do not store per-operation mutable state in the policy.

Before push, collection keys for unnamed opted-in resources use GVK, namespace,
and the same full logical identity, with a separate tag from API-name keys.
Named resources and non-opted-in kinds retain existing collection behavior.
Equivalent valid unnamed identities still deduplicate; different titles or
template kinds remain distinct. Invalid unnamed identities use an in-memory
per-resource key so each reaches push validation and failure aggregation rather
than silently replacing another input. This fallback is not a portable identity.

The push sequence for an opted-in kind is:

1. Validate title/kind; count read-only local input as a read-only skip before lookup or mutation.
2. GET a nonempty supplied metadata name. Any error except not found stops the operation.
3. If found with the desired logical identity, fail before a write if the destination is read-only; otherwise update a copy with its destination name/version. Never send changed title/kind to that object's update endpoint.
4. If unnamed/not found, or the name resolves to a different logical identity, use the cached destination list to match the desired full identity. A unique read-only destination match fails before a write with source-management/rename guidance; a writable match is updated with its destination name/version; multiple matches fail. The unrelated name match is not mutated.
5. If no match exists, deep-copy and clear `metadata.name`, `generateName`, `resourceVersion`, `uid`, `generation`, timestamps/deletion fields, and `managedFields` before a CREATE operation. Preserve spec, labels, annotations/provenance, and the namespace supplied by the existing destination processor. Do not mutate the original local manifest.

Matching before create is necessary even after the immediate 400 fix: a 12.0–12.3 source name differs from a newer target's name, so merely clearing the name on each miss would conflict on the next push. Name-less authored manifests also need a logical match to remain upserts. Do not infer API absence from an object 404, retry a failed mutation as another verb, or retry through provisioning.

### D5. Complete reads and shared read-only handling

`IsReadOnlyNative` applies to TimeInterval and TemplateGroup: `spec.kind: mimir`,
or provenance `system`, `converted_prometheus`, or `file`. Absent provenance and
`api` remain writable. List/get, including generic get, preserve all returned
objects. Generic pull retains read-only objects by default; `--skip-read-only` excludes
them and counts them as skipped. Push/delete always skip them,
and show a read-only reason. Pull writes no artifact for excluded objects. The shared fetch
path sets `ExcludeReadOnly` from `--skip-read-only`; delete selectors always
exclude them, while get/edit fetch them for display or refusal.
Unknown server kinds alone are not classified as read-only and remain pullable.

Dedicated mutations reject read-only input or targets. Generic edit fetches the
object but refuses it before opening the editor. Force bypasses confirmation,
not read-only classification. A writable local interval matching a read-only
destination fails before update through either the GET or cached-list match;
it is not skipped, replaced, or retried as create.

Generic file deletion first skips read-only objects, then rejects unnamed writable
TimeInterval/TemplateGroup files before any request for that file. The error
points to dedicated title-based delete or a pulled manifest with its actual API
name. Other files continue under the existing batch error policy.

Push/delete failures add optional `source` from `Resource.SourcePath()`, omitted
when unavailable, to `gcx.mutation_batch` schema version 1. Target name continues
to mean the actual API address. Batch mutations and artifact receipts may include
an optional top-level `skipped_reasons` map of reason to count, omitted when
empty. `summary.skipped` remains a total; human summaries render counts for each
reason, including read-only, not server-verified, and not listable. JSON and
agent-mode output expose the same reason counts.

Read-only create/update errors are ordinary errors with exit code 1, not usage
errors with exit code 2. Their guidance uses plain descriptions: system-provided
objects are built into Grafana, imported objects are managed in their external
Alertmanager configuration or completed in Grafana, and file-provisioned
objects are changed in the provisioning file.

### D6. Expose only reviewed kinds and preserve the dry-run guard

Expand `partiallyExposedGroups` to routingtrees/timeintervals/templategroups, preserving the group's preferred-version discovery entry. Amend the existing routing-tree spec's discovery requirement rather than leaving a contradictory promise that all other notification kinds remain hidden.

Neither kind enters the trusted server-dry-run allowlist. Add create/update/delete regression coverage for both versions using a recording transport that would apply writes if the guard forwarded them. Assert zero mutation requests and skipped/unverified output, including after name normalization and logical matching. The existing explicit `--assume-server-dry-run` override keeps its opt-in semantics; do not silently add it to tests or normal operation. Dedicated CRUD has no new dry-run flag in this slice.

### D7. Provisioning export is a separate output contract

Provisioning export remains on `gcx alert mute-timings export --name <title> --format yaml|json|hcl`. The native time-interval family directs users there rather than registering a second command for the same endpoint. Native template groups also have no export leaf. Provisioning export covers Grafana-managed intervals and is not a native manifest format; imported intervals visible in native reads may be excluded.

### D8. Native Conflict retry remains inside the dry-run guard

Wrap TimeInterval/TemplateGroup dynamic-client Create/Update/Delete in bounded backoff matching only
`apierrors.IsConflict`. Other resource kinds and reads pass through unchanged. `AlreadyExists` and other
errors are returned immediately; exhausted attempts return the last conflict
unchanged. Use context-aware waits and debug attempt logging. The retry wrapper
sits inside `newGuardedDynamicClient`, before router construction, so blocked
previews make no attempt and provider adapters are unaffected. Existing bounded
concurrency remains; no alerting-specific serial phase or error-text matching is
introduced. Retries keep the same operation/body, without a version refresh or
fallback to another verb/backend. A stale resource version can still exhaust
backoff. Recording contention tests cover concurrent pushes and deletes.

### D9. Verification and documentation

Table-driven command tests use `native.Fixed`/`native.Func` and request-recording HTTP servers. Cover title matching, duplicate titles, historical absent kind/encoded name, create conflicts, version selection, stdin, stale/missing-version update, explicit old-title rename with the old API name and supplied version, rename destination conflicts, retained-name mismatches, unchanged template kind, read-only imports, confirmation, empty results, and preserved 401/403/404/409/5xx/cancellation errors. Verify one selected mutation request and no fallback.

Core tests cover copies, name preparation for both versions, unchanged caller-selected names, source vs destination versions, logical matching, historical-to-new encoding repeated pushes, unnamed upsert, changed-title new identity, historical/current encoded-name collisions, ambiguity, and dry-run nonmutation. Test behavior through the pusher/real commands rather than mirroring helper code.

After authorization, run the required gate then full gate serially, maintain docs/reference and the doc-maintenance checks, and execute the E2E sequence in `tasks.md` against local compose and the authorized throwaway context. Use isolated scratch config and task-owned resources. Record each command, exit, key result, and cleanup in the PR description, as #1472 did. Public text uses generic context/resource placeholders and no credentials or deployment identifiers. PR wording references #1509 and says **Part of #1473**, without a closing keyword on the umbrella.

## Risks / Trade-offs

- **Identity changed within `v0alpha1`, and encoded names overlap.** → Match logical identity, use metadata name only as a hint in push, and let the server derive names. Test the collision between historical `grafana|foo` and newer `foo` rather than accepting two formulas as a rename detector.
- **Title lookup requires list permission and collection I/O.** → This follows the settled UX. Keep full pagination; disclose permission errors and use the existing spill/field-selection codecs for large inventories.
- **List-to-write races remain possible.** → Keep caller versions for imperative updates and server conflict/not-found responses. Generic native mutations retry only Conflict with bounded backoff; stale versions can still fail. Never convert update into create.
- **Imported groups can collide with editable titles.** → Kind-qualified matching and explicit ambiguity errors prevent selecting the wrong object.
- **Cross-version omitted-kind writes are not live-verified.** → Reads/matching tolerate historical absence; test native defaulting and cross-version payloads before claiming runtime compatibility. Preserve server validation if a target rejects a manifest; no second-backend retry.
- **Push cannot infer rename from a desired manifest; local files remain unchanged after dedicated rename.** → Document update's explicit current-title subject, show old/new titles in its receipt, and fetch/pull again after rename. Push ensures desired identities exist and never prunes omitted identities. Surface backend reference/provenance restrictions rather than promising a client-managed migration.
- **Server dryRun can apply real changes.** → Default-deny client-side guard and recording-transport tests; never count skipped/unverified preview as server validation.

## Migration Plan

This is additive command exposure plus a narrowly opted-in push correction. Prepare public-safe artifacts locally, obtain the requested implementation go-ahead, apply one change, complete unit/gate/E2E verification, then publish one PR under the existing authorization policy. Do not commit copied OpenSpec setup skills. Existing provisioning commands remain available. Rollback is a revert of the new commands, allowlist entries, and opted-in policy; resources created during verification are cleaned up explicitly.
