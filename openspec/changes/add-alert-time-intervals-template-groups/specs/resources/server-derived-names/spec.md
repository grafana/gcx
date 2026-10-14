# Spec Delta

## Purpose

Makes native resources whose names Grafana derives portable across contexts while preserving idempotent push, explicit identity changes, and nonmutating dry-run behavior.

## ADDED Requirements

### Requirement: Create resources with server-derived names
For native kinds whose names are server-derived, `gcx resources push` SHALL omit `metadata.name` and source concurrency/identity fields on CREATE, while preserving the spec, labels, annotations, and destination namespace. The original manifest SHALL remain unchanged. Kinds with caller-selected names SHALL retain their existing behavior.

#### Scenario: New resource on another context
- **WHEN** a pulled TimeInterval or TemplateGroup is pushed to a context without that logical identity
- **THEN** an unnamed create operation succeeds and the server returns its derived identity

#### Scenario: Manifest from get
- **WHEN** a manifest containing source name, UID, resource version, and timestamps is pushed to an empty target
- **THEN** the create body contains no source concurrency/identity fields and the local input is unchanged

#### Scenario: Caller-selected name
- **WHEN** an ordinary resource such as a dashboard is created by push
- **THEN** its configured name is still sent

### Requirement: Upsert by destination identity and version
Push SHALL use the manifest's logical identity, using metadata.name only as a lookup hint. A matching identity SHALL update the destination name with its current version; a name pointing to another identity SHALL fall through to logical matching without modifying that object. TimeInterval identity SHALL be its name; TemplateGroup identity SHALL be kind plus title, with absent historical kind meaning Grafana-managed. Multiple matches during list-based resolution SHALL fail without mutation.

#### Scenario: Repeat push
- **WHEN** the same time interval or template group manifest is pushed twice
- **THEN** the first push creates if necessary and the second updates the same destination object without duplication

#### Scenario: Historical template encoding
- **WHEN** a title-only encoded TemplateGroup manifest is pushed to a target using kind-plus-title encoding
- **THEN** an existing matching group is updated using its destination name/version, including on repeated pushes

#### Scenario: Unnamed authoring manifest
- **WHEN** an unnamed manifest matches an existing logical identity
- **THEN** push updates that identity rather than attempting a conflicting create

#### Scenario: Imported and editable titles collide
- **WHEN** an editable and an imported template group share a title
- **THEN** their different kinds remain different logical identities and neither is substituted for the other

#### Scenario: Historical and current encoded names collide
- **WHEN** a historical Grafana template titled `grafana|foo` has the same metadata.name as a newer target group titled `foo`
- **THEN** push matches or creates the requested `grafana|foo` identity without updating the unrelated `foo` group

### Requirement: Treat title edits as a new declarative identity
For server-derived kinds, push SHALL create or update the identity described by the manifest's title/kind. It SHALL NOT change another object's title/kind merely because metadata.name points to it. A title edit SHALL leave the original object and its references untouched; clearing the old metadata.name SHALL NOT be required.

#### Scenario: Edited title with old name retained
- **WHEN** a pulled manifest changes its title while retaining the old derived name
- **THEN** push creates or updates the desired new identity, leaves the original unchanged, and repeats idempotently

#### Scenario: Explicit new identity
- **WHEN** the title is changed and metadata.name is explicitly removed
- **THEN** push creates or updates the new logical identity and leaves the original resource in place

### Requirement: Apply shared read-only handling without hiding reads
For TimeInterval and TemplateGroup, `spec.kind: mimir` or `grafana.com/provenance` equal to `system`, `converted_prometheus`, or `file` SHALL mean read-only. Absent provenance and `api` SHALL remain writable. Generic pull SHALL retain read-only objects by default and SHALL offer `--skip-read-only` to exclude them with counted read-only skips. Push and delete SHALL always skip them with a read-only reason. Reads SHALL retain them. Generic edit SHALL reject them before opening the editor. Dedicated create/update SHALL reject read-only manifests with exit code 1, not usage exit code 2. JSON `summary.skipped` SHALL retain its total-count shape; human summaries SHALL distinguish skip reasons. Batch mutation and artifact receipt JSON SHALL expose recorded skip reasons in an optional top-level `skipped_reasons` map of reason to count, omitted when empty and additive within `schema_version: "1"`; agent-mode JSON SHALL expose the same map.

#### Scenario: Read-only local input
- **WHEN** push or file-based delete receives a read-only native manifest
- **THEN** the resource is skipped without a client request, counted as skipped rather than failed, and other batch items retain existing processing semantics

#### Scenario: Read-only pull selector
- **WHEN** pull selects read-only native objects without `--skip-read-only`
- **THEN** it writes their manifests for backup, audit, and diffing

#### Scenario: Optional read-only exclusion
- **WHEN** pull selects read-only native objects with `--skip-read-only`
- **THEN** it counts read-only skips and writes no artifact for them

#### Scenario: Edit refusal
- **WHEN** generic edit fetches a read-only native object
- **THEN** it returns a read-only error before the editor opens

### Requirement: Preserve read-only destinations and transport failures
Push SHALL fail before update when writable local input matches a read-only destination through either GET or cached-list resolution. The failure SHALL explain the read-only source and advise source management or a different local identity. Lookup failures other than not found SHALL be surfaced without create or another API fallback. Per-resource failures SHALL follow existing aggregation and error policy.

