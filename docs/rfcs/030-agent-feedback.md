# RFC 030: Agent Feedback from the CLI

**Status**: proposed
**Issue**: [#1748](https://github.com/grafana/gcx/issues/1748)

## Summary

Add an experimental `gcx feedback send` command that lets a person or an agent send a short free-text note and a few named fields to the gcx team. gcx scrubs the text for credentials and identifying details before it is printed or sent, attaches install metadata only when usage statistics are enabled, and posts it once to a dedicated endpoint that confirms storage.

## Motivation

Agents run a large share of gcx commands, and when they hit friction the team usually doesn't hear about it. A command doesn't exist, an error doesn't say what to do next, or a flag name is a guess that fails. The agent works around it or gives up, and the session ends.

[Usage statistics](../sources/anonymous-usage-statistics.md) tell us that a command failed, but by design they carry no free text, so they can't say what the agent was trying to do or what would have helped. Today that context reaches the team only when someone goes back through an agent transcript and writes it up by hand, as in #384, #425 and #519.

Several other CLIs now let the agent report this directly, with a feedback command that takes a note plus a few named fields. This RFC proposes the same for gcx.

## Scope

In scope:

- The `gcx feedback send` command, its help text, output and exit codes.
- Client-side scrubbing of the note and fields.
- The wire contract with the receiver, and how it relates to the telemetry setting.
- The `GCX_FEEDBACK_ENDPOINT` override for tests and development.

Out of scope, and deferred (see [Future possibilities](#future-possibilities)):

- Changing the bundled agent skills or error output to suggest sending feedback.
- How received feedback is triaged, routed or turned into issues.
- An MCP tool for feedback.

The receiver is a separate service. This RFC records the contract the client depends on, not the receiver's implementation.

## User experience

An agent tries to list alerts for one team and finds no label filter. It sends:

```shell
gcx feedback send "alerts list can't filter by label" \
  --field type=feature \
  --field command="alerts list" \
  --field task="find the alerts owned by one team" \
  --field expected="a --label flag" \
  --field actual="unknown flag: --label"
```

and gcx prints `✔ Feedback sent`. In agent mode the same invocation emits one JSON value, the shared single-mutation result:

```json
{"type":"gcx.mutation","schema_version":"1","action":"sent","target":{"kind":"feedback","id":"3f0c2a7e-8a51-4c2e-9d3e-6b1f2a9c4d10"}}
```

The rules:

- The note is the one positional argument, or `-` to read it from stdin. `-` with stdin attached to a terminal is a usage error.
- `--field key=value` can be repeated and accepts any key. The help text recommends `type` (`bug`, `friction` or `feature`), `command`, `model`, `task`, `expected`, `actual` and `impact`.
- The note is optional when at least one field is set. A repeated key keeps its last value. Keys or values that are empty or whitespace are dropped. If nothing is left, that is a usage error.
- Passing a note both positionally and as `--field note=...` is a usage error.
- `gcx feedback` alone prints help and sends nothing.
- `--dry-run` prints the exact payload, after scrubbing, and sends nothing.
- No login and no configured context are needed.
- The help text asks people not to include secrets or personal data, and says what is scrubbed.

Error messages never quote what the user typed, because that text is the feedback itself. A note that starts with `-` would otherwise reach the user's terminal inside a flag parse error, so the command replaces those errors with one that says to put `--` before the note.

The command is marked experimental, so its flags and output can change while the receiver and the triage process settle.

## Technical design

### Command placement and verb

The [CLI grammar](../../CONSTITUTION.md#cli-grammar) keeps bare top-level verbs to a closed list, so the command is an `$AREA $VERB` tooling command like `config`. The area is `feedback` and the verb is `send`.

`send` is a new domain verb. In gcx it means: deliver a message to a receiver outside the user's Grafana, in one attempt, with nothing to list, read back, update or delete afterwards. `create` would suggest a resource the user can later address, which feedback is not.

### Code layout

- `internal/feedback` holds the scrubber, the payload, metadata collection and delivery.
- `cmd/gcx/feedback` holds the Cobra wiring, argument handling, the text codec and the mapping to `DetailedError`.

The payload is a separate type on a separate route from the usage event in `internal/telemetry`. The usage event's privacy rules forbid free text, so feedback is never added to it. The usage event for a `feedback send` invocation records the command path and flag names only, like every other command. The two share the telemetry mode, which gates metadata, and the device ID inside that metadata.

The command works in the wasip1 build. Delivery uses `httputils.NewClient`, which reaches the host through `httputils.WireTransport` there, so no build-tagged variant is needed.

### Wire contract

`POST https://stats.grafana.org/gcx-feedback` with `Content-Type: application/json`. `GCX_FEEDBACK_ENDPOINT` overrides the URL. The request body is at most 65536 bytes, and both sides enforce it.

```json
{
  "service": "gcx",
  "feedback_id": "3f0c2a7e-8a51-4c2e-9d3e-6b1f2a9c4d10",
  "data": {
    "note": "The error didn't say which flag was wrong",
    "type": "friction",
    "command": "alerts list"
  },
  "metadata": {
    "version": "0.9.0",
    "os": "darwin",
    "arch": "arm64",
    "is_agent": true,
    "agent": "generic",
    "device_id": "1c9e7a3f-1234-5678-9abc-def012345678",
    "device_id_persisted": true
  }
}
```

- `feedback_id` is a random UUIDv4 per invocation. The receiver uses it for best-effort deduplication.
- `data` maps string keys to string values, with at least one entry. The positional note is stored under `note`.
- `metadata` is `null` unless the telemetry mode resolves to `enabled`. `disabled` and `log` both send `null`. The mode follows the usual precedence: `GCX_TELEMETRY`, then `DO_NOT_TRACK`, then `diagnostics.telemetry` in config, then the default. The device ID is read, and created on first use, only on the enabled path. `agent` uses the same labels as the usage event, including `generic` when agent mode is on without a known identity.

Response semantics:

| Response | Meaning | gcx reports |
| :---- | :---- | :---- |
| `200` with `{"success":true}` | Stored | Success |
| `400` | Malformed request | `Feedback could not be submitted (HTTP 400). Try again later.` |
| `413` | Body over 64 KiB | Same, with the status |
| `429` | Rate limited | Same, with a note that too many requests were sent |
| Network error, any `5xx`, or `200` without a confirmation | Unknown. A `5xx` can follow a stored row, for example when the storage acknowledgement is lost or a proxy fails after the handler replied | `Could not confirm that the feedback was received. It may have been submitted. gcx did not retry.` |

gcx makes a single attempt with a ten second timeout, follows no redirects and never retries. A redirect is reported as a rejection with its status. Rejections and unknown outcomes exit 1, and both print the `feedback_id` so a report can be matched to a stored row. Usage errors, including oversized input, exit 2 and send nothing.

The size check runs on the encoded body after scrubbing, since placeholders can make the text longer. Stdin is read only up to one byte past the limit, so a large stream is rejected without being buffered.

### Scrubbing

Every key and value is scrubbed before `--dry-run` prints it and before it is sent. The receiver scrubs again with rules of the same intent. Matches are replaced with `<redacted:KIND>`:

- `url`: every `http` or `https` URL, whole. Paths can carry dashboard UIDs and stack names, and the host can name a stack or a company.
- `userinfo`: credentials in other URLs, such as `postgres://user:pass@host`.
- `auth`: `Authorization` header values and `Bearer` or `Basic` credentials. A credential must look like one (a digit, base64 punctuation or mixed case), so prose such as "basic authentication" survives.
- `token`: Grafana Cloud access policy and service account tokens (`glc_`, `glsa_`), JWTs, and the token formats of common source hosting, chat and cloud providers.
- `secret`: values of `key=value`, `key: value` and `--key value` pairs whose key contains `token`, `secret`, `password`, `passwd`, `api_key`, `access_key` or `private_key`, in any case and with any prefix, such as `GRAFANA_TOKEN=`.
- `email`: email addresses.
- `host`: bare Grafana Cloud stack hostnames under `grafana.net`, `grafana-dev.net` and `grafana-ops.net`.
- `ip`: IPv4 and IPv6 addresses. IPv6 candidates must parse as addresses, so clock times and MAC addresses survive.

Home directory paths under `/Users/<name>`, `/home/<name>` and `C:\Users\<name>` become `~`.

The rules run in a fixed order: whole URLs first, token formats before key=value secrets, emails before hostnames, IPv4 before IPv6. Scrubbing is pattern based and over-redacts rather than under-redacts. It is a safety net, not a guarantee, which is why the help text and the docs ask people not to include secrets at all.

## Drawbacks

- Free text can carry anything. Scrubbing lowers the risk but cannot remove it, and a determined or careless sender can still include identifying details in prose.
- The command sends data even when usage statistics are disabled. That is intended, because the user asked for it explicitly, but it is a second data path that the usage statistics page now has to explain.
- An unknown outcome can lead people to resend, which creates duplicates. The `feedback_id` lets the receiver drop exact retries of one invocation, but not a second invocation with the same text.
- Over-redaction can remove useful context, for example a version string that looks like an IPv4 address, or prose after `token:`.

## Rationale and alternatives

- **Bare `gcx feedback`.** Shorter, but the constitution keeps bare top-level verbs to a closed list. Adding one would need a constitution amendment for a command that fits `$AREA $VERB` well.
- **Open a GitHub issue from the CLI.** It needs a GitHub account and token, publishes the text, and puts the triage burden on the sender. Most agent sessions have no GitHub credential, and the text would be public before anyone could check it for sensitive content.
- **An MCP tool.** It would reach only agents that run gcx through MCP. A CLI command reaches every agent and person, and an MCP tool can call the same internals later.
- **Put feedback in the usage event.** That would break the usage event's promise of carrying no free text, and make every opt-out decision about both.
- **Send metadata always.** It would help triage, but people who opted out of usage statistics expect no device ID or environment details to leave their machine. Tying metadata to the existing setting avoids a second switch.
- **Retry on failure.** A retry after an unknown outcome can store the feedback twice, and a slow retry delays the agent. One attempt with an honest message is simpler and cheaper.

## Prior art

- gcx usage statistics: the mode resolution, the device ID, the single-attempt export and the endpoint override all come from `internal/telemetry`.
- Several other CLIs added a feedback command that an agent can call with a note and named fields. This design follows that shape.

## Unresolved questions

- Which fields the team actually uses in triage, and whether the recommended list should change. Resolved by reviewing the first weeks of received feedback.
- When the command leaves experimental. Resolved once the receiver and triage process are stable.

## Future possibilities

- Mention `gcx feedback send` in the bundled agent skills, and add a short nudge to error output so agents know they can report friction.
- Route received feedback to triage, for example by opening issues after a human review.
- Expose the same send path as an MCP tool.

## Terminology

- **Note**: the free-text body of a feedback request, stored under `data.note`.
- **Field**: a named `key=value` pair passed with `--field`.
- **Metadata**: install details attached only when usage statistics are enabled.
- **Unknown outcome**: gcx sent the request but could not confirm that the receiver stored it.
