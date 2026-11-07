# RFC 030: IRM provisioning and incident workflows

**Status**: proposed

## Summary

Add explicit validation and preview results for IRM configuration. Add commands for shift swap acceptance, incident tasks, hook runs, alert-group history, and access settings where the stack-local API supports them. Document the current structured output and incident export limits. Preserve existing v1 command behavior. Publish each implementation slice as a separate PR after review of this proposal and its API prerequisites.

## Motivation

An operator must know whether a manifest was checked, who a shift swap assigns, and why an alert created an incident. The current command tree leaves gaps in these steps. A generic API call can provide a diagnostic path, but it does not provide the command validation, output, pagination, or permission guidance that automation needs.

A skipped validation already has a visible count. It still exits successfully under the current [dry-run contract](../design/safety.md#34-dry-run). Scripts that use only the exit code cannot require server verification. Several dedicated IRM configuration commands have no preview. An operator must compare local input, apply it, and inspect the result.

These are separate from the confirmed input and schema defects. Bug fixes can proceed while this RFC remains proposed.

## Scope

This proposal covers the eight feature requests from the IRM provisioning review:

| Request | Proposed change |
|---|---|
| [Shift swap acceptance](https://github.com/grafana/gcx/issues/1723) | Add a dedicated acceptance action. |
| [Strict validation](https://github.com/grafana/gcx/issues/1726) | Add an explicit requirement for complete server verification. |
| [Tasks and hook runs](https://github.com/grafana/gcx/issues/1727) | Add incident-scoped commands for supported operations. |
| [Alert-group history](https://github.com/grafana/gcx/issues/1728) | Read timeline events and the related incident link. |
| [Incremental export](https://github.com/grafana/gcx/issues/1729) | Document the creation-time limit; add update-time export only after its contract is established. |
| [JSON shapes](https://github.com/grafana/gcx/issues/1731) | Document list, get, mutation, and omitted-field behavior. |
| [Configuration preview](https://github.com/grafana/gcx/issues/1732) | Add local, no-write plans with an explicit verification level. |
| [Access settings](https://github.com/grafana/gcx/issues/1733) | Expose supported team visibility and direct paging settings. |

Grafana team creation, membership, plugin sync, personal notification rules, and escalation order have separate tracking work. Regional API credential management is outside this proposal. This RFC does not authorize new undocumented routes or infer that a public regional route also exists behind the plugin proxy.

## User experience

All examples in this section are proposed commands or options. Resource identifiers are placeholders.

### Require a complete validation

An automation can opt into strict validation:

```sh
gcx resources validate -p ./manifests --require-verified -o json
```

The default remains unchanged. With `--require-verified`, any skipped resource produces exit code 4. The command still prints the normal result with separate failure and skipped counts. It must not convert a skipped resource to a successful validation. A fully verified batch exits 0. Usage and authentication errors retain their existing exit codes.

The flag changes how the result is accepted. It does not expand the server dry-run allowlist or send a write request to obtain verification. Explicit `--on-error=ignore` does not waive `--require-verified`; the two options address different conditions.

### Preview a configuration change

Add `--dry-run` to supported dedicated IRM create and update commands, including integration template updates. For example:

```sh
gcx irm oncall escalation-policies create -f policy.yaml --dry-run -o json
gcx irm oncall routes update <route-id> -f route.yaml --dry-run -o json
gcx irm oncall integrations update-templates <integration-id> -f templates.json --dry-run -o json
```

A plan identifies the resource, the intended create or update operation, the changed fields, and the verification level. A new-resource plan checks the input and available references without retrieving the nonexistent target. An update plan reads the target and compares writable fields. It does not promise that a subsequent write will succeed.

Use three verification values: `local`, `server`, and `unverified`. `local` means the input and known constraints were checked without server validation. `server` requires an established no-write validation endpoint. `unverified` means a required check could not be completed. A local preview must not claim server verification. Display incomplete checks in structured output and diagnostics.

Template plans redact fields that can contain credentials. The first slice can show changed field names without template contents. It does not claim Jinja validity unless a safe validation API is established.

Shift plans show the requested recurrence. They identify started rotations whose update may replace an ID or change the start anchor. A prospective final calendar, overlap check, or boundary warning requires a server contract that can calculate the result without a write. Until then, show the limit and require a final-calendar read after an authorized apply. Existing calendar reads do not prove what a future update will produce.

### Accept a shift swap

```sh
gcx irm oncall shift-swaps take <swap-id> --benefactor <user-id> -o json
```

The explicit user identifier keeps the assignment visible. Read the swap before the action. Reject missing identifiers, closed swaps, and invalid time ranges before a write where the read supplies those facts. Report the target and the resulting assignment. Do not present the existing update verb as acceptance.

The existing [take client](../../internal/providers/irm/oncall_client.go) provides the action method and a request type with a benefactor. Its deployed behavior and permission rules still need a contract test. Whether the current user can be the default is deferred; the initial command requires `--benefactor` rather than guessing an identity.

### Work with incident tasks and hook runs

Proposed task commands use an incident-scoped noun:

```sh
gcx irm incidents tasks list <incident-id> -o json
gcx irm incidents tasks create <incident-id> -f task.json -o json
gcx irm incidents tasks update <incident-id> <task-id> -f task.json -o json
gcx irm incidents list-hook-runs <incident-id> -o json
```

A task update changes only fields explicitly supplied in the input. Completion is a task status update. Empty text and cleared assignment need explicit input rules; an omitted field does not clear the existing value. Incident reads include a task list. Confirm that this list is complete and establish any paging behavior before using it as the task list command. Do not infer a separate task list endpoint from the method names for writes.

Hook-run reads reuse the existing [incident client](../../internal/providers/irm/incidents_client.go), which already supports the read used by `get-pir`. Expose available records without interpreting a successful hook request as proof of an external effect.

### Inspect alert-group history

```sh
gcx irm oncall alert-groups timeline <group-id> -o json
```

Return timestamps and event types in stable order. Include matched routes, escalation actions, selected users, notification attempts, and the related incident identifier only where the API supplies them. A provider send record must remain distinct from confirmed delivery. Missing data is not an empty, successful timeline.

Add an optional related incident field to the rich group read when a verified source provides it. Keep the existing reverse lookup through incident contexts. Do not perform an unbounded scan of every incident to synthesize a group link.

### Read access settings

The test stack plugin reads expose `is_sharing_resources_to_all` on teams and `manually_manage_direct_paging_integrations` on the organization. The current typed reads omit these fields. Expose them as optional booleans and preserve false values. An absent field means the endpoint did not supply the setting; it must not mean false.

After a plugin API contract is established, extend `teams update <id>` and `organizations update` with typed input files for supported settings. These commands update only named fields and read the stored result. They do not create Grafana teams or change membership.

A protected integration delete can explain which direct paging setting prevents the operation. It must not change that setting automatically. Default route protection and a route with a missing integration reference remain product questions.

### Document output and export limits

Document the current list envelope and bare get/mutation object for shared OnCall commands. Show equivalent selectors for each shape. Explain where Go zero-value omission or absent server fields affect read-back. Preserve explicit v1 JSON output; a common envelope needs an opt-in mode or a major-version change.

State that incident `--from` and `--to` currently filter creation time on the client. A modified incident can be missed if it was created before the range. The published preview query supports `modifiedTime` ordering, and previews include that timestamp. An order field alone does not establish a complete change feed. Do not add `--modified-since` or issue a durable cursor until an update-time selection and continuation contract can capture later changes to old incidents.

If that contract becomes available, use a stable boundary that includes a tie-breaker for equal timestamps. Document whether the cursor reflects a snapshot or requires an overlap window and deduplication. A create-time cursor is not an incremental change feed.

## Technical design

Keep command parsing in the existing IRM command builders. Put API operations, validation, and domain conversion in `internal/providers/irm`. Reuse the existing config loader and plugin proxy transport. Do not add a second regional authentication path for these features.

Adapter-backed resources continue through the shared typed CRUD path. Task actions, hook runs, and timelines can remain provider-only where they do not belong in the push/pull resource model. They do not need an adapter merely to expose a command.

Strict validation extends the final result decision in [the resources validate command](../../cmd/gcx/resources/validate.go). Keep result production independent of the selected format. Reuse the existing exit-code definitions and describe the opt-in behavior in the safety and exit-code guidance.

Preview logic must be shared by the dedicated command and adapter path where both expose the same operation. Start with the current input type, schema, reference lookup, and secret handling. Add a small provider-local plan type only when the first two real callers need it. Do not introduce a generic planning framework for possible future providers.

A proposed preview result contains the operation, resource identity, verification level, changed field names, and unresolved checks. This is a new output contract for a new option. Keep ordinary mutation output unchanged. Add a schema and tests for the exact result before the preview implementation is accepted.

Every new leaf declares its output class and token cost. Collection commands use bounded pagination and the shared limit conventions. If a backend cannot provide bounded completeness, the command must state the limit instead of returning a complete-looking list. Reuse existing RPC error handling, including errors returned inside an HTTP 200 response.

Settings reads have an explicitly viable slice: the plugin responses contain both requested booleans. Settings writes still require verified plugin paths, allowed fields, permission behavior, and read-back. Regional API documentation establishes product capability, but not the internal transport contract. Unknown API ownership, mutation safety, or pagination is a backend prerequisite.

## Verification and delivery

Implement in small PRs after this RFC is accepted. Documentation and strict validation can proceed without new IRM write contracts. Hook-run reads reuse an existing client. Task writes, swap acceptance, timeline reads, access-setting writes, and modified-time export need the prerequisites below before their relevant slices proceed.

Use a freshly built `bin/gcx` against the selected test context. Keep stack names and resource identifiers out of public evidence. Verify schemas, catalogs, help, strict validation, and preview results with read-only calls. Confirm that preview leaves the target unchanged. Exercise API request mappings and no-write guarantees with local HTTP fixtures.

Positive write tests use isolated temporary resources only where they can be removed. Do not create a permanent incident just to test description or task input. Do not page responders or change active rotations. A write path that cannot be checked safely is reported as unverified, with the required follow-up test. Unit tests do not establish a deployed API contract.

Run the repository checks for each PR. If a required tool is unavailable, publish a draft PR and identify the failed or unavailable check. Do not report an incomplete full gate as passing.

## Drawbacks

The new commands add maintenance work across multiple APIs. A local preview can become stale between read and apply. Some requested operations depend on internal endpoints whose contract is not yet established. Strict validation is useful only when callers opt in. Existing output shapes remain different during v1.

## Rationale and alternatives

An opt-in strict flag preserves the existing skipped-result contract. Changing the default exit code would break scripts that use safe best-effort validation.

A local plan gives useful input and diff checks without unsafe server writes. Treating every preview as server verified would hide API limits. A complete server planner could improve final-calendar previews later, but it is a product API dependency.

Dedicated commands provide typed inputs, bounded output, and consistent errors. A documented raw API example is a useful fallback, but it does not replace those contracts. Adding a regional transport now would also expand the credential and endpoint scope beyond these requests.

## Prior art

Reuse the resource dry-run guard, typed CRUD registration, the existing shift swap action client, and the hook-run reader used for post-incident review links. Follow the existing incident activity command family for incident-scoped operations. Preserve the shared list/get builders' released serialization behavior.

## Unresolved questions

| Question | Evidence needed | Owner |
|---|---|---|
| Which task read method provides complete task data? | Read response schema, pagination, permissions, and a test-stack probe. | IRM API owners |
| What does swap acceptance permit for the benefactor? | Deployed request/response contract, current-user and delegated-user permission checks. | IRM OnCall owners |
| Which plugin route serves the alert-group timeline and incident link? | Published or owned route contract, event schema, ordering, pagination, and a read probe. | IRM OnCall owners |
| Which settings can be updated through the plugin API? | Allowed write fields, permissions, and read-back. Both requested booleans were observed on plugin reads. | IRM OnCall owners |
| Can the API validate Jinja or calculate a prospective calendar without a write? | Explicit no-write endpoint and behavior. Otherwise retain a stated preview limit. | IRM OnCall owners |
| Does incident query support a reliable modification cursor? | Update-time selection, tie-breaker, continuation stability, and a test with an older changed incident; modifiedTime ordering is already documented. | IRM Incident owners |

These questions are prerequisites for the affected slices, not reasons to guess a new contract. Documentation, strict validation, and locally proven preview behavior can be reviewed separately.

## Future possibilities

A verified modification cursor can support warehouse synchronization. A server calendar planner can support overlap and boundary warnings before an update. A future major release can adopt a common envelope for OnCall structured output.

## Terminology

- **Local preview**: input checks and a plan calculated without applying a server change.
- **Server verification**: a result from an established no-write server validation operation.
- **Skipped validation**: the server check was not performed; the resource has not passed validation.
- **Benefactor**: the responder who accepts the offered shift period.
- **Plugin proxy**: the stack-local IRM route that uses the configured Grafana authentication.
