# Spec Delta

## Purpose

Lets users manage Grafana notification routing trees, the default tree and additional named trees, through dedicated `gcx alert routing-trees` commands and the generic `gcx resources` commands, using native `RoutingTree` manifests.

## ADDED Requirements

### Requirement: List routing trees
`gcx alert routing-trees list` SHALL return every routing tree in the configured namespace, including the default tree `user-defined`, as native `RoutingTree` manifests.

#### Scenario: Default and named trees listed
- **WHEN** the target has the default tree and named trees `team-a` and `team-b`
- **THEN** `list` returns three items named `user-defined`, `team-a`, and `team-b`

#### Scenario: Structured output
- **WHEN** `list -o json` runs
- **THEN** each item is a native manifest with `apiVersion`, `kind: RoutingTree`, `metadata` (including `resourceVersion` and annotations), and `spec`

### Requirement: Get a routing tree by name
`gcx alert routing-trees get <name>` SHALL return the named tree's native manifest unchanged. Names SHALL be passed to the server as given, without client-side aliasing.

#### Scenario: Existing tree
- **WHEN** `get team-a -o yaml` runs and `team-a` exists
- **THEN** the output is the server's `RoutingTree` manifest for `team-a`, including `metadata.resourceVersion`

#### Scenario: Missing tree
- **WHEN** `get team-z` runs and no such tree exists
- **THEN** the command exits non-zero and reports the server's not-found error

### Requirement: Create a routing tree
`gcx alert routing-trees create -f <file>` SHALL create the tree described by the manifest and SHALL NOT update an existing tree.

#### Scenario: New named tree
- **WHEN** `create -f new-tree.yaml` runs with a manifest named `team-c` that does not exist
- **THEN** the tree is created and the command reports the created resource

#### Scenario: Name already exists
- **WHEN** `create` runs with a manifest whose name already exists or is reserved (`user-defined`, `default`)
- **THEN** the command exits non-zero, reports the server's conflict error, and changes nothing

### Requirement: Update a routing tree
`gcx alert routing-trees update <name> -f <file>` SHALL replace the named tree with the manifest using optimistic concurrency, and SHALL leave every other tree unchanged.

#### Scenario: Update one named tree
- **WHEN** `team-a` is fetched with `get`, edited, and applied with `update team-a -f tree.yaml`
- **THEN** `team-a` reflects the edit, and `team-b` and `user-defined` are unchanged

#### Scenario: Missing resource version
- **WHEN** the manifest has no `metadata.resourceVersion`
- **THEN** the command exits with a usage error before sending any request, and tells the user to fetch the current tree first

#### Scenario: Stale resource version
- **WHEN** the tree changed on the server after the manifest was fetched
- **THEN** the command exits non-zero and reports the server's conflict error

#### Scenario: Name mismatch
- **WHEN** the `<name>` argument differs from the manifest's `metadata.name`
- **THEN** the command exits with a usage error before sending any request

### Requirement: Delete a routing tree
`gcx alert routing-trees delete <name>` SHALL delete a named tree, or reset the default tree, after confirmation. The result SHALL state which of the two happened.

#### Scenario: Delete a named tree
- **WHEN** `delete team-a --force` runs
- **THEN** `team-a` no longer exists and the result reports it as deleted

#### Scenario: Reset the default tree
- **WHEN** `delete user-defined --force` runs
- **THEN** the default tree is reset to Grafana's built-in configuration and the result reports a reset, not a removal

#### Scenario: Confirmation required
- **WHEN** `delete team-a` runs interactively without `--force`
- **THEN** the command prompts before deleting, and a declined prompt exits with the cancelled status and changes nothing

#### Scenario: Agent mode without force
- **WHEN** `delete team-a` runs in agent mode without `--force` or `GCX_AUTO_APPROVE`
- **THEN** the command fails with an actionable error naming `--force` and changes nothing

#### Scenario: No confirmation input
- **WHEN** `delete team-a` runs outside agent mode without `--force` and standard input is closed
- **THEN** the command fails with an error naming `--force` and changes nothing

### Requirement: Manifest input
`create` and `update` SHALL read the manifest from the path given by `-f/--filename`, or from standard input when the value is `-`, in JSON or YAML.

#### Scenario: Manifest from stdin
- **WHEN** `get team-a -o yaml | update team-a -f -` runs
- **THEN** the manifest is read from standard input and the update proceeds as with a file

### Requirement: API version selection
Commands SHALL use the server's preferred `notifications.alerting.grafana.app` version unless one is specified. For `list`, `get`, and `delete`, `--api-version` SHALL select it. For `create` and `update`, the manifest's `apiVersion` SHALL select it.

#### Scenario: Preferred version
- **WHEN** `list` runs without `--api-version` against Grafana 13.x
- **THEN** the request uses `v1beta1`

#### Scenario: Older server
- **WHEN** `list` runs against Grafana 12.x
- **THEN** the request uses `v0alpha1`

#### Scenario: Manifest version used
- **WHEN** `update` runs with a manifest whose `apiVersion` is `notifications.alerting.grafana.app/v0alpha1`
- **THEN** the request uses `v0alpha1`

#### Scenario: Flag and manifest disagree
- **WHEN** `create` or `update` runs with `--api-version` that differs from the manifest's `apiVersion`
- **THEN** the command exits with a usage error before sending any request

#### Scenario: Wrong group
- **WHEN** the manifest's or flag's group is not `notifications.alerting.grafana.app`, or its kind is not `RoutingTree`
- **THEN** the command exits with a usage error before sending any request

### Requirement: Server responses are surfaced unchanged
Commands SHALL send each requested operation once to the native API and SHALL report the server's response. They SHALL NOT probe for feature availability, retry through another API, or turn a not-found response into a feature diagnosis.

#### Scenario: Named trees unavailable
- **WHEN** `create` of a named tree runs against a target where named trees are disabled or unsupported
- **THEN** the command exits non-zero with the server's error, sends no other request, and the default tree is unchanged

#### Scenario: Permission denied
- **WHEN** the server responds 403 to any routing-tree operation
- **THEN** the command exits with the auth-failure status and reports the server's message

### Requirement: Generic resource access
`gcx resources` SHALL expose `routingtrees` in `notifications.alerting.grafana.app` with the same manifests as the dedicated commands. Other resources in that group SHALL remain hidden from `gcx resources`.

#### Scenario: Same manifest from both paths
- **WHEN** `gcx resources get routingtrees.notifications.alerting.grafana.app/team-a -o yaml` and `gcx alert routing-trees get team-a -o yaml` run
- **THEN** both return the same manifest

#### Scenario: Other notification resources hidden
- **WHEN** `gcx resources list-types` runs against a server that serves receivers, template groups, and time intervals
- **THEN** `routingtrees` is listed and those other resources are not

#### Scenario: Push uses destination version
- **WHEN** a manifest fetched with `get` from one stack is pushed with `gcx resources push` to another stack where a tree with that name exists
- **THEN** the destination tree is updated using the destination's current resource version
