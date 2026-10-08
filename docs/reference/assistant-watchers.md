# Assistant Watchers

Watcher reads are experimental and require Grafana Cloud. They use the selected gcx context and its configured identity. These commands inspect existing Watchers without starting monitoring, requesting calibration or changing configuration.

```bash
gcx assistant watchers list
gcx assistant watchers list --archived -o json
gcx assistant watchers get checkout-health -o yaml
gcx assistant watchers status checkout-health -o json
gcx resources get watchers/checkout-health -o json
gcx resources pull watchers -p ./watchers -o yaml
```

`list` reads every page of the non-archived Watchers visible to the caller. `--archived` lists archived Watchers only. Machine output carries an `items` array and coverage describing the selected archive partition and caller-visible scope. This is not an atomic snapshot or an inventory of resources the caller cannot access.

`get` returns the same configuration-only manifest as `resources get`. The resource name is derived from its title; the server-assigned ID appears in the `assistant.ext.grafana.app/watcher-id` annotation. Names that match several visible Watchers report their candidate IDs. Use an ID to select one explicitly. Bulk pull writes unaffected Watchers and reports conflicting candidates, including conflicts with archived Watchers, without overwriting a conflicting file.

`status` separates lifecycle from the last reported health assessment. It includes available calibration progress and checks, run timestamps, estimated usage, audit metadata and the current definition version. Missing observations remain absent or unknown: a paused Watcher with an older warning is still paused, and a latest-run timestamp is not proof of completion. Unsupported check types and parameters remain inspectable. If calibration needs input or fails, continue in Grafana.

Pulled manifests contain the configuration defined by [RFC 002](https://github.com/grafana/gcx/blob/main/docs/rfcs/002-assistant-watchers.md). Calibrated checks, learned state and runtime history are excluded. Configuration outside that model is not exported, so this is not a full backup of every product setting. Modeled settings must be readable; denied or unavailable supplementary configuration reads cause explicit export failure rather than a guessed default.

Webhook URLs and credentials are never exported as values or display placeholders. Each confirmed configured secret is represented as `{ preserve: true }`, including secrets on disabled destinations. The schema also recognizes `fromEnv`, `fromFile` and `clear: true` inputs for later write support; these reads do not resolve secret sources. Each input selects exactly one alternative. An enabled webhook cannot clear its URL.

Watcher writes are not supported yet. `resources push` and `resources delete` reject Watcher mutations; dedicated create/update/delete, calibration, lifecycle, run history and version-history commands are outside this release slice.
