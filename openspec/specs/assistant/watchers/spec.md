# assistant/watchers Specification

## Purpose

Allow users and agents to inspect Assistant Watcher configuration and current runtime observations, and export safe configuration manifests through gcx.

## Requirements

### Requirement: Experimental read surface
The CLI SHALL expose experimental `assistant watchers list`, `get WATCHER` and `status WATCHER` commands and generic `resources get/pull watchers[/WATCHER]` access. All dedicated subtree nodes MUST disclose experimental stability in help and agent metadata.

#### Scenario: Discover the read commands
- **WHEN** a user inspects help or the command catalog
- **THEN** every Watcher node is marked experimental and describes configuration reads versus runtime status
- **AND** list accepts no positional arguments, while get and status require one nonempty resource name or server ID

#### Scenario: Discover a registered kind in a different version
- **WHEN** native discovery prefers a group version that lacks Watcher while the provider registers Watcher in another version
- **THEN** unversioned Watcher selectors and preferred-resource enumeration resolve the registered descriptor
- **AND** native kinds retain their preferred versions and explicit version requests remain exact

### Requirement: Configuration manifest contract
The Watcher manifest SHALL use `assistant.ext.grafana.app/v1alpha1`, kind `Watcher`, and a strict configuration-only spec covering every field in RFC 002's manifest example. The secret-input union MUST be fixed in this slice. Later writes MUST consume this same pulled format.

#### Scenario: Inspect a fully configured Watcher
- **WHEN** a user requests a manifest
- **THEN** its schema supports title, description, prompt, datasourceUids, interval, sensitivity, skipReviewOnCleanRuns, autoStop, labels, automaticRecalibration, notifications and investigation
- **AND** nested fields include all RFC stop settings, notification enablement/channel/severity/secret settings and investigation team access
- **AND** supported effective configuration is materialized without lifecycle, calibrated checks, learned state, usage, history or audit fields in spec

#### Scenario: Validate the final schema
- **WHEN** a manifest contains unknown fields, invalid enums, blank required strings, invalid duration/timestamp input or invalid enabled-block dependencies
- **THEN** client-side validation reports the offending nonsecret input and its expected form
- **AND** validation performs no mutation or secret-source resolution

#### Scenario: Cannot clear an enabled webhook URL
- **WHEN** a manifest enables webhook delivery and sets its URL secret input to clear true
- **THEN** validation rejects the conflicting configuration before any I/O

### Requirement: Secret input and export safety
Secret input SHALL accept exactly one nonempty fromEnv, nonempty fromFile, preserve true or clear true. Reads MUST export only preserve true for confirmed configured secrets, including disabled destinations, and MUST never export secret values or redacted placeholders.

#### Scenario: Export configured webhook secrets
- **WHEN** the server confirms a configured webhook URL, bearer token or signing secret
- **THEN** the corresponding manifest field contains only `preserve: true`
- **AND** no cleartext, display placeholder or reference invented by gcx appears in dedicated output or pulled files

#### Scenario: No configured secret
- **WHEN** the server confirms that a secret is absent
- **THEN** no preserve marker is emitted for that secret

#### Scenario: Reject malformed secret inputs
- **WHEN** a secret object has multiple alternatives, a false marker, an empty source reference, an unknown key or a literal value
- **THEN** schema validation rejects it without displaying any secret value

### Requirement: Read completeness of modeled configuration
Successful full manifest exports MUST reflect observed modeled configuration. An inaccessible or unavailable supplementary configuration read MUST NOT be substituted with a disabled/default value or represented as a valid complete export.

#### Scenario: One discovered candidate cannot be exported
- **WHEN** collection discovery succeeds but a candidate detail or modeled configuration read fails
- **THEN** successful candidates remain available, each failed candidate is identified, dedicated list declares incomplete coverage and returns exit 4, and pull records per-candidate failures according to its error policy
- **AND** abort policy writes no files

#### Scenario: A discovered candidate disappears
- **WHEN** the candidate detail read reports not-found
- **THEN** the candidate is reported as skipped and successful candidates remain available with incomplete list coverage
- **AND** not-found from a supplementary configuration read remains an explicit failure

#### Scenario: Read automatic recalibration configuration
- **WHEN** enrollment is reported as enabled or disabled
- **THEN** the manifest reports that observed boolean

#### Scenario: Cannot establish modeled configuration
- **WHEN** a required supplementary configuration read is unavailable, denied or fails
- **THEN** manifest access reports an explicit failure and does not export a guessed setting
- **AND** independent runtime observations remain accessible through status when their reads succeed

### Requirement: Shared manifest representation
Dedicated get and generic resource get SHALL return identical JSON/YAML manifest representations for the same Watcher and context. Agent output MUST use the same configuration envelope. Output format MUST NOT alter configuration acquisition.

#### Scenario: Compare access paths
- **WHEN** a Watcher is requested through each get path with JSON or YAML
- **THEN** apiVersion, kind, metadata and spec match, including its server-ID annotation and secret-preserve markers

### Requirement: Deterministic Watcher identity
Resource names SHALL be derived deterministically from titles. The same-stack ID SHALL appear in `assistant.ext.grafana.app/watcher-id`. Dedicated and generic references MUST accept names and server IDs, with ID precedence. Name resolution MUST inspect all visible candidates across archive partitions and reject ambiguity.

#### Scenario: Resolve a unique name on a later page
- **WHEN** a named Watcher is beyond the initial collection page
- **THEN** either access path resolves the same resource

