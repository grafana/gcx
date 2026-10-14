# Spec Delta

## Purpose

Lets users manage named Grafana notification time intervals using native manifests, with the same data available through dedicated commands and generic resource workflows.

## ADDED Requirements

### Requirement: List and fetch time intervals by title
`gcx alert time-intervals list` SHALL enumerate all time intervals. `get <title>` SHALL resolve an exact, case-sensitive `spec.name` match and return its native manifest unchanged. Human tables SHALL show the title, `metadata.name`, and read-only status. The shared rule SHALL classify `spec.kind: mimir` or provenance `system`, `converted_prometheus`, or `file` as read-only, leaving absent provenance and `api` writable. Title arguments SHALL NOT be interpreted as encoded resource names.

#### Scenario: Complete structured enumeration
- **WHEN** `list -o json` runs with zero or more time intervals
- **THEN** it returns a native list containing all items, with an empty `items` array when none exist

#### Scenario: Title lookup
- **WHEN** `get weekends -o yaml` runs and `spec.name: weekends` exists
- **THEN** it returns the server's `TimeInterval` manifest, including the encoded name, resource version, and annotations

#### Scenario: No exact match
- **WHEN** `get weekends` runs and only `Weekends` exists
- **THEN** it exits non-zero with a not-found error for the requested title

#### Scenario: Native imported interval remains readable
- **WHEN** native list/get returns an interval with converted-prometheus provenance
- **THEN** reads retain it, its table status is read-only, and dedicated mutations refuse it before a write

### Requirement: Create a time interval without upsert
`gcx alert time-intervals create -f <file>` SHALL create the native `TimeInterval` manifest as a new resource. A title already present SHALL fail without updating it. Read-only manifests SHALL fail before a write. The request SHALL omit source server identity and concurrency metadata; the result SHALL report the title and the name returned by the server.

#### Scenario: Fresh manifest
- **WHEN** create receives a valid manifest with `spec.name: maintenance` and empty `metadata.name`
- **THEN** the server derives its name and the command reports a created resource

#### Scenario: Manifest fetched from another context
- **WHEN** create receives a manifest with a valid source name and resource version, and its title is absent on the target
- **THEN** it creates using empty name and no source concurrency fields, leaving the input unchanged

#### Scenario: Existing title
- **WHEN** create receives a title that already exists
- **THEN** it exits non-zero with a conflict and does not update that resource

### Requirement: Update a time interval with optimistic concurrency
`gcx alert time-intervals update <current-title> -f <file>` SHALL update only the existing interval identified by the argument. The manifest SHALL retain its fetched `metadata.resourceVersion`; `spec.name` SHALL supply the desired title and MAY differ from the argument for an explicit rename. A supplied metadata name SHALL equal the resolved current interval's API name; if absent, the resolved name SHALL be used. The command SHALL send one update using the current API name and the supplied version, and SHALL report the returned title/name and both old and new titles on rename. Read-only input or destination intervals SHALL fail before a write. GCX SHALL NOT rewrite local files or implement server reference migration.

#### Scenario: Current manifest update
- **WHEN** a fetched interval is edited without changing its title and updated with its current version
- **THEN** that interval reflects the edit and other intervals are unchanged

#### Scenario: Missing version
- **WHEN** the manifest has no resource version
- **THEN** the command exits with a usage error before sending a request

#### Scenario: Explicit rename
- **WHEN** `update weekends -f interval.yaml` receives a current manifest for weekends with `spec.name` edited to holidays
- **THEN** it updates the resolved weekends API name with the supplied version and desired title, and reports the server's returned holidays identity without rewriting the file

#### Scenario: Rename conflict
- **WHEN** an explicit rename is rejected by the server because the desired title exists or a reference/provenance restriction applies
- **THEN** the command preserves the server error without attempting create, delete, or a second mutation

#### Scenario: Retained name identifies another title
- **WHEN** both the argument and spec.name are changed but metadata.name still identifies the old title
- **THEN** update fails with a usage error and sends no mutation, even if the requested new title already exists

#### Scenario: Stale version
- **WHEN** the interval changed after the supplied manifest was fetched
- **THEN** update reports the conflict, retains the server error, and advises re-fetching before retrying

#### Scenario: Missing resource
- **WHEN** update names a title absent on the target
- **THEN** it fails as not found without creating a replacement

### Requirement: Delete a time interval after confirmation
`gcx alert time-intervals delete <title>` SHALL resolve the title and delete its native resource after confirmation or explicit force. Read-only intervals SHALL fail before a write even with force. Its receipt SHALL include the title and encoded resource name.

#### Scenario: Forced deletion
- **WHEN** `delete maintenance --force` targets an existing interval
- **THEN** it is removed and the result reports deletion

#### Scenario: Declined or unavailable confirmation
- **WHEN** confirmation is declined, or agent mode/closed stdin supplies neither force nor auto-approval
- **THEN** no mutation occurs, with cancelled status for a declined prompt and actionable force guidance when confirmation is unavailable

### Requirement: Native manifest and API version contract
Create and update SHALL accept JSON/YAML files and injected stdin via `-f -`. Manifests SHALL have the notifications group, `kind: TimeInterval`, and a nonempty string `spec.name`. Read/delete SHALL select the discovered preferred version unless `--api-version` is explicit; writes SHALL use the manifest version and reject a conflicting flag before I/O.

#### Scenario: Pipe round trip
- **WHEN** `get weekends -o yaml | update weekends -f -` runs
- **THEN** update consumes stdin and preserves optimistic concurrency

#### Scenario: Invalid input
- **WHEN** the input has a wrong group/kind, empty title, explicitly empty filename/version flag, or conflicting API version
- **THEN** it exits with a usage error before network I/O

#### Scenario: Older native version
- **WHEN** the server advertises only the supported `v0alpha1` resource
- **THEN** default reads use that version without a provisioning fallback

### Requirement: Keep provisioning export on its existing command
The native time-interval family SHALL NOT add an export leaf. Its help SHALL direct users to `gcx alert mute-timings export --name <title> --format yaml|json|hcl` for provisioning bytes, preserving that command's behavior. Provisioning export covers Grafana-managed intervals; imported intervals visible in native reads are excluded.

#### Scenario: Provisioning export guidance
- **WHEN** time-intervals help is displayed
- **THEN** it identifies the existing mute-timings export command and distinguishes provisioning output from native manifests

#### Scenario: Native export absent
- **WHEN** a caller attempts time-intervals export
- **THEN** no such native command is registered, while mute-timings export remains available

### Requirement: Same resource on both command surfaces
Dedicated JSON/YAML reads and `gcx resources` SHALL exchange the same native `TimeInterval` manifests. The existing `mute-timings` commands SHALL retain their behavior.

#### Scenario: Generic get agrees
- **WHEN** the dedicated title lookup and generic get using its returned `metadata.name` read the same interval with the same API version
- **THEN** both return the same native object

#### Scenario: Generic manifest round trip
- **WHEN** a dedicated get manifest is pushed, or a generic manifest is updated through the dedicated command with a fresh version
- **THEN** both paths operate on the same interval without payload conversion
