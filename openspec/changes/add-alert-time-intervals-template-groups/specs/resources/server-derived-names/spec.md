# Spec Delta

## Purpose

Makes native resources whose names Grafana derives portable across contexts while preserving idempotent push, explicit identity changes, and nonmutating dry-run behavior.

## ADDED Requirements

### Requirement: Create resources with server-derived names
For native kinds whose names are server-derived, `gcx resources push` SHALL omit `metadata.name` and source concurrency/identity fields on CREATE, while preserving the spec, labels, annotations, and destination namespace. The original manifest SHALL remain unchanged. Kinds with caller-selected names SHALL retain their existing behavior.

#### Scenario: New resource on another context
- **WHEN** a pulled TimeInterval or TemplateGroup is pushed to a context without that logical identity
- **THEN** one unnamed create succeeds and the server returns its derived identity

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

### Requirement: Surface read-only and transport failures
Push SHALL reject imported `spec.kind: mimir` template groups as read-only without sending a mutation. Lookup failures other than not found SHALL be surfaced without falling through to create or retrying through another API. Per-resource failures SHALL follow existing push aggregation and `--on-error` behavior.

#### Scenario: Imported manifest push
- **WHEN** push receives a Mimir-imported template group
- **THEN** that resource fails as read-only and no POST/PUT is sent

#### Scenario: Forbidden lookup
- **WHEN** the destination lookup returns forbidden or a transport failure
- **THEN** its error is preserved and no create fallback occurs

### Requirement: Default preview never mutates alerting resources
`resources push --dry-run` and `resources delete --dry-run` for time intervals and template groups SHALL use client-side preview by default, with no POST, PUT, PATCH, or DELETE sent. Output SHALL explain unverified validation and report skipped/unverified rather than confirmed application. The existing explicit server-dry-run assertion remains an opt-in override.

#### Scenario: Preview create or update
- **WHEN** a valid new or existing manifest is pushed with dry-run and no server-dry-run assertion
- **THEN** no mutation is sent and the unverified/skipped result states the client-side validation limits

#### Scenario: Preview delete
- **WHEN** an existing time interval or template group is deleted with dry-run and no assertion
- **THEN** existence may be read but the delete is not sent and the resource remains unchanged
