# Design

## Context

See [proposal.md](proposal.md) for scope and [RFC 002](https://github.com/grafana/gcx/blob/main/docs/rfcs/002-assistant-watchers.md) for settled cross-step decisions. The existing Assistant MCP resource supplies the transport, lazy configuration, typed envelope and identity precedent. The shared puller records per-resource failures, but its resource collection replaces entries with identical identities. Collision detection must therefore happen before collection insertion, not only before file writing.

Backend source has been inspected for collection paging, archive filtering, access checks, configuration, secret-presence reporting, checks and calibration reads. Deployed behavior remains unverified until throwaway-stack e2e. API mapping and the exact inventory of excluded response fields are kept in a private working note outside this checkout; no private API names belong in these artifacts.

## Goals / Non-Goals

**Goals:** Fix one manifest format that later writes can consume unchanged; share configuration reads across both command paths; distinguish missing observations from defaults; retain collision candidates until they can be reported.

**Non-Goals:** Add writable checks, secret resolution, mutation callbacks, calibration requests, new authentication, or a framework for future Assistant features.

## Decisions

### 1. Extend the Assistant provider with shared typed reads

Use `internal/assistant/watchers/` for the product client, `internal/assistant/watcher/` for the manifest and typed adapter, and `internal/providers/assistant/watchers/` for commands. Reuse `providers.ConfigLoader` and `assistanthttp`. Register Watcher as the second typed resource through `TypedRegistrations()`, with schema, example, plural `watchers` and singular `watcher`. Do not introduce another provider or auth path.

Dedicated `list` and `get` use the same TypedCRUD as generic resource reads. Serialize through the shared unstructured envelope projection: current typed wrapping does not merge extra metadata, so returning the typed object directly would lose the ID annotation. Status is an extension using the resolved identity and product reads. Unset create/update/delete callbacks; preserve unsupported-operation identity through wrapping and translate it into a Watcher-specific “not supported yet” recovery message at the generic operation boundary if necessary. Tests must drive actual push/delete paths and assert no mutation requests. Returning success or pretending that this is a writable resource is excluded.

### Provider discovery across versions

A discovered group can prefer a version containing different kinds from registered provider resources. Preserve the native group preference. For an unspecified version, resolve the preferred native kind first; only when it is absent, use the first registered static descriptor for that group/kind. Apply the same resolution to unqualified selectors and preferred-resource enumeration. Explicit versions remain exact and cannot fall back. Test native-kind preservation, registered kinds in a different version, duplicate prevention and registration precedence through registry and real resource command paths.

### 2. Lock the complete configuration schema

The schema is strict and configuration-only. The table lists the full RFC field set; a disabled block can be omitted in hand-authored input, while a successful full export materializes its effective settings.

| Manifest field | Type and rules |
| --- | --- |
| `title` | Required nonblank string; display title and natural key |
| `description` | String; empty default |
| `prompt` | Required nonblank string |
| `datasourceUids` | Array of strings; empty default |
| `interval` | Positive whole-second duration within the target's supported range; use shared duration parsing; effective server value on read |
| `sensitivity` | `sensitive`, `balanced`, `relaxed`; default `balanced` |
| `skipReviewOnCleanRuns` | Boolean; default true |
| `autoStop.enabled` | Boolean; default false |
| `autoStop.at` | RFC 3339 timestamp with explicit offset; required when enabled; omit when no deadline is configured |
| `autoStop.archive` | Boolean; default false; cannot enable archiving without an enabled stop |
| `labels` | Map of strings; empty default; distinct from metadata labels |
| `automaticRecalibration.enabled` | Boolean; default false for future input, observed enrollment on read |
| `notifications.slack.enabled` | Boolean; default false |
| `notifications.slack.channelId` | String; required when enabled |
| `notifications.slack.severity` | `warning-and-critical` or `critical`; default former |
| `notifications.teams.enabled` | Boolean; default false |
| `notifications.teams.channelId` | String; required when enabled |
| `notifications.teams.severity` | Same severity choices and default |
| `notifications.alerting.enabled` | Boolean; default false |
| `notifications.webhook.enabled` | Boolean; default false |
| `notifications.webhook.severity` | Same severity choices and default |
| `notifications.webhook.url` | Secret input; required when enabled |
| `notifications.webhook.bearerToken` | Optional secret input |
| `notifications.webhook.signingSecret` | Optional secret input |
| `investigation.enabled` | Boolean; default false |
| `investigation.teamAccess` | Array of strings; empty default |

Each secret input is an object with exactly one of nonempty `fromEnv`, nonempty `fromFile`, `preserve: true`, or `clear: true`. Unknown keys, empty references, false markers, literal secret values and multiple alternatives are invalid. This slice defines and validates the shape but never reads environment/file secret sources. Exports emit only `preserve: true` for values the server confirms are configured, even for disabled destinations; absent values have no marker. Redacted display strings and any unexpectedly returned values are never copied. An enabled webhook rejects `url: { clear: true }`; supplying a URL-shaped secret object alone does not satisfy that dependency.

Enrollment is read separately using bounded concurrency. A known-disabled response exports false; denied, unavailable or failed reads must not become false. A full manifest export fails explicitly when that modeled setting cannot be established. This is preferable to silently generating a file that later disables a setting. No “unknown” sentinel is added to the final manifest. Status can still report independent observations with explicit sub-read availability.

Settings outside the RFC model are not added to spec. Public prose describes their categories only: calibrated checks and context, learned notes and tracked issues, log-classification settings, additional destination/routing settings, legacy configuration, runtime history/usage, archive/calibration/audit observations, and internal version bookkeeping. The private note lists every excluded API field; step 2 decides which require write refusal. This slice neither declares them safe to overwrite nor adds opaque configuration passthrough.

### 3. Share deterministic identity resolution

Use the MCP slug convention applied to the full title for `metadata.name`, with the original title retained in spec. Keep the server ID in `assistant.ext.grafana.app/watcher-id`; it is same-stack addressing, not portable identity. A recognized server ID has precedence; otherwise compare the computed resource name exactly. Names and IDs work in both dedicated and generic get/pull selectors. No first-page or first-match lookup is permitted.

Name resolution searches both archived and non-archived visible collections, preventing lifecycle state from hiding a collision. Explicit ID reads remain usable for archived Watchers. Default bulk reads select non-archived resources; dedicated `list --archived` selects archived only. Distinct titles with the same slug are collisions too. Do not append IDs to bulk-export names or silently rename resources.

Lists retain all colliding entries with IDs. Pull must reject every member of a colliding group while writing unaffected resources. The minimal shared integration is a preflight in the puller's fetched item batches, before insertion into the identity-keyed collection: use a Watcher-owned identity-only index across both visible archive partitions to identify every candidate for selected resource names, and record each rejected selected item using the existing failure summary. This extra identity read must not fetch supplementary configuration for archived candidates that are not being exported; errors building the index are explicit failures, never permission-blind guesses. Respect existing `--on-error` behavior. Keep Watcher logic under its owned directories; shared glue remains gcx-owned. A broader adapter-hook registry or generic writer redesign is unnecessary for this slice. An explicit single-ID pull has no bulk name collision and exports its chosen resource normally.

### 4. Complete collection reads and honest coverage

Exhaust cursors until absent, including empty intermediate pages. Detect repeated cursors, propagate cancellation and reject failed pages without returning a seemingly complete list. No new CLI paging or search flags: `list` consumes the entire selected archive partition. Use an `items` envelope, initialized to `[]`, with coverage metadata describing the archive partition and caller-visible scope; do not claim a tenant-wide count or snapshot consistency. Human output shows name, ID and title; wide output adds useful configuration columns through codecs.

`get` JSON/YAML/agents emits the standard manifest envelope and matches generic get. Output format never changes which configuration is fetched. List metadata is for coverage, not fabricated total counts. Agent codecs handle large payloads without client-side truncation; mark list's worst-case token cost large and provide narrowing guidance toward get.

### 5. Status reports observations with their actual meaning

Status includes name/ID, lifecycle, archive observation when present, last reported assessment, run timestamps, server-reported token estimate with sample context, creator/timestamps, current definition version, calibration progress/context and checks. Keep lifecycle distinct from assessment; unknown lifecycle/calibration values remain visible without mapping them to healthy or ready.

Use the RFC public status vocabulary. Do not equate a generic latest-run timestamp with completion, fabricate an assessment observation time, invent check titles, or derive completed calibration from stored checks. Omit unavailable start/completion timestamps and check titles. A next scheduled time is included only when reported. Estimate values are observations, not guarantees or limits.

Calibration reads are independent of lifecycle. If that sub-capability is unavailable, status marks calibration unknown/unavailable and retains other observations. Denied access is identified distinctly; transport or server failures are explicit sub-read failures, not idle. Include guidance to continue in Grafana when calibration fails or needs input. No read queues work.

Checks retain ID, enabled state, source, public query type, expression, interpretation guidance and mapped parameters. Known default-enabled semantics can be normalized. Unknown query types and parameters are represented as unsupported with their information retained in status, never silently dropped or copied into spec. Keep translation and raw response names in Go client code; prose describes check properties only.

### 6. Command and error contract

All four nodes (`watchers`, `list`, `get`, `status`) have the experimental short/long prefixes and stability annotation. Leaves have opts/setup/Validate, argument validators, codecs, output classes and token-cost annotations. List accepts no positional arguments and a boolean `--archived` (false: current fleet, true: archived only). Get/status require one nonempty WATCHER. No mutations or confirmation flags are introduced.

All leaves are finite outputs; generic pull retains its artifact receipt. Machine outputs emit one result or one error document. Empty list is success, denied access is permission failure, a missing ID/name is not-found, and missing collection capability is explicitly unavailable. Preserve typed error identity; do not classify by parsing rendered transport error text. Collection unavailability must not become the generic puller's silent not-listable skip. Tests pin existing exit-code conventions, including the documented Cobra usage-error limitation rather than claiming an unwired code.

Nearest sibling: `assistant mcp-servers` reads integration configuration; `assistant watchers` reads recurring telemetry-monitoring definitions. Status is for current runtime observations; get is for reusable configuration. Metadata and examples explain that distinction.

## Risks / Trade-offs

- Public-preview drift → isolate mapping in the client and verify against the deployed throwaway stack after login.
- Additional permissions or unavailable enrollment reads can block full exports → fail explicitly; verify this early in e2e rather than silently defaulting.
- Collection churn during pagination → report caller-visible coverage without claiming an atomic snapshot.
- Shared puller deduplicates identities → perform collision checks before insertion; test different pages and slug collisions through the real pull path.
- Unmodeled configuration makes exports incomplete representations of some Watchers → describe export scope; retain the private exclusion inventory for step 2's safety decisions, with writes still disabled here.
- Large collections require extra configuration reads → bounded errgroup concurrency with shared cancellation; no new dependencies or transport.

## Migration Plan

This adds an experimental surface and one resource registration. Existing commands and manifests retain their behavior. Rollback removes the new command wiring and registration. Approval of these artifacts precedes implementation. Later write support must consume the exact schema above without changing pulled file format.

## Open Questions

No unsettled product decision is being reopened. Deployed read availability and permissions remain a verification item, with failure behavior specified above. The RFC is read from `origin/main` because the requested planning base predates it; the public link remains canonical.
