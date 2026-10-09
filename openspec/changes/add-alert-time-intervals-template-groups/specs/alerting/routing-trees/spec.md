# Spec Delta

## MODIFIED Requirements

### Requirement: Generic resource access
`gcx resources` SHALL expose `routingtrees`, `timeintervals`, and `templategroups` in `notifications.alerting.grafana.app` with the same manifests as the dedicated commands. Receivers, inhibition rules, and other resources in that group SHALL remain hidden from `gcx resources` until explicitly exposed by a later change.

#### Scenario: Same manifest from both paths
- **WHEN** `gcx resources get routingtrees.notifications.alerting.grafana.app/team-a -o yaml` and `gcx alert routing-trees get team-a -o yaml` run
- **THEN** both return the same manifest

#### Scenario: Other notification resources hidden
- **WHEN** `gcx resources list-types` runs against a server that serves routing trees, time intervals, template groups, receivers, and inhibition rules
- **THEN** routing trees, time intervals, and template groups are listed, while receivers and inhibition rules are not

#### Scenario: Push uses destination version
- **WHEN** a manifest fetched with `get` from one stack is pushed with `gcx resources push` to another stack where a tree with that name exists
- **THEN** the destination tree is updated using the destination's current resource version
