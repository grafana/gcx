---
aliases:
  - /docs/grafana-cloud/as-code/observability-as-code/grafana-cli/gcx/anonymous-usage-statistics/
title: Usage statistics
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 4
---

# Understand gcx usage statistics

`gcx` reports limited usage statistics about itself to Grafana Labs. This data is used to understand which commands and flags are used most, where commands fail, and which commands people try that don't exist, so we can make the product better.

The statistics describe only the *shape* of usage, including command path, and flag names. Positional argument values, free-form flag values, and resource names are never sent, and the flags you set are recorded by name only. Numeric values such as exit code, duration, and HTTP status describe command execution or protocol behavior. No raw count of batch or resource volume is sent.

For the resource commands that operate on batches, the size of the operation is sent as one of seven fixed categories rather than as a number. Two of those categories, `0` and `1`, cover a single value each, so those two sizes are exact; every larger category is a range. See [How to read the batch fields](#how-to-read-the-batch-fields).

Further fields describe how the command ran rather than naming a flag: `output_format` records the output format used, from a fixed list of known formats; `dry_run` records whether the operation executed in dry-run mode; and `grafana_auth_method` records the authentication category selected for the Grafana connection, from a fixed vocabulary and never the credential itself. `output_format` is read from `--output`, which means a command that renders JSON because you passed `--json` is still recorded with `--output`'s value; that mismatch is a known bug rather than intended behaviour. `dry_run` is derived from the operation and is set even for commands that have no `--dry-run` flag; `grafana_auth_method` is described in [Failure and authentication fields](#failure-and-authentication-fields). Some server-side enrichment is also performed on the usage statistics exported - see [Server-side enrichment](#server-side-enrichment) for details.

{{< admonition type="note" >}} Usage statistics reporting is **enabled by default**. See the [Opt out](#opt-out) section below for guidance on how to turn off usage reporting.{{< /admonition >}}

## Telemetry data and identifiers

The only identifier is a `device_id` field: a randomly generated UUID created on first use and stored at `$XDG_STATE_HOME/gcx/device-id`. It identifies an installation of `gcx`, not a person. It's random, not derived from your hardware or account.

## Understand which data is collected

Each `gcx` event contains the following properties:

| Field | Description | Example |
| :---- | :---- | :---- |
| `service` | Always `gcx`, identifying the reporting product. | `gcx` |
| `version` | The version of `gcx`. | `0.4.1` |
| `os` | Operating system. | `linux`, `darwin`, `windows` |
| `arch` | CPU architecture. | `amd64`, `arm64` |
| `device_id` | The random per-installation ID described in [Telemetry data and identifiers](#telemetry-data-and-identifiers). | UUID |
| `device_id_persisted` | Whether the device ID was read from or saved to disk. `false` means a throwaway ID was used for this invocation. | `true` |
| `command` | The resolved command path only, no arguments are sent. | `dashboards push` |
| `flags` | The **names** of the flags you set, sorted. No flag values are sent in this field. | `dry-run,folder` |
| `provider` | The resource provider the command belongs to. | `dashboards` |
| `outcome` | How the invocation ended: `ok`, `runtime_error`, `canceled`, or `help`. (`parse_error` is reserved and not sent yet — see below.) | `ok` |
| `exit_code` | The process exit code. | `0` |
| `error_kind` | A coarse error category when the command failed: `usage_error`, `auth_failure`, `partial_failure`, `version_incompatible`, or `error`. Never an error message. Empty when the command did not fail, including when it was canceled. | `auth_failure` |
| `duration_ms` | Total invocation duration in milliseconds. | `1234` |
| `is_tty` | Whether `gcx` ran attached to an interactive terminal. | `false` |
| `is_ci` | Whether a CI environment was detected. | `true` |
| `ci_provider` | Which CI system was detected, from a fixed list of known names. `gcx` reads well-known CI environment variables to detect the provider but never sends their values. | `github_actions` |
| `is_agent` | Whether an AI coding agent drove the invocation. | `true` |
| `agent` | The name of the agent harness, if one was detected. | `claude-code` |
| `target_kind` | Whether the target Grafana is `cloud` or `self-hosted`. Empty when no effective Grafana target could be resolved. Deliberately coarse — never the URL, hostname, or stack slug. | `cloud` |
| `output_format` | The output format the command used. | `table`, `json` |

When the invocation is a batch resource operation (`gcx resources push`, `pull`, `delete`, or `validate`) that ran to completion, these additional fields are set. They describe the *size* of the operation, never what it contained:

| Field | Description | Example |
| :---- | :---- | :---- |
| `batch_succeeded_bucket` | The size of the successful part of the operation, as one of seven fixed categories. | `21-100` |
| `batch_failed_bucket` | The size of the failed part, from the same seven categories. | `0` |
| `batch_skipped_bucket` | The size of the skipped part, from the same seven categories. | `0` |
| `dry_run` | Whether the operation executed in dry-run mode. `false` does not imply anything was changed: `gcx resources pull` is read-only and always reports `false`. Interpret it together with `command`. Derived from the operation, not read from a flag: `gcx resources validate` always reports `true` and `pull` always `false`, and neither has a `--dry-run` flag. | `false` |

The seven categories are exactly `0`, `1`, `2-5`, `6-20`, `21-100`, `101-1000`, and `1001+`. Note that `0` and `1` are singleton categories, so those two sizes are recoverable exactly; every larger category is a range.

Sizes are sent as categories rather than as numbers on purpose. An exact count of a large batch, correlated with the per-installation `device_id` and the network organization name added on receipt, would describe a specific organization's resource inventory. Categories answer how `gcx` is used without carrying that detail, and the two singleton categories carry no inventory to infer. No raw numeric count field is sent.

### How to read the batch fields

These fields are easy to misread, so the following constraints are part of the contract:

- **All four are present together, or none are.** Their absence means the invocation was not one of those four commands, or it stopped before the operation reached a final count.
- **`0` means nothing was counted in that outcome, which is not the same as nothing having happened.** The three counts are not a complete partition of the work: a resource filtered out before processing is recorded in none of them. `gcx resources validate` over resources that are all managed elsewhere, or `gcx resources delete` against a resource type whose API does not support deletion, can therefore report `0`/`0`/`0` for a run that did examine resources. A `0` is still distinct from the field being absent, which means the operation never reached a final count.
- **The sizes describe the operation, not the output.** They are recorded once the operation has finished and its counts are final. If the summary then fails to render or cannot be written to stdout, the sizes are still reported, because work that already happened is not undone by a display failure. Equally, they do not always correspond to a number printed on screen: `--jq` and `--json <fields>` reshape the output, and `validate` prints only failures and skips in JSON.
- **An operation that aborted partway reports nothing.** It never reached a final count, so there is no size to report. Note that `gcx resources delete` with `--on-error=abort` may have deleted some resources before stopping; that partial work is deliberately not reported, so absence consistently means "no final count", never "no work done".
- **The unit depends on the command, so these must not be compared or totalled across commands.** `gcx resources pull` is the clearest case, and its failure count is genuinely mixed: a resource fetched but not written to disk counts as one failure, and a whole resource *type* whose list call fails also counts as one failure. Skips there count whole resource types the server could not list. A pull failure count of 2 can therefore mean two resources or two entire types.
- **`batch_skipped_bucket` means different things per command, and whether it measures anything depends on the run.** In `gcx resources push` and `gcx resources delete` a skip is recorded solely when a dry run cannot be verified server-side, so a run without `--dry-run` reports `0` by construction rather than as a measurement, while a dry run reports a real count. `gcx resources validate` is always a dry run, so its skip count is a genuine measurement on every run, and a non-zero value there is a normal outcome rather than a sign of trouble. In `gcx resources pull` it is also a genuine measurement on every run, counting resource types the server could not list.
- **`dry_run` is not a mutation flag.** `gcx resources validate` always reports `true` and `gcx resources pull` always `false`, yet neither changes anything: pull is read-only. Read `dry_run` together with `command`, never as "this run modified resources".
- **`gcx resources get` never reports these fields**, because only the four commands listed above are instrumented. It is a read, but so is `pull`, which does report.

### Canceled invocations

An invocation that stopped before it finished reports `outcome: canceled` with `exit_code: 5`, and `error_kind` present but empty, because a stop is not a kind of failure. No property is collected specifically for cancellation — and the failure fields described in [Failure and authentication fields](#failure-and-authentication-fields) are never attached to one, for the same reason. Like every other event it is sent on a best-effort basis. For an invocation you interrupted, pressing Ctrl-C a second time ends the process immediately, before the report is sent; an invocation that stopped for another reason waits out the report like any other run, because there is no interrupt for the second Ctrl-C to follow.

Three things this value does *not* tell you:

- **It is not always your Ctrl-C.** Any invocation whose exit code is `5` reports `canceled`, which includes a confirmation prompt you declined and a task the server itself reported as canceled. The field records that the invocation stopped early, not who stopped it.
- **Not every interrupted command reports it.** Commands that treat an interrupt as a clean shutdown — `gcx dev serve`, for example — finish normally when you press Ctrl-C, so they report `ok` with `exit_code: 0` like any other successful run.
- **Only Ctrl-C is caught.** `gcx` installs a handler for `SIGINT` alone. A `SIGTERM` or a `SIGKILL` ends the process at once, before any report is built, so the invocation reports nothing at all. Orchestrators and CI runners usually stop a process with `SIGTERM`, so `canceled` undercounts the invocations that stopped early in those environments.

If your first-ever `gcx` command is one you interrupt, the one-time notice described in [Opt out](#opt-out) is printed after the interrupt, because that invocation does report. The notice comes first and the export is attempted after it, so the notice records the attempt rather than a delivery: as above, the report is best-effort and may never arrive.

This moves the denominator of every outcome rate in two ways, so compare rates within a `version` rather than across the version where `canceled` first appears:

- **Some invocations start being counted at all.** Earlier versions reported nothing for an invocation that ended on the interrupt path — exit code `5` with no error printed. Those now count towards the total, so the share of `ok` invocations falls without anything having got worse. This is narrower than "every Ctrl-C": an interrupt a command turns into a clean shutdown was always reported as `ok`, and one that leaves a batch partially applied was always reported as a partial failure.
- **Some invocations change label.** Exit-code-`5` invocations that *were* already reported — a declined prompt, for instance — moved out of `runtime_error` with `error_kind: error` and into `canceled` with an empty `error_kind`. Both the `runtime_error` share and the volume of `error_kind: error` drop for the same reason, with no change in what happened.

### Failure and authentication fields

For Grafana connections, `gcx` records the authentication category selected, such as `oauth`, `token`, `basic`, `mtls`, `anonymous`, or `unknown`, but never credentials. For some failed commands, it may also record a 4xx/5xx HTTP status or a fixed Kubernetes reason category; these details are omitted for partial failures and cancellations.

| Field | Description | Example |
| :---- | :---- | :---- |
| `http_status` | The HTTP transport status of the failing request, only ever `400`–`599`. Never a status embedded inside a response body, and never a Kubernetes status code. | `403` |
| `k8s_reason` | The Kubernetes status reason of the failing API call, from a fixed vocabulary. Any reason outside the vocabulary is sent as `other`, never verbatim. | `NotFound` |
| `grafana_auth_method` | The authentication category selected for the Grafana connection. Exactly one of `oauth`, `token`, `basic`, `mtls`, `anonymous`, or `unknown`. Never a raw configured value, never credential material. | `token` |

The `k8s_reason` vocabulary is exactly: `Unauthorized`, `Forbidden`, `NotFound`, `AlreadyExists`, `Conflict`, `Gone`, `Invalid`, `BadRequest`, `MethodNotAllowed`, `NotAcceptable`, `RequestEntityTooLarge`, `UnsupportedMediaType`, `Expired`, `Timeout`, `ServerTimeout`, `TooManyRequests`, `InternalError`, `ServiceUnavailable`, `StorageReadError`, plus the `other` sentinel.

These fields are easy to misread, so the following constraints are part of the contract:

- **`http_status` is a transport status, never a body status.** Datasource queries can fail inside an HTTP 200 response, with the real error and its status embedded in the body. Such a failure sets no `http_status` at all. In exactly that case `error_kind` may still say `auth_failure`, because the command classifies and explains errors on the embedded status; the divergence is deliberate and must not be "fixed" by aligning either side.
- **Only failure statuses travel.** A status outside `400`–`599` — a success, a redirect, anything malformed — is omitted, never coerced into the range.
- **Coverage is partial, so absence proves nothing.** `http_status` is recorded for selected provider APIs that use the shared error helper, for datasource query endpoints, and for `gcx api`. GCOM, Fleet, k6, Faro, Adaptive Metrics, the assistant, and other clients are not instrumented, so the absence of `http_status` must never be read as "no HTTP failure occurred".
- **Kubernetes failures report a reason, never a status.** A Kubernetes API error sets `k8s_reason` and leaves `http_status` empty; the status code inside a Kubernetes error never feeds `http_status`.
- **Both failure fields are omitted for exit codes `4` and `5`.** A partial failure has no single causal status — the one that surfaced would stand in for many — and a canceled run is not a failure.
- **`grafana_auth_method` describes Grafana connection authentication only**, never Grafana Cloud/GCOM authentication. Absent means no Grafana context was selected, or the invocation selected several different methods (`gcx config check` examines every context, so it reports no single method when they differ). `anonymous` means a valid selection with no credential material — requests went out unauthenticated. `unknown` means the configured method could not be classified. A selected method whose credential is invalid or rejected reports the method: which authentication category fails is precisely what this field exists to show.
- **`grafana_auth_method` is present on every outcome** — success, failure, partial failure, and cancellation — whenever a Grafana context was selected. `gcx login` reports the method it actually resolved by probing, even when the login fails after authentication was resolved.

### Parse-failure fields

When the invocation fails to parse, these additional fields are set. They capture what was attempted so the team can understand the differences between what users expect and what exists. They are not populated yet: a parse failure currently reports no event at all (see [Invocations that report nothing](#invocations-that-report-nothing)), and `outcome` is never `parse_error` today.

| Field | Description | Example |
| :---- | :---- | :---- |
| `parse_error_kind` | The kind of parse failure: `unknown_command`, `unknown_flag`, or `invalid_args`. | `unknown_command` |
| `parse_error_parent` | The deepest valid command reached before the failure. | `dashboards` |
| `parse_error_token` | The first unknown toke. It's only sent if it looks like a command name (short, lowercase, no digits, not a URL, IP address, or UUID); otherwise it's replaced with `<redacted>`. | `serch` |
| `attempted_command` | The parent command plus the unknown token, truncated at the unknown token so no later arguments are included. | `dashboards serch` |
| `parse_error_flags` | The **names** of unknown flags. No flag values are sent. | `verbsoe` |
| `parse_error_nearest` | The nearest real command or flag name, if one is close. | `search` |
| `parse_error_distance` | The edit distance to the nearest real name, or `-1` if there is no near match. | `2` |

## Invocations that report nothing

Some invocations never emit an event:

- **Shell completion** — the completion machinery runs on every tab-press and carries no usage signal.  
- **`gcx version`**  
- **Invocations that failed to parse** — an unknown command or flag reports nothing today, which is why the `parse_error_*` properties above are not yet populated.

## Server-side enrichment

Reports are received by Grafana's usage-stats service, the same service that receives usage reports from Grafana, Loki, Tempo, and Mimir. On receipt, the service adds two pieces of information derived from the connection:

- A coarse **geographic region** (for example, a country or subdivision), taken from headers added by the CDN edge.  
- The **network organization name** from a whois lookup of the connecting IP address. For CLI traffic this typically resolves to your ISP or employer's network.

The connecting IP address is not stored in the usage event.

## Inspect what would be sent

To see exactly what `gcx` would report for an invocation, set `GCX_TELEMETRY=log`. The event is printed to stderr and nothing is sent:

```shell
GCX_TELEMETRY=log gcx dashboards list
```

## Opt out

You can control usage statistics reporting three ways:

1. **`GCX_TELEMETRY` environment variable**: Set to `enabled`, `disabled`, or `log`. Takes precedence over everything else:

```shell
export GCX_TELEMETRY=disabled
```

2. **`DO_NOT_TRACK` environment variable**:  Set to `1` or `true` to disable reporting, following the cross-tool [DO_NOT_TRACK](https://consoledonottrack.com/) convention. Overridden by `GCX_TELEMETRY`.  
     
3. **Configuration file**: Add a top-level `diagnostics` block to your `gcx` configuration file, with `telemetry` set to `enabled`, `disabled`, or `log`:

```
diagnostics:
  telemetry: disabled
```

Opting out disables reporting entirely. No event is constructed and nothing is sent.
