# Assistant Watchers

Watcher commands are experimental and require Grafana Cloud. They use the selected gcx context and its configured identity. Configuration writes create inactive, uncalibrated definitions or reconcile existing configuration. They do not activate monitoring, pause Watchers, request calibration or execute runs.

```bash
gcx assistant watchers list
gcx assistant watchers list --archived -o json
gcx assistant watchers get checkout-health -o yaml
gcx assistant watchers status checkout-health -o json
gcx assistant watchers create -f watcher.yaml
gcx assistant watchers update checkout-health -f watcher.yaml
gcx assistant watchers delete checkout-health --force
gcx resources get watchers/checkout-health -o json
gcx resources pull watchers -p ./watchers -o yaml
gcx resources push -p ./watchers
gcx resources delete watchers/checkout-health --force
```

## Read configuration and observations

`list` reads every page of the non-archived Watchers visible to the caller. `--archived` lists archived Watchers only. Machine output carries an `items` array and coverage describing the selected archive partition and caller-visible scope. This is not an atomic snapshot or an inventory of resources the caller cannot access.

After discovery, one failed configuration read does not discard other Watchers. List reports failed and skipped candidates with incomplete coverage; actionable per-item failures return exit 4. A Watcher that disappears before its detail read is skipped. Pull retains successful manifests and records failures individually; `--on-error abort` writes no files. Collection paging failures still fail the collection.

`get` returns the same configuration-only manifest as `resources get`. The resource name is derived from its title; the server-assigned ID appears in the `assistant.ext.grafana.app/watcher-id` annotation. Names that match several visible Watchers report their candidate IDs. Use an ID to select one explicitly. Bulk pull writes unaffected Watchers and reports conflicting candidates, including conflicts with archived Watchers, without overwriting a conflicting file.

`status` separates lifecycle from the last reported health assessment. It includes available calibration progress and checks, run timestamps, estimated usage, audit metadata and the current definition version. Missing observations remain absent or unknown: a paused Watcher with an older warning is still paused, and a latest-run timestamp is not proof of completion. Unsupported check types and parameters remain inspectable; this marks properties gcx does not recognize, rather than declaring the server configuration invalid. If calibration needs input or fails, continue in Grafana.

Selector-free `resources get` and `resources pull` include registered Watchers even when their API version differs from the group's preferred native version. A selector-free pull or get skips an unavailable Watcher or MCP server API; an explicit selection still fails under the selected error policy.

## Authoritative configuration manifests

