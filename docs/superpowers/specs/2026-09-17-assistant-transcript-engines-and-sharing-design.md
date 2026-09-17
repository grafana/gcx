# Assistant transcripts across engines and shared links

Status: **implemented locally; backend rollout dependency remains**. Date: 2026-09-17.

The user approved URL-aware resolution with an ordinary-then-shared cascade for
bare IDs. No `--shared` flag or parallel endpoint probing is required. Backend
verification requirements below remain prerequisites to implementation readiness.

## Problem and evidence

An external coding agent should be able to read an Assistant conversation using
`gcx assistant conversation get`, including an AI SDK conversation opened through
a shared link. Today two independent assumptions prevent that:

1. The command fetches `/chats/{id}` followed by `/chats/{id}/all-messages`.
   Assistant restricts the latter endpoint to the legacy engine.
2. A shared URL identifies a shared snapshot. Its ID must be read through
   `/shared/{id}`, not treated as an ordinary caller-owned chat ID.

Evidence collected in this task on September 17:

- [Original report](https://raintank-corp.slack.com/archives/C0AHTU3ELH5/p1789571731533829).
- Installed GCX v1.3.0 returned `chat not found` for the reported shared ID.
- Reading that ID through the plugin proxy's `/api/v1/shared/{id}` returned
  `engine: aisdk`, `isPublic: true`, a distinct `parentChatId`, and empty legacy
  `messages`.
- `/api/v1/shared/{id}/ui-messages` returned 12 messages, with 5 user and 7
  assistant turns. The backend already supports this use case.
- GCX main fetched during the investigation still hardcodes `/all-messages`.
- [Assistant PR #10427](https://github.com/grafana/grafana-assistant-app/pull/10427)
  merged September 16 and adds an engine-aware Cloud MCP transcript reader.
  It uses ordinary `/chats/{id}` routes; it does not establish shared-link support
  or deployment availability for that tool.

No transcript contents or credentials belong in committed fixtures. Use synthetic
messages and placeholder stack names. These observations establish the reported
case, not every authentication mode or deployment's capabilities.

## Placement and governing constraints

- **Necessity:** extend the existing `assistant conversation get` leaf. No new
  provider, resource adapter, or parallel export command is warranted.
- **Wiring:** existing Assistant client and provider config/auth resolution.
  Domain parsing and endpoint selection stay in `internal/assistant`; Cobra
  binding stays in `internal/providers/assistant`.
- **Readiness:** shared AI SDK retrieval through the plugin proxy is verified.
  Backend source verification found that the direct CLI OAuth allowlist omits
  `/api/cli/v1/shared`; shared OAuth retrieval requires an Assistant fix/rollout.
  Legacy shared metadata embeds messages from the user audience. UI and legacy
  message reads are unpaginated and ordered by stored sequence in current source.
  Assistant owns those API/access contracts; GCX owns selecting and rendering them.
- **Constitution:** preserve released command paths, ID invocations, and legacy
  output fields; fetch the same data for every output format; use existing codecs,
  auth refresh, and HTTP infrastructure. No new dependency is proposed.
- **Vision:** this directly supports moving Grafana context into coding agents.
- **Design:** finite structured output, actionable errors, no implicit writes.
- **Architecture:** no cross-provider imports or general transport migration.

The referenced `docs/reference/spec-mental-model.md` was not found in this checkout
or the searched parent documentation. This document uses the superpowers spec
location. It records the approved design, not an execution plan or ratified ADR.

## Options

| Approach | Benefits | Costs |
|---|---|---|
| **Extend the current reader with engine and shared-reference dispatch (recommended)** | Fixes the reported workflow using existing APIs; retains the command and output model | GCX must understand a small AI SDK read schema and validate URLs |
| Add a unified transcript endpoint in Assistant first | Backend owns normalization and future engines | Requires backend work and rollout despite working read endpoints; still needs GCX URL handling |
| Direct users to Cloud MCP or raw `gcx api` | Immediate workaround for some cases | Leaves GCX's documented transcript command broken; MCP's merged reader does not cover shared URLs |

Use the first approach with a small local wire model. Do not introduce an engine
plugin registry or take a dependency on the full AI SDK runtime.

## Proposed user contract

Keep existing ID invocations and accept shared URLs. Users need not identify the
conversation kind:

```sh
gcx assistant conversation get <conversation-id> --context <my-context> -o json
gcx assistant conversation get <shared-id> --context <my-context> -o json
gcx assistant conversation get \
  'https://example.grafana.net/a/grafana-assistant-app/chats/shared/<shared-id>' \
  --context <my-context> -o text
```

A shared URL routes directly to `/shared/{id}`. A bare ID first resolves through
`/chats/{id}`; only a 404 triggers a second metadata request to `/shared/{id}`.
An ordinary result takes precedence if both routes could succeed. No fan-out:
ordinary reads need no extra request, and precedence stays deterministic. Stop on
400, 401, 403, timeout, cancellation, or server errors rather than masking them
with a second route. If both metadata routes return 404, report
`conversation not found or inaccessible`.

This fallback resolves reference kind only. After metadata succeeds, dispatch by
engine; a message-read failure never triggers another reference or engine probe.

The first change only promises the verified shared URL shape. Other full URL
paths receive an actionable unsupported-URL error; ordinary chats remain available
by ID. Broader Assistant workspace/mobile URL support is a separate decision.

Parsing requirements:

- Accept a nonempty opaque single-segment ID; do not impose a new UUID-only rule
  on existing callers. Reject path separators, traversal, control characters,
  and malformed URL-shaped input. Encode IDs as path segments.
- Accept the shared URL under the configured Grafana base path. Ignore its query
  and fragment as navigation state, never as API parameters. Reject embedded
  credentials and malformed or additional path segments.
- Compare URL origin and base path against the resolved Grafana server. A mismatch
  tells the user to select the matching context. Do not switch context, fetch the
  supplied URL, or send credentials to its host. Compare against the Grafana
  origin, not the configured Assistant proxy endpoint.
- Syntax validation precedes I/O; context-origin validation follows config
  resolution but precedes transcript requests.

This is a read operation. It must not import a shared chat, create a copy, send a
prompt, or follow `parentChatId` to fetch additional private history. The existing
shared-metadata GET increments a server-side view counter and emits a view event;
read-only here means no conversation-content mutation, not storage-pure backend
execution.

## Data flow and endpoint selection

```text
Bare ID or shared URL
          |
    parse reference ---- validate selected Grafana context
          |
    shared URL: /shared/{id}
    bare ID: /chats/{id} -- only on 404 --> /shared/{id}
          |
    reference kind + metadata.engine
          |
    select reader ---- normalize message representation
          |
    ConversationTranscript ---- existing output codecs
```

Paths below are relative to the Assistant API base chosen by the existing client:

| Reference | Metadata | Legacy messages | AI SDK messages |
|---|---|---|---|
| Resolved ordinary ID | `/chats/{id}` | `/chats/{id}/all-messages` | `/chats/{id}/ui-messages?thread=main` |
| Resolved shared ID/URL | `/shared/{id}` | Embedded shared-response messages, **verified in backend source** | `/shared/{id}/ui-messages?thread=main` |

Read metadata once after successful resolution (at most two metadata requests for
a bare ID). Treat an absent engine as legacy for older-server compatibility;
recognize `legacy` and `aisdk`; reject an unknown nonempty engine with an explicit
unsupported-engine error. Never interpret a 400, 401, 403, or 404 as an engine
discovery mechanism.

The client currently chooses either the Grafana plugin resource base or the
configured Assistant endpoint's `/api/cli/v1` base. Preserve that selection and
fresh-token behavior. Verify all newly used routes under both supported auth
paths. A missing route must not trigger a speculative transport fallback. If the
direct CLI surface lacks a required route, resolve that with the Assistant owner
or explicitly narrow the first release before implementation.

Use one transcript orchestration operation in `internal/assistant` so the provider
does not own engine dispatch. Keep existing low-level message readers usable by
their current callers; do not silently change unrelated investigation or prompt
semantics as part of this fix.

## Output and completeness

Preserve the top-level `{chat, messages}` structure and existing legacy message
fields. Add optional engine and shared-origin metadata only where needed to
identify the result. `chat.id` is the object actually read, including the snapshot
ID for a shared result; do not replace it with the parent ID.

For AI SDK responses, normalize `id`, `role`, `created`, and text parts into the
existing message fields and text content blocks. Preserve each original parts
array in an additive `parts` field using raw JSON, including tool, file, data, and
unknown parts. This avoids pretending a prose projection is a full-fidelity tool
transcript. Do not synthesize legacy tool-result blocks or tool execution states.

Text output uses the existing visible user/assistant prose policy and context-tag
stripping. JSON/YAML preserve the returned parts, including their original text;
formatting does not change the fetch. Preserve server message/part order and
timestamps; do not sort by IDs or manufacture timestamps. Empty message arrays
serialize as `[]`.

For AI SDK chats this first version reads the server-visible **main thread**, not
every subagent thread or hidden storage row. Say that in help and additive scope
metadata for AI SDK results. Legacy `/all-messages` behavior remains unchanged.
Do not introduce client-side limits or claim a full storage dump. Verify whether
the endpoints paginate or cap results; follow documented pagination or disclose
any bounded result before release. Treat unavailable completeness guarantees as
an implementation readiness issue, not as evidence that the result is complete.

Unknown parts are retained in structured output; they are not prose. A text result
containing only non-text parts must clearly say that no displayable prose is
available and suggest JSON, rather than implying the conversation is empty.

Reading does not prove resumability. Replace unconditional continuation advice in
the command and agent hint with language that distinguishes transcript retrieval
from `assistant prompt --context-id`. Shared snapshots are read-only in this
workflow. Continuation of AI SDK chats through A2A is outside this design.

## Errors

- Invalid reference or context mismatch: fail before transcript requests, with
  the accepted forms and a corrected invocation using placeholder values.
- 401/403: preserve authentication/authorization classification through existing
  error conversion. Do not retry through shared or parent routes.
- Metadata 404 for a bare ID: try the shared metadata route once. If that also
  returns 404, report `conversation not found or inaccessible`. A shared URL 404
  fails directly. Message-read 404 fails without fallback. Do not assert deletion
  when the backend may conceal authorization failures.
- Unknown engine, missing endpoint, malformed envelope or malformed message data:
  fail explicitly. Never return a successful empty transcript on decode failure.
- Cancellation and timeouts: retain the existing client and command behavior.
  No unbounded retries or transcript contents in diagnostics.

## Code boundaries

| Location | Intended responsibility |
|---|---|
| `internal/providers/assistant/conversation.go` | Accept the ID or URL reference, call transcript orchestration, encode result; update examples and help |
| `internal/assistant/client.go` | Authenticated transcript orchestration using existing token refresh and endpoint selection |
| New `internal/assistant/conversation_reference.go` | Parse ID/shared URL and validate against selected Grafana origin/base path |
| `internal/assistant/api.go` and a focused AI SDK reader file if needed | Metadata/shared/UI-message HTTP reads and typed envelopes; retain legacy reader |
| `internal/assistant/types.go`, `transcript.go`, `transcript_codec.go` | Additive engine/shared/scope metadata, AI SDK normalization and honest text empty states |
| Corresponding package tests, `internal/providers/assistant/conversation_test.go` | Wire-contract tests and actual Cobra invocation regressions |
| `internal/agent/command_annotations.go`, generated CLI references, relevant portable skills | Correct routing hints, accepted inputs, scope and continuation advice |

Reuse the existing Assistant client's transport. The separate `assistanthttp`
package uses a different config/transport path; consolidating both clients is not
a prerequisite and would widen this change unnecessarily.

## Validation and acceptance

Use table-driven tests at each boundary, without duplicating transport assertions
in every CLI test:

1. Reference parsing: opaque ID, shared URL, base path, query/fragment,
   origin/port mismatch, userinfo, encoded separators,
   malformed/unsupported URLs. Invalid input makes no transcript request.
2. Endpoint selection: ordinary legacy, absent engine, ordinary AI SDK, shared AI
   SDK, verified legacy shared shape, unknown engine. Assert method, path, query,
   request order, token refresh, and absence of import/parent requests. Pin the
   single 404 metadata fallback for bare IDs, direct shared-URL routing, ordinary
   success precedence, both-404 errors, and no fallback on other failures or on
   message-read errors. Neither metadata route may run in parallel.
3. Message fidelity: multiple text parts, interleaved tool/data/file parts, unknown
   parts, non-text-only messages, empty arrays, API order, timestamps, context tags,
   malformed responses. Existing legacy JSON and text regressions remain green.
4. Errors: auth failures, inaccessible snapshot, unsupported route, cancellation,
   timeout. Ensure no success-shaped transcript leaks after a failed read.
5. Cobra: an actual shared URL invocation and bare shared-ID invocation reach the right
   endpoints; JSON/YAML/text and agent default fetch identical data. Verify field
   selection and finite output through the existing codec/conformance machinery.
6. Live, read-only smoke checks with authorized synthetic fixtures: both engines,
   ordinary and shared references, plugin and direct CLI auth paths. Record
   untested combinations as skipped. Do not commit real transcript payloads.

Acceptance: the original shared AI SDK use case works through the dedicated
command; ordinary AI SDK transcripts work; legacy invocations remain compatible;
result scope is explicit; no new mutation or continuation behavior is introduced.

After implementation, run focused Assistant/provider tests, the root metadata and
output conformance checks, regenerate references, and run
`GCX_AGENT_MODE=false mise run all` plus the documented doc-maintenance gate before
PR publication. This draft itself makes no claim that implementation tests ran.

## Approved decisions and remaining verification

- **Input surface:** `get <id-or-url>`, with no `--shared` flag. Shared URLs resolve
  directly; bare IDs use ordinary metadata first and shared metadata only on 404.
- **Structured output:** text normalization plus additive raw `parts`; preserve
  data and the released envelope, defer richer tool rendering.
- **Scope:** main-thread AI SDK retrieval with explicit scope metadata. All-thread
  export and A2A continuation are separate work. Shared snapshots remain visibly
  identified in the result without requiring users to classify input IDs.
- **Backend verification:** before writing an execution plan, verify direct CLI
  route/auth parity, legacy shared payload semantics, and endpoint completeness
  with the Assistant API owners. The verified shared plugin-proxy case remains
  the concrete regression target.

Proposed delivery is one focused GCX PR after verification, unless route parity
requires a separate backend prerequisite. Approval settles the design; GCX implementation is underway. External publication
has not started. Shared OAuth retrieval remains dependent on the Assistant CLI
allowlist rollout; GCX preserves a denial rather than bypassing it.

## Implementation readiness evidence (September 17)

Assistant main `77dd63412c225ee09514d227184e58b5cdcd5d97` retains the relevant
contracts checked in the sibling checkout. `chat.API.Register` mounts the shared
GET routes on the CLI API, but `cliScopeMappings` omits the shared prefix and
`CLIScopeMiddleware` denies unmapped paths. The smallest backend prerequisite is
an explicit `assistant:chat` mapping for shared reads with scope regression tests.
No GCX transport fallback is introduced.

Legacy shared metadata uses flattened `ChatResponse` fields and embedded
`Message[]`, populated by `GetChatMessages(..., "user")`; it has no audience or
sequence fields in the wire message. AI SDK UI messages and legacy shared messages
are ordered by sequence and not paginated/capped in the verified backend source.
An authorized live check against the `ops` deployment on September 17 confirmed
the distinction: an ordinary legacy conversation succeeded, while both a shared
ID and the equivalent shared URL returned the typed Assistant HTTP 403 path. The
backend allowlist fix and rollout remain required before direct CLI OAuth can read
shared transcripts. No ordinary AI SDK conversation was available for a live
check; synthetic route fixtures cover that reader locally.