#### Scenario: Imported interval destination
- **WHEN** a writable local TimeInterval matches a destination interval with converted-prometheus provenance
- **THEN** push fails with read-only guidance and sends no mutation, without replacing or skipping the destination match

#### Scenario: Forbidden lookup
- **WHEN** the destination lookup returns forbidden or a transport failure
- **THEN** its error is preserved and no create fallback occurs

### Requirement: Tolerate remote unknown kinds while validating local identity
Unknown or otherwise unparseable remote native identities SHALL be ignored by list-based logical matching with a debug diagnostic. Local identity validation SHALL remain strict. Unknown server template kinds alone SHALL NOT be assumed read-only and SHALL remain readable and pullable.

#### Scenario: Unknown remote template kind
- **WHEN** the destination list contains a future template kind beside a valid matching group
- **THEN** the valid match is resolved without failing the whole operation

#### Scenario: Invalid local kind
- **WHEN** a local template manifest uses an unknown kind
- **THEN** push reports its validation failure without a mutation

### Requirement: Delete requires the real API name
Generic file-based delete SHALL first skip read-only native objects, then reject unnamed writable TimeInterval and TemplateGroup manifests before any client request for that file. The failure SHALL point to dedicated title-based delete or a pulled manifest with the actual metadata name. Generic delete SHALL NOT resolve unnamed targets by title.

#### Scenario: Unnamed writable file
- **WHEN** generic delete receives an unnamed writable native manifest
- **THEN** that file fails locally with dedicated-command guidance and other batch files retain existing processing semantics

#### Scenario: Named writable file
- **WHEN** generic delete receives a writable pulled native manifest with its API name
- **THEN** deletion uses that API address

### Requirement: Failure entries retain optional local source
Push/delete mutation batch failures SHALL expose optional `source` from the resource source path, omitting it when unavailable. This additive field SHALL retain `gcx.mutation_batch` schema version 1, and SHALL NOT replace the API target name.

#### Scenario: File failure attribution
- **WHEN** a local manifest fails push or delete
- **THEN** its failure entry includes the source path when attached to the resource

#### Scenario: No source path
- **WHEN** a failed resource has no source path
- **THEN** its failure entry omits source

### Requirement: Retry native mutation Conflict responses inside the guard
The dynamic-client mutation wrapper SHALL retry Create, Update, and Delete only for TimeInterval and TemplateGroup Kubernetes Conflict responses with bounded, context-aware backoff. It SHALL pass reads and all other resource kinds through without retry, return non-Conflict errors immediately, and preserve the last conflict on exhaustion. It SHALL run inside the dry-run guard before routing, leaving blocked previews and provider adapters unaffected. Existing bounded concurrency SHALL remain without an alerting-specific serial phase, error-text matching, version refresh, or another verb/backend fallback.

#### Scenario: Transient conflict
- **WHEN** a native mutation returns Conflict and a subsequent attempt succeeds
- **THEN** the operation succeeds with the same mutation semantics

#### Scenario: Already exists or another error
- **WHEN** a native mutation returns AlreadyExists or another non-Conflict error
- **THEN** the wrapper returns that error without retrying

#### Scenario: Exhaustion or cancellation
- **WHEN** conflicts exhaust the backoff or the context is cancelled
- **THEN** the last conflict is preserved on exhaustion or retries stop promptly on cancellation

### Requirement: Default preview never mutates alerting resources
`resources push --dry-run` and `resources delete --dry-run` for time intervals and template groups SHALL use client-side preview by default, with no POST, PUT, PATCH, or DELETE sent. Output SHALL explain unverified validation and report skipped/unverified rather than confirmed application. The existing explicit server-dry-run assertion remains an opt-in override.

#### Scenario: Preview create or update
- **WHEN** a valid new or existing manifest is pushed with dry-run and no server-dry-run assertion
- **THEN** no mutation is sent and the unverified/skipped result states the client-side validation limits

#### Scenario: Preview delete
- **WHEN** an existing time interval or template group is deleted with dry-run and no assertion
- **THEN** existence may be read but the delete is not sent and the resource remains unchanged

### Requirement: Preserve distinct unnamed inputs before push
Resource collection and directory loading SHALL preserve separate unnamed TimeInterval and TemplateGroup manifests by GVK, namespace, and full logical identity. Equivalent valid identities MAY retain existing deduplication behavior. Named and non-opted-in resource collection keys SHALL remain unchanged. Invalid unnamed identities SHALL remain available for per-resource validation failures.

#### Scenario: Multiple unnamed manifests in a directory
- **WHEN** a directory contains multiple unnamed time intervals or template groups with distinct titles
- **THEN** every distinct identity reaches the pusher, including on repeated push

#### Scenario: Equal title with different template kinds
- **WHEN** unnamed grafana and mimir template groups share a title in a push directory
- **THEN** the editable group reaches normal push and the imported group independently skips as read-only without mutation

#### Scenario: Multiple malformed unnamed manifests
- **WHEN** separate unnamed opted-in inputs contain missing or invalid identity fields
- **THEN** each remains in the collection and produces its own validation failure
