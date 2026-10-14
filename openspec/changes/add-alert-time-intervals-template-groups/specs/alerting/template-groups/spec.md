# Spec Delta

## Purpose

Lets users manage Grafana notification template groups as native resources and inspect imported groups without confusing read-only imports with editable Grafana groups.

## ADDED Requirements

### Requirement: List all template groups with their identities
`gcx alert template-groups list` SHALL enumerate all returned groups, including `spec.kind: mimir` imports. Human tables SHALL show title, encoded `metadata.name`, kind, and shared read-only status based on `spec.kind: mimir` or provenance `system`, `converted_prometheus`, or `file`. Unknown server kinds SHALL render their raw kind with unknown read-only status without failing list. JSON/YAML SHALL preserve native manifests without adding presentation fields. Absent historical `spec.kind` SHALL mean Grafana-managed for matching and presentation.

#### Scenario: Editable and imported groups
- **WHEN** the server returns a Grafana group and a Mimir-imported group
- **THEN** list includes both and human output marks the imported group read-only

#### Scenario: Historical manifest
- **WHEN** the server returns a group with title/content and no `spec.kind`
- **THEN** it remains readable and is displayed as Grafana-managed without modifying the structured manifest

#### Scenario: Empty collection
- **WHEN** no groups exist and `list -o json` runs
- **THEN** the native list contains `items: []`

#### Scenario: Built-in or provisioned group
- **WHEN** native list returns a group with system, converted-prometheus, or file provenance
- **THEN** it remains readable and is marked read-only, and dedicated mutations reject it before a write

### Requirement: Resolve template groups by exact title
`gcx alert template-groups get <title>` SHALL match `spec.title` exactly and return the native object unchanged. An optional `--kind grafana|mimir` SHALL disambiguate equal titles for get and delete. Zero matches SHALL fail as not found; multiple matches SHALL fail with actionable ambiguity guidance and candidate kinds/names. A known-kind filter SHALL ignore unknown server kinds; unfiltered title lookup SHALL include them as candidates with their raw kind.

#### Scenario: Equal titles
- **WHEN** Grafana and imported groups share `spec.title: shared` and get supplies no kind
- **THEN** it fails without arbitrarily choosing a group and advises using `--kind`

#### Scenario: Imported group fetch
- **WHEN** `get shared --kind mimir -o yaml` selects an imported group
- **THEN** it returns its native read-only manifest, including concurrency metadata

#### Scenario: Unknown kind lookup
- **WHEN** a future-kind server group shares a title with a Grafana-managed group
- **THEN** filtered get resolves the known kind while unfiltered lookup preserves the ambiguity and reports the raw candidate kind

### Requirement: Create an editable template group
`gcx alert template-groups create -f <file>` SHALL create a Grafana-managed group from a native manifest and SHALL NOT update an existing group. Create SHALL omit source server identity and concurrency metadata and report both title and the server's derived name. A read-only manifest SHALL fail as a command error with exit code 1, not a usage error with exit code 2, and without a mutation. Local unknown kinds SHALL fail validation.

#### Scenario: New group
- **WHEN** create receives a valid title/content manifest for a Grafana group absent on the target
- **THEN** it sends an unnamed create and reports the created identity

#### Scenario: Existing logical identity
- **WHEN** create receives the title of an existing Grafana group
- **THEN** it reports a conflict and does not overwrite the group

#### Scenario: Attempted import creation
- **WHEN** create receives `spec.kind: mimir`
- **THEN** it fails clearly as read-only before a mutation request

### Requirement: Update only the selected editable template group
`gcx alert template-groups update <current-title> -f <file>` SHALL accept only a Grafana-managed manifest and require a fetched resource version. The argument SHALL select the current Grafana-managed group by title; it SHALL NOT select an imported group based on the manifest. An absent historical `spec.kind` SHALL mean Grafana-managed. `spec.title` SHALL supply the desired title and MAY differ from the argument for an explicit rename. A supplied metadata name SHALL equal the resolved current group's API name; if absent, the resolved name SHALL be used. Update SHALL send one mutation with the resolved current API name and caller version, preserve server conflicts, and fail if missing, stale, ambiguous, or read-only. Template kind SHALL remain unchanged. A rename receipt SHALL show old and new titles and the returned API name; GCX SHALL NOT rewrite local files.

#### Scenario: Edit content
- **WHEN** a Grafana group's content is edited and updated with its current version
- **THEN** only that group changes and the receipt includes title and encoded name

#### Scenario: Invalid input
- **WHEN** a version is absent or a supplied name differs from the resolved current group's API name
- **THEN** update fails with usage status and no mutation

#### Scenario: Explicit title rename
- **WHEN** a current Grafana-managed manifest's title is edited and passed to update with its old title as the argument
- **THEN** the command updates the resolved old API name using the supplied version, preserves kind, and reports the server's new title and API name

#### Scenario: Rename conflict
- **WHEN** the server rejects a desired title or provenance transition
- **THEN** the server error is preserved without another mutation or an overwrite of the conflicting group

#### Scenario: Attempted kind change
- **WHEN** the manifest's changed kind conflicts with the group identified by its retained API name
- **THEN** update fails without mutation rather than converting an imported group into an editable group or vice versa

#### Scenario: Stale update
- **WHEN** the supplied version is stale
- **THEN** the server's conflict is retained and guidance explains how to fetch and retry

#### Scenario: Imported update
- **WHEN** the update manifest itself is read-only, including an imported Mimir manifest
- **THEN** it fails as read-only with exit code 1 and sends no PUT

#### Scenario: Imported title without a Grafana group
- **WHEN** a Grafana-managed manifest is passed to update and the argument title exists only on an imported group
- **THEN** title resolution returns not found instead of treating the imported group as a read-only update target

### Requirement: Delete only an editable template group after confirmation
`gcx alert template-groups delete <title>` SHALL remove the uniquely selected editable group after confirmation or force. Groups classified as read-only by kind or provenance SHALL fail as read-only, including when force is supplied. A successful receipt SHALL include title and encoded name.

#### Scenario: Writable deletion
- **WHEN** `delete shared --kind grafana --force` selects an existing Grafana group
- **THEN** that group is removed while an imported group with the same title remains

#### Scenario: Imported deletion
- **WHEN** `delete shared --kind mimir --force` runs
- **THEN** it fails as read-only without sending DELETE

#### Scenario: Confirmation not granted
- **WHEN** confirmation is declined or unavailable without force/auto-approval
- **THEN** no delete is sent and the command follows the standard cancelled/force-guidance contract

### Requirement: Manifest and version interoperability
Create/update SHALL accept native JSON/YAML and `-f -`, validate group/kind/title and API-version agreement before I/O, and select the manifest version. Other native operations SHALL use discovery or explicit `--api-version`. Dedicated and generic reads SHALL return the same object. Existing `templates` commands SHALL be unchanged; no template-group export command SHALL be added.

#### Scenario: Pipe and generic round trips
- **WHEN** a current get manifest is piped to update, or compared with generic get by encoded name and the same version
- **THEN** the pipe succeeds and the two read surfaces agree

#### Scenario: Wrong or explicitly empty input
- **WHEN** the group/kind is wrong, title is empty, version/filename/kind flag is explicitly empty, or flag and manifest versions disagree
- **THEN** it fails with usage status before network I/O

#### Scenario: No provisioning export
- **WHEN** the template-groups help tree is inspected
- **THEN** it lists the five native CRUD verbs and no export verb