Pulled manifests retain the configuration shape defined by [RFC 002](https://github.com/grafana/gcx/blob/main/docs/rfcs/002-assistant-watchers.md). Calibrated checks, calibration guidance, learned state and runtime history are excluded and preserved when applying configuration. This is not a full backup of every product setting. Modeled settings must be readable; denied or unavailable supplementary configuration reads cause explicit export or reconciliation failure rather than a guessed default.

```yaml
apiVersion: assistant.ext.grafana.app/v1alpha1
kind: Watcher
metadata:
  name: checkout-health
spec:
  title: Checkout health
  prompt: Watch request failures and sustained latency increases.
  datasourceUids: [example-prometheus]
  interval: 15m
  sensitivity: balanced
  labels:
    service: checkout
  automaticRecalibration:
    enabled: false
```

`create -f FILE` requires one YAML or JSON manifest; `-f -` reads stdin. Creation refuses an existing exact title and recommends update. `update WATCHER -f FILE` replaces the modeled configuration of its positional target. A manifest ID that conflicts with that target fails before mutation. An explicit target can be renamed, but cannot take another visible Watcher's exact title.

Optional fields are authoritative, including when omitted: description defaults to empty, data-source and team collections and labels to empty, interval to `10m`, sensitivity to `balanced`, clean-run review skipping to enabled, and optional notifications, investigation, automatic stop and automatic recalibration to disabled. Notification severity defaults to `warning-and-critical`. Omitted destinations become disabled; omitted collection entries are removed. Disabled automatic stop clears its deadline and archive intent. Secret omission has separate preservation semantics below.

Sensitivity, notification severity and Slack target types are open strings. Documented sensitivity values are `sensitive`, `balanced` and `relaxed`; documented severity values are `warning-and-critical` and `critical`. Slack target types are `channel` (the default) and `dm`. Unfamiliar values survive read/export and produce nonfatal client-validation warnings naming the field, value and known values. The API decides whether they are accepted on apply. Unknown fields, incorrect types and invalid dependencies still fail validation.

Mutation refuses meaningful configuration that the established manifest cannot represent, including additional Teams targeting or delivery settings and custom automatic-recalibration schedules. Custom schedules remain outside the manifest model. The refusal identifies the setting category without exposing its values. Use Grafana to manage those settings. Calibration-owned content, observed lifecycle and audit fields, and deprecated compatibility settings do not independently block configuration writes. The API decides whether an archived or calibrating Watcher can be edited; gcx reports its rejection rather than imposing a local state rule.

## Slack destinations and Alerting routing

Slack retains the existing `channelId` field. Set `targetType: dm` with a Slack `userId` for a direct message, or use `targetType: channel` with `channelId` for a channel. Optional `teamId` selects the Slack workspace. Enabled channels require a nonblank `channelId`; a direct-message target requires a nonblank `userId` even when disabled. For compatibility, disabled channel destinations may retain workspace or user identifiers before a channel is selected. These remain inspectable and exportable; the API validates whether the complete target can be saved on apply. A supplied whitespace-only channel ID is invalid. Teams retains its channel-only configuration.

```yaml
notifications:
  slack:
    enabled: false
    targetType: dm
    userId: U_EXAMPLE
    teamId: T_EXAMPLE
    severity: critical
  alerting:
    enabled: false
    severity: critical
    routingTree: example-policy-tree
```

This fragment belongs under `spec`. Alerting uses the same severity vocabulary as the other destinations, defaulting to `warning-and-critical`. `routingTree` selects a named Grafana Alerting notification policy tree; omission or an empty string selects the default tree. gcx configures the destination selection, while Grafana Alerting manages the policy trees and contact points.

An explicit routing selection takes precedence over the legacy `__grafana_managed_route__` label. Exports recover the legacy selection when the server has no explicit routing field, then omit that reserved label from `spec.labels`. Writes also omit it so it cannot override a later reset; ordinary labels are retained. A changed route is trimmed, and `default` or `user-defined` selects the default tree. An unchanged exported legacy route retains its exact value on reapply, including when other configuration changes. To reset such a legacy value, set `routingTree: ""` explicitly. Optional fields remain authoritative: omitting the new fields restores their defaults, and omitting Slack destination IDs removes them.

## Apply, compare and recover

Dedicated create/update and generic resource push share one reconciliation implementation. Generic push first selects a matching same-stack annotated ID. An absent or stale ID falls back to an exact full-title match across visible non-archived and archived Watchers: zero matches creates, one updates, and multiple matches report all candidate IDs without mutation. A batch resolves every input before writing and rejects all manifests selecting the same Watcher or requesting the same new title. Distinct manifests sharing a computed resource name are retained for this check. Independent concurrent creators can still produce a later detectable collision.

Comparison uses effective defaults, equivalent intervals and timestamp instants, and collections whose ordering has no meaning. Interval comparison and writes use the API's rounded scheduling cadence: for example, `7m` becomes `6m` and `90m` becomes `2h`. Runtime observations and calibrated content do not trigger configuration writes. A fully unchanged apply sends no mutation. Automatic-recalibration enablement is reconciled separately: an enrollment-only change does not rewrite the definition, and an unchanged enrollment sends no request. Enrollment does not activate a Watcher or request a calibration run.

Mutation output uses the versioned `gcx.mutation` envelope: `action` (`created`, `updated`, `unchanged` or `deleted`), `target` with the resulting `name` and `id`, `changed`, and `outcome` with `changed`, `configuration`, `enrollment` and `readback` stages. Supported formats use the same outcome, including text/table/wide and JSON/YAML/agent output. A saved definition followed by failed enrollment or readback returns the mutation envelope with an error, exit 4 and its saved identity. There is no automatic rollback or retry. Inspect that ID with `get`, resolve the failed stage and reapply with `update`; unchanged stages are skipped. A failed initial configuration request does not claim success or attempt enrollment.

`resources push --dry-run` and `resources validate` validate locally without reading secret sources or target configuration and without mutations. They warn about unfamiliar string values and report that server acceptance remains unverified. Dry-run cannot establish whether the API accepts an unfamiliar value or whether existing target settings are representable.

## Webhook secret intent

Webhook URLs and credentials are never exported as values, previews or display placeholders. Each confirmed configured secret is represented as `{ preserve: true }`, including secrets on disabled destinations. Inputs select exactly one operation:

| Input | Effect |
|---|---|
| `fromEnv: WATCHER_WEBHOOK_URL` | Supply the nonempty value of an environment variable |
| `fromFile: ./secrets/webhook-signing-key` | Supply the nonempty contents of a file |
| `preserve: true`, or omission on update | Keep the existing secret |
| `clear: true` | Remove the secret; clearing an absent secret is unchanged |

Sources are resolved only after local validation, identity checks and unsupported-setting refusal. Missing or empty sources fail. Preserve markers require an existing Watcher and fail on create. An enabled webhook requires a URL and cannot clear it. Supplying a secret always sends a definition write, even when nonsecret configuration is unchanged. Every definition write sends the complete desired modeled configuration, including secret-only edits; equal nonsecret settings are retransmitted and every desired notification destination is retained. Calibration-owned content, learned state and lifecycle inputs remain excluded. No particular definition-version behavior is promised for secret-only edits.

Whenever the URL is supplied or cleared, every currently saved bearer token and signing secret must also be supplied or cleared in that request. Omission or preserve is insufficient, including on a disabled webhook and when a supplied URL appears unchanged.

```yaml
notifications:
  webhook:
    enabled: true
    severity: critical
    url:
      fromEnv: WATCHER_WEBHOOK_URL
    bearerToken:
      fromEnv: WATCHER_WEBHOOK_TOKEN
    signingSecret:
      clear: true
```

This fragment belongs under `spec` in a complete manifest. Keep resolved values out of manifests and version control. Secret inputs and raw server payloads do not appear in mutation outcomes or errors.

## Delete by identity

Dedicated deletion uses the same typed deletion implementation as generic resources and the shared confirmation/`--force` rules. It accepts a server ID or an unambiguous resource name. Deletion selects identity without requiring enrollment or complete configuration export; unsupported settings do not prevent explicit deletion.

File-based resource deletion addresses an annotated ID exactly. A stale ID fails as not found and never falls back to title or resource name. Without an ID, the resource name follows normal ambiguity rules. Delete dry-run selects identities without deleting them. Calibration, lifecycle controls, run history and version-history commands remain outside this slice.
