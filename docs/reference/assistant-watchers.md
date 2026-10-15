# Assistant Watchers

Watcher commands are experimental and require Grafana Cloud. They use the selected gcx context and its configured identity. Dedicated `create` saves an inactive definition and requests its first background calibration without automatic activation. `update` and `resources push` reconcile configuration only. Configuration writes do not activate monitoring, pause Watchers or execute runs.

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

`status` separates lifecycle from the last reported health assessment. It includes available calibration progress and checks, run timestamps, estimated usage, audit metadata and the current definition version. Missing observations remain absent or unknown: a paused Watcher with an older warning is still paused, and a latest-run timestamp is not proof of completion. Unsupported check types and parameters remain inspectable; this marks properties gcx does not recognize, rather than declaring the server configuration invalid. Each calibration state includes recovery guidance. Queued or running work is not ready for activation. When calibration completes, review enabled checks and lifecycle in Grafana before explicitly starting monitoring; completion alone does not establish readiness. Failed, cancelled or input-needed calibration must be continued in Grafana.

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

`create -f FILE` requires one YAML or JSON manifest; `-f -` reads stdin. After a successful configuration save and readback, it requests background calibration once, without automatic activation. Creation refuses an existing exact title and recommends update. `update WATCHER -f FILE` replaces the modeled configuration of its positional target. A manifest ID that conflicts with that target fails before mutation. An explicit target can be renamed, but cannot take another visible Watcher's exact title.

Optional fields are authoritative, including when omitted: description defaults to empty, data-source and team collections and labels to empty, interval to `10m`, sensitivity to `balanced`, clean-run review skipping to enabled, and optional notifications, investigation, automatic stop and automatic recalibration to disabled. Notification severity defaults to `warning-and-critical`. Omitted destinations become disabled; omitted collection entries are removed. Disabled automatic stop clears its deadline and archive intent. Secret omission has separate preservation semantics below.

Sensitivity and notification severity are open strings. Documented sensitivity values are `sensitive`, `balanced` and `relaxed`; documented severity values are `warning-and-critical` and `critical`. Unfamiliar values survive read/export and produce nonfatal client-validation warnings naming the field, value and known values. The API decides whether they are accepted on apply. Unknown fields, incorrect types and invalid dependencies still fail validation.

Mutation refuses meaningful configuration that the established manifest cannot represent, including additional notification targeting or delivery settings, nonstandard Alerting severity or named routing, and custom automatic-recalibration schedules. The refusal identifies the setting category without exposing its values. Use Grafana to manage those settings. Calibration-owned content, observed lifecycle and audit fields, and deprecated compatibility settings do not independently block configuration writes. The API decides whether an archived or calibrating Watcher can be edited; gcx reports its rejection rather than imposing a local state rule.

## Apply, compare and recover

Dedicated create/update and generic resource push share one reconciliation implementation. Generic push first selects a matching same-stack annotated ID. An absent or stale ID falls back to an exact full-title match across visible non-archived and archived Watchers: zero matches creates, one updates, and multiple matches report all candidate IDs without mutation. A batch resolves every input before writing and rejects all manifests selecting the same Watcher or requesting the same new title. Distinct manifests sharing a computed resource name are retained for this check. Independent concurrent creators can still produce a later detectable collision.

Comparison uses effective defaults, equivalent intervals and timestamp instants, and collections whose ordering has no meaning. Interval comparison and writes use the API's rounded scheduling cadence: for example, `7m` becomes `6m` and `90m` becomes `2h`. Runtime observations and calibrated content do not trigger configuration writes. A fully unchanged apply sends no mutation. Automatic-recalibration enablement is reconciled separately: an enrollment-only change does not rewrite the definition, and an unchanged enrollment sends no request. Enrollment does not activate a Watcher or request a calibration run.

Mutation output uses the versioned `gcx.mutation` envelope: `action` (`created`, `updated`, `unchanged` or `deleted`), `target` with the resulting `name` and `id`, `changed`, and `outcome` with `changed`, `configuration`, `enrollment` and `readback` stages. Supported formats use the same outcome, including text/table/wide and JSON/YAML/agent output. A saved definition followed by failed enrollment or readback returns the mutation envelope with an error, exit 4 and its saved identity. There is no automatic rollback or retry. Inspect that ID with `get`, resolve the failed stage and reapply with `update`; unchanged stages are skipped. A failed initial configuration request does not claim success or attempt enrollment.

