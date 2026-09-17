# Assistant Transcript Retrieval Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan. Steps use checkbox syntax for tracking.

**Goal:** Read legacy and AI SDK conversations by ID or shared URL without a shared flag.

**Architecture:** Extend the existing Assistant client with reference parsing and one transcript orchestration operation. Resolve bare IDs through ordinary metadata then shared metadata only on 404; choose the message reader from engine metadata. Preserve existing output and add raw AI SDK parts and explicit scope.

**Tech Stack:** Go, Cobra, existing Assistant HTTP/auth infrastructure, stdlib JSON and URL parsing.

**Spec:** `docs/superpowers/specs/2026-09-17-assistant-transcript-engines-and-sharing-design.md`

## Global Constraints

- No `--shared` flag or parallel endpoint probing is required.
- A message-read failure never triggers another reference or engine probe.
- No new dependency is proposed.
- Preserve the top-level `{chat, messages}` structure and existing legacy message fields.
- This is a read operation. It must not import a shared chat, create a copy, send a prompt, or follow `parentChatId` to fetch additional private history.
- JSON/YAML preserve the returned parts, including their original text; formatting does not change the fetch.
- Work only in `/tmp/gcx-assistant-transcripts`, branch `codex/assistant-transcripts`. No pushes, PR publication or remote mutation.
- Synthetic fixtures only. No real transcript contents or credentials in source, logs or committed tests.

## Task 1: Integrate reference resolution, engine-aware reading and the command

This is one coherent deliverable: splitting the client from its only CLI consumer would leave a nonfunctional feature between tasks. A single implementer owns it, followed by independent task and whole-branch reviews.

**Files:**
- Create `internal/assistant/conversation_reference.go` and corresponding tests.
- Create `internal/assistant/conversation_read.go` and corresponding tests for transcript orchestration and focused wire types.
- Modify `internal/assistant/types.go`, `transcript.go`, and their tests only for additive fields and text behavior.
- Modify `internal/providers/assistant/conversation.go` and `conversation_test.go` for real command wiring and regressions.
- Modify `internal/agent/command_annotations.go`, `README.md` where relevant, and generated `docs/reference/cli/gcx_assistant_conversation_get.md`.
- Use `internal/assistant/api.go`/`client.go` only where sharing existing helpers avoids duplication. Do not migrate unrelated clients.
- Add one focused `cmd/gcx/fail` regression if needed to establish typed auth errors convert to exit 3.

**Interfaces:**
Consumes existing `Client` endpoint/auth fields, `freshToken`, `Chat`, `ChatMessage`, `ConversationTranscript`, `ResolveClientOptions` and codec options. Produces:

```go
type ConversationReference struct {
    ID string
    Shared bool
    // Private parsed URL state, if needed for validating the Grafana origin.
}
func ParseConversationReference(input string) (ConversationReference, error)
func (r ConversationReference) ValidateGrafanaURL(grafanaURL string) error
func (c *Client) GetConversation(ctx context.Context, ref ConversationReference) (*ConversationTranscript, error)
```

Parse without network activity. Validate a supplied URL against the configured Grafana origin/base path before transcript requests. Keep plain single-segment IDs opaque; reject empty IDs, slashes/backslashes, traversal, controls and URL-shaped malformed values. Use `net/url` parsing and path escaping. Support configured Grafana subpaths and normalize default ports, host case and scheme for origin comparison. Shared URLs follow the verified `/a/grafana-assistant-app/chats/shared/{id}` path, permitting navigation query/fragment but never interpreting them as API data. Do not use a URL's host for network requests.

- [ ] **Step 1: Add failing behavioral tests.** Begin with actual Cobra shared-ID and ordinary AI SDK requests against httptest servers. Use current exported `Command()` and `writeAssistantTestConfig`; the production defects must cause behavioral failures before adding new interfaces. Example synthetic responses:

```go
// For a bare shared ID: GET /chats/shared-1 -> 404, then these responses:
metadata := `{"data":{"id":"shared-1","name":"Synthetic","engine":"aisdk","source":"assistant","isPublic":true}}`
messages := `{"data":{"thread":"main","messages":[{"id":"m1","role":"assistant","created":"2026-01-01T00:00:00Z","parts":[{"type":"text","text":"hello"},{"type":"data-synthetic","data":{"value":1}}]}]}}`
// Execute conversation get shared-1 --config <fixture> -o json.
// Assert requests exactly /chats/shared-1, /shared/shared-1,
// /shared/shared-1/ui-messages?thread=main; output text content hello,
// original two parts, chat.id shared-1, shared provenance and main-thread scope.
```

Run `go test ./internal/providers/assistant -run TestConversation -count=1`. Record RED output in the report.

Add table-driven client/reference tests covering explicit shared URL routing, normal metadata precedence, metadata-only 404 fallback, all non-404 stop paths, both-404 errors, no fallback on message errors, unknown engine and absent engine compatibility. Assert exact request sequences rather than only happy-path results.

- [ ] **Step 2: Implement resolution and wire readers.** The control flow is:

```go
// Pseudocode shows ordering; implement with typed errors, not string matching.
shared := ref.Shared
meta, err := readMetadata(ctx, ref.ID, shared)
if !shared && isHTTPNotFound(err) {
    shared = true
    meta, err = readMetadata(ctx, ref.ID, true)
}
if err != nil { return nil, err }
switch meta.Engine {
case "", "legacy":
    // shared: consume embedded metadata messages, ordinary: /all-messages
case "aisdk":
    // /chats/{id}/ui-messages?thread=main or /shared/{id}/ui-messages?thread=main
 default:
    return nil, fmt.Errorf("unsupported conversation engine %q", meta.Engine)
}
```

Before coding shared legacy handling, read `/tmp/gcx-transcript-api-contract.md` for the verified wire format and route parity. Use the existing configured API base (including `/api/cli/v1` for OAuth), HTTP client and per-request token refresh. No alternate transport on errors. Introduce only the focused error type needed to preserve status:

```go
// Implement these methods for the existing root error converter.
HTTPStatusCode() int
APIServiceName() string // "Assistant"
APIUserMessage() string
```

Do not dump response bodies (which can contain transcript data) into errors. Keep safe operation/status context. A typed status 404 triggers only the documented metadata fallback. Validate required data envelope/metadata ID and message collection structure so missing data is not silently decoded as an empty success. Accept valid empty arrays and initialize empty result slices. Keep ordinary legacy decoding compatible with existing fixtures.

- [ ] **Step 3: Normalize and preserve data.** Add `Engine string json:"engine,omitempty"` and `Shared bool json:"shared,omitempty"` to `Chat`; add `Parts json.RawMessage json:"parts,omitempty"` to `ChatMessage`; add `Scope string json:"scope,omitempty"` to `ConversationTranscript`. Scope `main` on AI SDK results, `shared` on legacy shared results, omitted for ordinary legacy. These additive fields distinguish engine/source/scope without changing old fields.

Use focused wire structs for UI messages. Copy ID/role/created and raw parts unchanged; convert only `type:text` parts to existing `ContentBlock{Type:"text", Text:...}`. Keep all parts in original order, including unknown types; do not synthesize tool blocks. Reject malformed part envelopes/text values rather than dropping invalid data. Preserve returned message order. Render shared provenance and AI SDK scope in text only when applicable. If messages contain non-text parts but no displayable prose, say so and suggest JSON rather than claiming an empty conversation. Keep context stripping a text rendering behavior.

Test multiple text parts, raw unknown/tool/file parts, timestamps/order, empty arrays, hidden and non-prose messages, malformed responses and format parity. Reuse existing transcript codec tests; avoid changing prompt or investigation behavior.

- [ ] **Step 4: Wire Cobra and documentation.** Parse the reference before client/config resolution in RunE, validate URL against `clientOpts.GrafanaURL`, call `GetConversation`, and encode using existing codecs:

```go
ref, err := assistant.ParseConversationReference(args[0])
if err != nil { return err }
// ResolveClientOptions remains the existing provider auth entry point.
// ValidateGrafanaURL precedes GetConversation.
transcript, err := client.GetConversation(cmd.Context(), ref)
if err != nil { return fmt.Errorf("failed to fetch conversation: %w", err) }
return opts.IO.Encode(cmd.OutOrStdout(), transcript)
```

Use `get <id-or-url>` in help. Keep existing ID invocation valid. No new flags. Explain main-thread scope for AI SDK and shared snapshot behavior; remove unconditional continuation advice from the command and agent metadata. Add a verified example to README and relevant portable skill only if existing conversation instructions need correction; do not expand unrelated assistant help.

Command tests cover shared URL, bare shared ID, ordinary legacy and AI SDK, invalid syntax/origin before transcript I/O, text/JSON/YAML/agent default parity, and field selection. One auth conversion regression tests 401 and 403 through existing root conversion machinery if not already covered by typed interface tests.

- [ ] **Step 5: Verify, self-review and commit.** Run focused tests, then gofmt on touched Go files, `GCX_AGENT_MODE=false mise run all` (covers lint, full tests, build, references and docs). Use normal tool escalation for cache/test-server permission failures; never treat a sandbox setup error as a code defect. Capture logs, command exit codes and RED/GREEN evidence. Check `git diff --check`, generated-reference changes, doc-maintenance structural applicability and working tree status. Commit only this feature plus its design/plan after required checks pass. Do not push. Use a title plus What/Why body. If a gate is unavailable or fails outside this change, report exact evidence to controller before committing.

**Acceptance:** The dedicated command retrieves the original shared AI SDK case using the selected context; bare shared IDs resolve without flags; auth/server/message errors never cause probing; legacy output stays compatible; structured data retains unknown parts; only intended docs/source change. No prompt, import or mutation requests occur.

## Backend readiness finding

Current Assistant main (77dd63412c225ee09514d227184e58b5cdcd5d97) registers shared
routes but its CLI OAuth scope allowlist omits `/api/cli/v1/shared`. GCX must
preserve that 403 without fallback. The supported plugin-proxy slice and client
fixtures remain implementable. A separate Assistant allowlist fix/rollout is
required for shared retrieval through the direct OAuth CLI base. This is a known
release dependency, not a reason to weaken GCX auth behavior. The controller is
asking whether that backend fix should also be prepared; no implementer should
change backend files.
