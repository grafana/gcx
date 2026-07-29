---
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

The statistics describe only the *shape* of usage, including command path, and flag names. Positional argument values and flag values are never sent. The one place a value is used at all is the `api` command, where the requested route and the datasource plugin type are first reduced to fixed vocabularies built into the binary, so only known, non-identifying names can ever be sent - see [The api command](#the-api-command) for details. Some server-side enrichment is also performed on the usage statistics exported - see [Server-side enrichment](#server-side-enrichment) for details.

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
| `flags` | The **names** of the flags you set, sorted. No flag values are sent. | `dry-run,folder` |
| `provider` | The resource provider the command belongs to. | `dashboards` |
| `outcome` | How the invocation ended: `ok`, `runtime_error`, `parse_error`, or `help`. | `ok` |
| `exit_code` | The process exit code. | `0` |
| `error_kind` | A coarse error category when the command failed: `usage_error`, `auth_failure`, `partial_failure`, `version_incompatible`, or `error`. Never an error message. | `auth_failure` |
| `duration_ms` | Total invocation duration in milliseconds. | `1234` |
| `is_tty` | Whether `gcx` ran attached to an interactive terminal. | `false` |
| `is_ci` | Whether a CI environment was detected. | `true` |
| `ci_provider` | Which CI system was detected, from a fixed list of known names. `gcx` reads well-known CI environment variables to detect the provider but never sends their values. | `github_actions` |
| `is_agent` | Whether an AI coding agent drove the invocation. | `true` |
| `agent` | The name of the agent harness, if one was detected. | `claude-code` |
| `target_kind` | Whether the target Grafana is `cloud` or `self-hosted`. Empty when no effective Grafana target could be resolved. Deliberately coarse — never the URL, hostname, or stack slug. | `cloud` |
| `output_format` | The output format the command used. | `table`, `json` |

When the invocation fails to parse, these additional fields are set. They capture what was attempted so the team can understand the differences between what users expect and what exists:

| Field | Description | Example |
| :---- | :---- | :---- |
| `parse_error_kind` | The kind of parse failure: `unknown_command`, `unknown_flag`, or `invalid_args`. | `unknown_command` |
| `parse_error_parent` | The deepest valid command reached before the failure. | `dashboards` |
| `parse_error_token` | The first unknown toke. It's only sent if it looks like a command name (short, lowercase, no digits, not a URL, IP address, or UUID); otherwise it's replaced with `<redacted>`. | `serch` |
| `attempted_command` | The parent command plus the unknown token, truncated at the unknown token so no later arguments are included. | `dashboards serch` |
| `parse_error_flags` | The **names** of unknown flags. No flag values are sent. | `verbsoe` |
| `parse_error_nearest` | The nearest real command or flag name, if one is close. | `search` |
| `parse_error_distance` | The edit distance to the nearest real name, or `-1` if there is no near match. | `2` |

### The api command

`gcx api` is a raw passthrough: the interesting usage signal (which endpoint, which method) lives in its argument values, which are never sent as-is. Instead, three additional fields carry derived values, each filtered through a fixed vocabulary built into the binary before anything is recorded. We use them to learn which endpoints and datasources people reach for through the raw passthrough, so we know which first-class `gcx` commands to build next.

| Field | Description | Example |
| :---- | :---- | :---- |
| `api_method` | The HTTP method, from the fixed list of valid verbs. | `POST` |
| `api_route` | The requested route, matched against a built-in table of known Grafana API route templates. Variable segments such as UIDs and names are replaced with placeholders, and a path that matches no known route is sent as `other`. The raw path is never sent, and the query string is discarded before matching. | `/api/dashboards/uid/{uid}` |
| `api_datasource_types` | For datasource query requests only: the datasource plugin types named in the request body. A type is sent only when it appears on a fixed list of Grafana-authored plugin IDs built into the binary (core datasource types plus Grafana-published catalog plugins). Any other type, including third-party and private plugins, is sent as `other`. The body is used only to read the `queries[].datasource.type` field. The datasource UID and name, the query text, the time range, and everything else in the body are never sent. | `grafana-postgresql-datasource` |

You can verify all of this for any invocation with `GCX_TELEMETRY=log` (see [Inspect what would be sent](#inspect-what-would-be-sent)).

## Invocations that report nothing

Some invocations never emit an event:

- **Shell completion** — the completion machinery runs on every tab-press and carries no usage signal.  
- **`gcx version`**  
- **Cancelled invocations** — pressing Ctrl-C emits nothing.

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