#### Scenario: Resolve an explicit server ID
- **WHEN** several Watchers share a title or slug and the user supplies one server ID
- **THEN** get, status and single-resource pull address only that Watcher, including when archived

#### Scenario: Reject name ambiguity
- **WHEN** multiple visible Watchers match one resource name, including distinct titles with the same slug or candidates in different archive partitions
- **THEN** name-addressed reads report all candidates with IDs and act on none
- **AND** list retains every candidate rather than dropping a duplicate

### Requirement: Complete collection enumeration
List SHALL exhaust pagination for the selected archive partition, represent an empty items collection as an empty array, and disclose that coverage is restricted to resources visible to the caller. Default list/bulk resource reads SHALL select non-archived Watchers; list --archived SHALL select archived Watchers only.

#### Scenario: Enumerate multiple pages
- **WHEN** the collection contains several pages, including an empty page with continuation
- **THEN** enumeration continues until no continuation remains
- **AND** the result identifies its archive partition and caller-visible coverage without claiming a total inventory or atomic snapshot

#### Scenario: Fail incomplete enumeration
- **WHEN** a page fails, a cursor repeats or the caller cancels
- **THEN** list reports the failure and never presents the accumulated prefix as a complete result

#### Scenario: Select the archived partition
- **WHEN** list is called with --archived
- **THEN** only archived visible Watchers are enumerated
- **AND** the default list selects only non-archived visible Watchers

### Requirement: Collision-safe bulk pull
Bulk pull MUST detect Watcher name collisions before collection deduplication or file writes, report all conflicting candidates with IDs, and write none of the conflicting manifests. Unaffected Watchers SHALL still be written under the default continue-and-fail policy, with failures reflected in the artifact receipt and exit status.

#### Scenario: Pull colliding and unique Watchers
- **WHEN** a default bulk pull encounters two colliding Watchers on different pages and one uniquely named Watcher
- **THEN** only the unique manifest is written, both rejected candidates are reported, and the receipt records the failures
- **AND** no existing file for the colliding name is overwritten

#### Scenario: Honor explicit error policy
- **WHEN** the user chooses an existing abort or ignore error policy
- **THEN** pull follows that policy while still refusing to export conflicting candidates

#### Scenario: Collision spans archive partitions
- **WHEN** a non-archived Watcher selected for bulk pull shares a resource name with an archived visible Watcher
- **THEN** pull reports both candidates with IDs and does not write the selected conflicting manifest
- **AND** unrelated selected resources are still written without exporting archived resources

### Requirement: Observed runtime status
Status SHALL separate lifecycle from assessment and calibration, report available run times, usage estimates with sample context, audit metadata, current version, calibration context and checks, and omit or mark unknown unavailable observations. Reads MUST NOT start monitoring, request calibration or execute a run.

#### Scenario: Paused Watcher with an old warning
- **WHEN** a paused Watcher retains a warning assessment
- **THEN** status reports paused lifecycle and the separate warning without implying current monitoring or a new assessment

#### Scenario: Missing observations
- **WHEN** completion time, assessment observation time or check title is unavailable
- **THEN** status omits or marks it unknown rather than deriving it from unrelated timestamps or text
- **AND** missing usage is not reported as zero consumption

#### Scenario: Calibration needs recovery
- **WHEN** calibration reports failed or needs input
- **THEN** status reports the observed state and guidance to continue in Grafana without suggesting activation

#### Scenario: Calibration sub-read is unavailable or denied
- **WHEN** the Watcher read succeeds but calibration progress cannot be read
- **THEN** status retains independent observations and explicitly distinguishes unavailable progress, denied access and failed reads from idle or completed calibration

### Requirement: Unsupported checks remain visible
Status MUST retain observable check identity, enablement, source, query type, expression, interpretation guidance and type-specific parameter information. Unknown query types or parameters MUST be marked unsupported without being silently discarded or added to spec.

#### Scenario: New query type or parameter
- **WHEN** a calibrated check contains a query type or parameter gcx does not recognize
- **THEN** status identifies the unsupported information and retains its inspectable content
- **AND** get/pull manifests still contain configuration only

### Requirement: Distinct read outcomes
Read commands MUST distinguish missing capability, denied access, absent Watchers and a successful empty collection. Errors MUST retain meaningful classification and recovery guidance across dedicated and generic paths.

#### Scenario: Capability is missing
- **WHEN** collection capability is unavailable on the selected target
- **THEN** the command reports unavailable capability rather than an empty collection, an absent individual Watcher or a silent successful pull

#### Scenario: Access is denied
- **WHEN** permission checks deny a read
- **THEN** the command reports permission failure without revealing inaccessible candidates

#### Scenario: No visible Watchers
- **WHEN** enumeration succeeds with zero visible resources
- **THEN** list succeeds with an empty items array and pull succeeds with a zero-file receipt

#### Scenario: Requested Watcher does not exist
- **WHEN** capability is available but the requested visible ID or name is absent
- **THEN** get/status report not-found rather than capability unavailable

### Requirement: Writes remain unsupported
Generic resources push and delete MUST reject Watcher mutation with an actionable not-supported-yet error. Create, update and delete support MUST remain absent, and no write request SHALL be sent.

#### Scenario: Attempt generic mutations
- **WHEN** a user pushes a valid new or existing Watcher manifest or requests Watcher deletion
- **THEN** the operation fails clearly as not supported yet and sends no mutation request