Dedicated creation also includes a `resource` configuration manifest and a separate `calibration` object with `outcome`, observed `state` when available, and `guidance`. The resource is the configuration read before calibration was requested, not a runtime status snapshot. Outcomes are:

| Outcome | Meaning and recovery |
|---|---|
| `accepted` | The request was accepted; checks are not necessarily ready. Follow `status WATCHER` and review calibration in Grafana. |
| `unavailable` | The definition was created but background calibration is unsupported. Calibrate it in Grafana. Creation exits successfully. |
| `error` | The definition was created but request acceptance could not be confirmed. Inspect `status WATCHER` and continue in Grafana; exit 4. |
| `not-requested` | A configuration stage failed after creation. Resolve that stage with `update`, then calibrate in Grafana; exit 4. |

Request failures retain the created Watcher. There is no automatic retry or deletion; a timeout or invalid response can leave calibration running despite an `error` outcome. Do not rerun `create`. Partial configuration results retain identity and stage outcomes but omit the unverified resource manifest. `update`, generic push and deletion do not request calibration and keep their configuration mutation output.

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

File-based resource deletion addresses an annotated ID exactly. A stale ID fails as not found and never falls back to title or resource name. Without an ID, the resource name follows normal ambiguity rules. Delete dry-run selects identities without deleting them. Lifecycle controls and run history are described below; definition-version operations remain separate from archive recovery. Calibration follow-up stays in Grafana; gcx has no calibration request, cancel, retry or recalibrate commands.

## Lifecycle and run history

```bash
gcx assistant watchers start checkout-health -o json
gcx assistant watchers run checkout-health -o json
gcx assistant watchers list-runs checkout-health --limit 20 -o json
gcx assistant watchers pause checkout-health -o json
gcx assistant watchers archive checkout-health -o json
gcx assistant watchers unarchive checkout-health -o json
```

`start` asks the server to activate a calibrated definition with at least one enabled check. It can immediately enqueue a scan and deliver configured notifications or launch investigations; event-only Watchers wait for their trigger. An initial scan request can fail after monitoring has become active. Failed lifecycle results retain the selected identity, report any observed lifecycle from a best-effort readback, and return nonzero. Inspect `status` before retrying. gcx never automatically retries or rolls back lifecycle requests.

`pause` stops future scans. An in-flight scan may finish and deliver configured notifications or investigations. Finish or cancel calibration in Grafana before pausing a calibrating Watcher. Neither pause nor archive promises cancellation of an already executing scan or its notifications.

`run` reports request acceptance, skipped work or a rejected/uncertain request. Acceptance never means a completed assessment. Its `taskId` is an enqueue identifier; history records use different IDs and exact correlation is unavailable. Do not assume the newest history entry belongs to this request. The API enforces lifecycle, calibration and enabled-check requirements. Running may require create permission in addition to write permission; being able to edit a Watcher does not establish permission to run it.

`list-runs` returns bounded summaries of the newest retained caller-visible executions: ID, execution status, assessment, trigger, timestamps and definition version. The default limit is 20; `--limit` accepts 1 through 100. gcx reads an extra entry, including a second request at the maximum limit, to report `coverage.hasMore`. Coverage is not an atomic snapshot or a promise of lifetime retention. Field selection preserves coverage. Failed, running and unknown execution states never render a healthy assessment; use Grafana for full run evidence.

`archive` requires an observed, explicitly paused, non-archived Watcher. gcx never invokes pause implicitly. This precheck and the archive request are not atomic: another client's concurrent start can be stopped by archive. Configuration and history remain available; archive can cancel pending calibration and resolve active Grafana Alerting findings.

`unarchive` requires an archived Watcher and recovers it without activating monitoring. It clears automatic-stop archive intent but retains the stop deadline, which can still prevent a later start. A concurrent start by another client can supersede recovery; an unexpected active response is reported as an error without an implicit corrective pause. Archive recovery is separate from restoring a definition version.
