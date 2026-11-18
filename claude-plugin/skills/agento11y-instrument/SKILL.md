---
name: agento11y-instrument
description: >-
  Instruments a developer's own LLM app or repairs missing generations and empty
  Performance charts in Grafana Agent Observability, reusing OpenTelemetry GenAI
  spans the app already emits. Applies confirmed changes and verifies generation
  ingest, traces, and metrics. Excludes coding-agent telemetry plugins. For
  offline tests use agento11y-test-starter; for eval rules and guards use
  agento11y-prod-setup.
allowed-tools: Bash, Read, Grep, Glob, Edit, Write, WebFetch
---

# Agent Observability — instrument an LLM app

Help a developer wire **their own** LLM app or agent to Grafana Agent Observability, from zero or
from a broken/partial state, and **keep going until data actually lands in the stack**. The value
this skill adds over the static instrumentation prompt is two things a prompt can't do:

1. A mechanical **gap checklist** run against the real code — headed by the one failure that is
   completely silent (missing OTel providers → every metric lost, no error).
2. A **verification loop** through `gcx`: after each change, run the app and check the
   `gcx agento11y` agents / conversations / generations commands to confirm generations arrived.
   Diagnose the next gap from what's missing, not from guesswork.

The SDK reference (env vars, provider snippets, field lists, framework adapters, workflow steps)
lives in agento11y's `llms.txt` "Path B". Fetch it rather than restating it here; this file holds
the flow and the decision logic. A minimal fallback lives in
[references/instrumentation.md](references/instrumentation.md) for when the fetch is unavailable.

Not every app needs the SDK. If the app already emits OpenTelemetry (OTel) GenAI spans for its
model calls (Step 2 defines the minimum), reuse them: classify it as `otel` (Step 2) and run the
OpenTelemetry checklist (Step 3b) instead of adding the SDK. The reference for that path is the
llms.txt section "Choose OpenTelemetry or the Grafana SDK" and the public guide
[Instrument agents with OpenTelemetry](https://grafana.com/docs/grafana-cloud/observe-and-act/agent-observability/get-started/opentelemetry/).

## Rules

- **Reference, don't restate.** Fetch SDK detail from
  `https://raw.githubusercontent.com/grafana/agento11y/main/llms.txt` (Path B). Only inline decision
  logic here; Step 3b is the exception (see the sync note at the end). If the fetch fails, fall back
  to [references/instrumentation.md](references/instrumentation.md).
- **Never invent an endpoint or a token.** Read them from the environment (`AGENTO11Y_ENDPOINT`,
  `AGENTO11Y_PROTOCOL`, `AGENTO11Y_AUTH_MODE`, `AGENTO11Y_AUTH_TENANT_ID`, `AGENTO11Y_AUTH_TOKEN`,
  `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`) or ask the developer. Never fabricate a
  URL or mint a token.
- **Target is Grafana Cloud.** The developer supplies the endpoint + token (Step 0), and the gcx
  verification loop (Step 5) confirms data landing against the Cloud tenant. Never fabricate the
  endpoint or token — read them from the environment or ask.
- **Write `AGENTO11Y_*` env vars, never `SIGIL_*`.** `SIGIL_*` is a deprecated legacy fallback. Do
  this **even if sibling apps or existing `.env` files in the repo use `SIGIL_*`** — matching a stale
  local convention perpetuates it. If the app already reads `SIGIL_*`, add the `AGENTO11Y_*` names
  (the SDK still honors both) and note the old ones are deprecated. Do not "match the siblings."
- **Gate every code WRITE on explicit confirmation.** Report first (Step 4), apply only after the
  developer says yes (Step 5). Read-only gcx verification and re-running the app happen freely inside
  the loop; editing files does not.
- **Keep diffs small; do not change app behavior.** Instrumentation is additive. No refactors, no
  prompt rewrites, no dependency upgrades beyond the SDK/adapter packages actually needed.
- **Never change the model, provider, or the app's LLM config — not even with permission, not even
  "just to run the verify loop."** Instrument whatever model the app already uses. This is absolute:
  changing the model is out of scope for instrumentation, full stop. If a run fails because a
  provider API key is missing, the only allowed responses are: (a) ask the developer to provide the
  key for the model the app *already* uses, or (b) skip the live run and report the wiring as
  verified-by-construction, telling the developer to run it themselves. Do **not** offer to switch
  the provider, do **not** ask "which provider should I use?", and do **not** add a new provider
  dependency (e.g. `langchain-anthropic`) to make the run succeed. If the developer separately says
  they *want* a different model, that is an app change they own — tell them to make it and re-invoke
  this skill; do not fold it into the instrumentation diff. Swapping the model silently changes what
  the app does and what gets observed, which defeats the point. **The provider API key
  (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, …) is the app's own concern, not the instrumentation's** —
  it authenticates the LLM call, not the telemetry export, and the app already has it if it runs at
  all. So don't ask for it, configure it, or rewire it; if the live verify-run fails on a missing
  provider key, skip the run and report verified-by-construction (see Step 5). Just don't conflate the
  two 401s: a 401 on **generation ingest** is observability auth and *is* yours to fix (usually a
  missing `AGENTO11Y_PROTOCOL`/`AGENTO11Y_AUTH_MODE`); an auth error from the **model provider** is
  not — surface it and let the developer handle their own key.
- **Do not assume language symmetry.** Verify the provider wrapper / framework adapter actually
  exists for the app's language before recommending it (Python has the most adapters, JS fewer, Go
  only google-adk, Java/.NET core + providers + google-adk). If it doesn't exist, hand-instrument
  with the core SDK. Prefer, in order: **provider wrapper → framework adapter → hand-instrumentation**.
- **Record each model call once.** If opted-in OTel GenAI spans already record a call, do not add an
  SDK generation for the same call. Both paths create a generation, so the call appears twice.
- **The loop is bounded.** At most ~3–4 instrument→verify iterations. If data still isn't landing,
  stop and report what's checked and what remains — don't loop forever.
- **Field-name traps:** `cache_write_input_tokens`, NOT `cache_creation_input_tokens`. `agent_version`
  maps to the `gen_ai.agent.version` label and is required for per-version Performance charts.
  **`MessageRole` (Python SDK) has only `USER`, `ASSISTANT`, `TOOL` — there is no `SYSTEM` (or
  `DEVELOPER`) member**; `MessageRole.SYSTEM` raises `AttributeError`. Fold the system prompt into the
  `USER` message (or a `text_part`), and prefer the `user_text_message()` / `assistant_text_message()`
  / `tool_result_message()` helpers over hand-building `Message(role=...)`. Always confirm enum members
  and helper names against the installed package before running — do not assume from llms.txt.
- **Out of scope:** offline test suites → `agento11y-test-starter`; tenant eval rules + guards on
  real traffic → `agento11y-prod-setup`. Coding-agent telemetry plugins (Claude Code, Cursor, …) →
  llms.txt "Path A". Any control-plane write.
- **If a required input is missing** (entrypoint, framework, endpoint, gcx auth), ask — don't guess.

## Step 0 — Credentials and endpoint

The app needs, in its environment before the SDK starts, **seven** vars — not five; the two mode
vars are the ones most often forgotten, and their absence is a silent 401:

- generation ingest: `AGENTO11Y_ENDPOINT`, `AGENTO11Y_PROTOCOL=http`, `AGENTO11Y_AUTH_MODE=basic`,
  `AGENTO11Y_AUTH_TENANT_ID`, `AGENTO11Y_AUTH_TOKEN`.
- traces/metrics: `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`.

`AGENTO11Y_PROTOCOL=http` and `AGENTO11Y_AUTH_MODE=basic` are **required for Cloud, not optional** —
the SDK defaults (grpc / no-auth) return a silent 401 against the Cloud HTTP ingest endpoint. (They
scope the ingest channel only; the OTel channel's transport/auth is set entirely by the `OTEL_*`
vars — see references/instrumentation.md.) The only var that is sometimes omittable is
`OTEL_EXPORTER_OTLP_HEADERS`: **required when sending directly to the Cloud OTLP gateway** (the common
case, gateway enforces Basic auth), omittable **only** when `OTEL_EXPORTER_OTLP_ENDPOINT` points at a
local Alloy / OTel Collector that already holds the Cloud credentials. First check what's already set
(including any existing `.env`) — if all are present, skip to Step 1. Watch for the near-miss where
the endpoint is set under the wrong name (e.g. `AGENTO11Y_API_ENDPOINT` — the SDK reads
`AGENTO11Y_ENDPOINT`, so the wrong name is silently ignored and ingest falls back to a default host).

> **The `otel` path (Step 2) needs fewer vars.** An app that already sends compatible OTel GenAI
> spans needs only the `OTEL_*` vars, with a token that has `traces:write` (and `metrics:write` if
> it exports metrics). It needs the `AGENTO11Y_*` vars only if it also calls SDK APIs (see
> Step 2). Read the app (Step 1) before you ask the developer for missing values, so you ask only
> for what its path needs.

> **When any value is missing, do NOT just list the variable names and ask — hand the developer the
> exact place to get each one (link + clicks), every time.** The concrete sources are in point 3
> below; surface them proactively. The most common failure of this skill is naming
> `OTEL_EXPORTER_OTLP_ENDPOINT`/`OTEL_EXPORTER_OTLP_HEADERS` and leaving the developer to guess — the
> answer is the stack OTLP tile + "Generate now", which precomputes both. Give that first.

**What gcx does for you (run these):**

1. `gcx config current-context` — is there a working context? If not, **just ask the developer to log
   in to the stack they want the instrumentation to connect to** — e.g. "run `gcx login` against your
   stack." Do not fabricate the login command yourself (don't guess the host or flags); let them run
   their own login (the Agent Observability setup screen gives them the exact command, or they use
   `gcx login`). Instrumentation itself needs no gcx login — only Step 5 verification does, so this
   never blocks writing the code.
2. `gcx cloud orgs list` — discover organisation slugs using browser Cloud OAuth.
   If re-authentication is needed, use `gcx cloud login` with the same context;
   default scopes include `profile` and stack management. Access-policy tokens
   cannot list memberships and override OAuth when `GRAFANA_CLOUD_TOKEN` or
   `cloud.<entry>.token` is set. With access-policy authentication, ask the developer
   for their organisation slug instead.
   Have the developer select the intended slug, then run
   `gcx cloud stacks list --org <org-slug>` and `gcx cloud stacks get <stack-slug>`
   to identify the target stack and its URLs. This gives you the stack to point the developer at, and confirms which tenant the Step 5
   verification will read from.

**What still needs the Connection page (gcx cannot do these today):**

3. gcx does **not** generate the Agent Observability OTLP gateway URL, and does **not** mint the
   ingest / access-policy token. **When you ask the developer for a value, always tell them exactly
   where to get it — a link and the clicks — never just name the variable and wait.** The two channels
   come from two different places:

   **`OTEL_*` (traces/metrics) — easiest, let Cloud build them.** Send the developer to the stack's
   OTLP tile: `https://grafana.com/orgs/<org-slug>/stacks/<stack-id>/otlp-info`. It already shows
   `OTEL_EXPORTER_OTLP_ENDPOINT` and the Instance ID; under **Password / API Token → "Generate now"**
   it mints a token and then fills an **Environment Variables** block with all `OTEL_*` vars — **the
   base64 `OTEL_EXPORTER_OTLP_HEADERS` is precomputed**, ready to copy. No manual base64. (In Python,
   the value uses `Basic%20…` — keep it as given.)

   **`AGENTO11Y_*` (generation ingest) — the plugin Connection page.** `AGENTO11Y_ENDPOINT` and the
   token come from `https://<stack>.grafana.net/plugins/grafana-agento11y-app` → Connection tab. When the
   developer creates the token via **"Create a token in Cloud Access Policies"**, tell them the scopes:
   **`sigil:write`, `metrics:write`, `traces:write`, `logs:write`**. UI heads-up: `sigil` is not in
   the default resource list — add it via **"Add scope"** (then tick Write); the scope is still
   `sigil:*` (the Cloud resource keeps the old name). The same `glc_…` token works for both channels
   if it has all four scopes. Also set `AGENTO11Y_PROTOCOL=http` and `AGENTO11Y_AUTH_MODE=basic` (see
   references/instrumentation.md — the SDK defaults grpc/none give a 401).

   Ask the developer to put the values in a gitignored `.env` (or export them) **themselves** — **do
   not ask them to paste a secret token into the chat** (it is captured in the transcript). Instrument
   the code to read from the environment and have them supply the values out-of-band. **Never invent a
   URL or mint a token.**

   > Two different tokens — don't confuse them. gcx logs in with its own OAuth token (`gat_`) and
   > refreshes it automatically; that is what authenticates the `gcx` commands here. It is **not** the
   > app's ingest token. The app needs a separate access-policy token (`glc_…`) in
   > `AGENTO11Y_AUTH_TOKEN` / `OTEL_EXPORTER_OTLP_HEADERS`, and gcx does not create that one.

Once gcx has a working context, the Step 5 verification commands (under the `gcx agento11y` group)
work against the developer's tenant even before the app's own credentials are fully wired.

> The Connection page is the only manual step. If a future gcx release can create the access-policy
> token and surface the OTLP endpoint, this step collapses to gcx-only — but do not assume it can
> today; check `gcx cloud --help` rather than promising it.

## Step 1 — Read the app and detect language / framework / shape

Find and read, recording `file:line` for each:

1. The generation entrypoint(s) — where the model is invoked.
2. How the LLM client is constructed (which provider: OpenAI / Anthropic / Gemini / other).
3. The app bootstrap / init — where an OTel `TracerProvider` / `MeterProvider` would be created.
4. Any existing Agent Observability SDK imports (`agento11y` / `@grafana/agento11y` / the Go
   `agento11y` package — or the legacy `sigil_sdk` / `@grafana/sigil-sdk-js` / Go `sigil` names in
   older code) or `AGENTO11Y_*` usage.
5. Existing OTel GenAI instrumentation: anything that writes `gen_ai.*` span attributes for the
   model calls. That can be an OTel GenAI instrumentation library, the framework's built-in OTel
   telemetry, or spans the app creates itself. A missing tracer provider or opt-in does not rule
   out the `otel` state; Step 2 also requires that the SDK (item 4) does not record those calls.
   Record separately whether a tracer provider with an exporter exists (Step 3b, O1). A zero-code
   agent can create it outside the app code (`opentelemetry-instrument`, a Node `--require`
   register hook, the Java agent, or Kubernetes Operator injection), so also check launch commands,
   Dockerfiles, and manifests. Also record whether `agento11y.record` is already set on the
   resource (`OTEL_RESOURCE_ATTRIBUTES`) or on spans. A framework's OTel exporter is not proof of
   compatibility; Step 3b checks the attributes that the spans actually carry.

Detect:
- **Language** — from the manifest / extensions (`pyproject.toml`/`.py`, `package.json`/`.ts`,
  `go.mod`/`.go`, gradle/`.java`, `.csproj`/`.cs`).
- **Framework** — grep for `langgraph`, `langchain`, `openai-agents`, `llamaindex`, `google-adk`,
  `strands`, `pydantic-ai`, `litellm`, `claude-agent-sdk`, `vercel-ai-sdk`, `crewai`, or a custom
  orchestrator.
- **Shape** — single generation vs agentic pipeline (multiple nodes / a graph / sub-agents). This
  decides whether workflow steps (checklist #8) and parent links (#7) are in play.

Before recommending a provider wrapper or framework adapter, confirm it exists for this language
(fetch llms.txt "SDK API surface" — the matrix is not symmetric across languages).

## Step 2 — Classify instrumentation state

State the classification explicitly, with the `file:line` evidence that led to it:

- **`none` (greenfield)** — no SDK import and no OTel GenAI instrumentation. Full SDK setup from
  scratch.
- **`otel`** — OTel GenAI instrumentation already covers the model calls (Step 1, item 5), and the
  SDK does not record those calls. A call counts as covered when it produces its own span with
  `gen_ai.operation.name` set to `chat`, `generate_content`, `text_completion`, or `invoke_agent`.
  A few ad-hoc `gen_ai.*` attributes on other app spans do not count; those calls are `none`. A
  covered call is `otel` even when its spans are not yet compatible, for example without the
  opt-in, content, provider and model, or a tracer provider; Step 3b finds those gaps. Reuse the
  spans and do **not** add the SDK for them. Run the OpenTelemetry checklist (Step 3b), not
  Step 3.
  - If the developer also needs SDK-only APIs (hooks such as guards, conversation ratings, or
    experiments), add the SDK for those APIs only, and do not wrap the model calls that the spans
    already record.
  - If spans cover only some model calls, treat those calls as `otel` and the rest as `none`.
  - **If the SDK also records generations in the same process, a resource marker alone is
    wrong.** It also selects the SDK's own spans. They carry the same generation IDs as the SDK's
    direct export, and ingest keeps whichever copy arrives first, so a span copy with less content
    can be stored instead of the SDK's record. A span-level marker wins over the resource marker:
    keep the resource marker and set `agento11y.record=false` on the SDK's own spans in a span
    processor, selected by the SDK's instrumentation scope. That needs no call-site change. It
    does not cover an SDK-wrapped call that goes through an auto-instrumented client: the inner
    span has the same scope as the `otel` spans. Exclude those inner spans call by call: for
    example, set a context variable around each SDK-wrapped call, and let the span processor set
    `false` on spans that start while it is set. Skip the `false`
    step if the SDK exports generations with its experimental `otel` protocol: its spans then carry
    a span-level `true` and are its only record.
  - A process with both SDK calls and `otel` calls runs Step 3 for the SDK calls and Step 3b for
    the `otel` calls.
  - If configuration or a small span processor cannot make the spans compatible (for example, a
    library that writes another vocabulary, such as flattened `gen_ai.prompt.N.*` attributes),
    propose switching the instrumentation library or using the SDK for those calls. Ask before you
    change dependencies.
- **`partial`** — an SDK client is constructed and some generations are wrapped, but coverage is
  incomplete (no OTel providers, no `agent_version`, no workflow steps for an agentic pipeline, no
  parent links). Run the full checklist; recommend + apply only the gaps.
- **`broken`** — the SDK is present but wrong: metrics silently lost (no MeterProvider), export
  misconfigured, legacy `SIGIL_*` vars, stream/non-stream mode mismatch, `set_result`/`SetResult`
  never called, or `rec.err()`/`Err()` never checked. The same model call recorded by both the SDK
  and opted-in OTel GenAI spans is also `broken` (duplicate generations, Step 3b O7). So is an SDK
  call whose own spans a resource-level marker selects (see `otel` above). Keep one path per call.
  By default, keep the SDK and stop the OTel copy; the SDK keeps its features and metric labels.
  If the marker is on the resource and no call is `otel`, remove the resource marker. If some
  calls are `otel`, keep the resource marker and exclude the SDK's spans with a span-level
  `false`, as `otel` above describes; removing the resource marker would also stop the `otel`
  calls. If the marker is on the span, remove it from the duplicated spans. These changes touch
  only the OTel setup, except marking an SDK-wrapped call through an auto-instrumented client,
  which can need a line at each call site. To keep OTel instead, remove the SDK wrapping, which
  changes app code, so confirm first. Then continue with Step 3 or Step 3b. Fix first, then
  gap-check.

`none`, `partial`, and `broken` converge on the same checklist (Step 3); they differ only in how much
is already done. `otel` uses Step 3b.

## Step 3 — Run the instrumentation gap checklist (SDK path)

Walk each item against the code. Record PRESENT / MISSING / WRONG with `file:line`. This mechanical
audit is the skill's core value. Items 0, 1, 2, 5, 6 fail **silently** (no error) — always check them.
Items 3, 7, 8 mean data lands but analysis is degraded. For the fix, read the named **section** of
the fetched llms.txt (locate it by its heading — do not trust line numbers, they drift).

| # | Check | Silent-failure symptom | llms.txt section |
|---|-------|------------------------|------------------|
| 0 | **The `.env` actually takes effect.** Confirm the app loads its own `.env` by an explicit path (not a bare `load_dotenv()` resolved by CWD) **and** that it wins over vars already in the environment. Verify by printing `os.environ["AGENTO11Y_ENDPOINT"]` / `OTEL_EXPORTER_OTLP_ENDPOINT` **after** all imports, not before | **`import litellm` (and some other libs) inject localhost OTLP/ingest defaults into `os.environ` at import time** (`OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318`, `AGENTO11Y_ENDPOINT=localhost:8080`). A plain `load_dotenv()` does **not** override already-set vars → the Cloud endpoints in `.env` never apply and everything ships to localhost, returning **200 OK** if a local stack is up. Zero error signal, and gcx against the Cloud tenant shows nothing. Fix: `load_dotenv(<path-relative-to-__file__>, override=True)` before constructing providers/client. A bare `load_dotenv()` also resolves the wrong `.env` by CWD | "Environment" |
| 1 | OTel TracerProvider **and** MeterProvider created before the SDK client (verify by construction + Performance view / OTLP POSTs — **not** via gcx, which can't see OTel; see Step 5) | spans/metrics go to no-op → all latency/token/cost metrics lost. The #1 failure. | "OTel provider setup for the Grafana SDK" |
| 2 | Providers shut down after `shutdown()` | last batch of spans/metrics dropped on exit | "OTel provider setup for the Grafana SDK" |
| 3 | `agent_name` + `agent_version` set on generations / handlers | per-version Performance charts break (join on `gen_ai.agent.version`) | "Agent Observability architecture and ingest model", "Telemetry fields to prioritize" |
| 4 | `set_result`/`SetResult` includes response_id, response_model, finish/stop reason, full token usage (incl. `cache_read_input_tokens`, `cache_write_input_tokens`, `reasoning_tokens`), **and `input`/`output` populated with `Message` objects** (system+user prompt in `input`, model reply in `output`) | charts/cost blank; wrong `cache_creation_input_tokens` name silently ignored; **empty `input`/`output` → the conversation thread shows "No messages in this turn" — tokens land but there is no visible prompt/response** | "Implementation rules", "Telemetry fields to prioritize" |
| 5 | `rec.err()`/`Err()` checked after the recorder closes | SDK validation/enqueue errors are silent → generations never arrive, no signal | "Implementation rules" |
| 6 | SYNC (non-stream) vs STREAM (stream) set correctly | streaming metrics (TTFT) corrupted | "Agent Observability architecture and ingest model", "Implementation rules" |
| 6b | `operation_name` is a **recognized** value — `generateText` (SYNC default), `streamText` (STREAM default), `embeddings`, `execute_tool`, `framework_chain`, `framework_retriever`. Best: omit it and take the SDK default. Do **not** invent one like `"chat"` | the span reaches Tempo but the UI classifies `gen_ai.operation.name` as `unknown` → the conversation renders a synthetic generation node **with no attached span** → the trace does not show in the conversation and the "T" (trace) icon is absent, even though `trace_id`/`span_id` are set. Silent, like #1 | "Agent Observability architecture and ingest model", "Implementation rules" |
| 7 | `parent_generation_ids` set on multi-agent / fan-in generations | no dependency DAG; upstream eval failures don't propagate | "Multi-agent dependency tracking" |
| 8 | Workflow steps emitted for agentic pipelines with non-LLM nodes | execution graph invisible; node input/output state lost. Use the adapter if one exists, else `enqueue_workflow_step`; never both for one node (duplicates) | "Workflow step instrumentation (agentic pipelines)" |
| 9 | Env vars are `AGENTO11Y_*` (not legacy `SIGIL_*`); client built config-free when env present | drift; duplicated config | "Environment" |
| 10 | Content-capture mode intentional (SDK default `no_tool_content`); no secrets in `tags`/`metadata`/`user_id` | PII/secrets leak into exports | "Content capture", "Tags, metadata, and user id" |
| 11 | Client tags low-cardinality; end-user identity via `user_id`, not a tag | high-cardinality tags blow up metric labels | "Tags, metadata, and user id", "Implementation rules" |

## Step 3b — Run the OpenTelemetry checklist (`otel` path)

Walk each item against the code and the export config. Record PRESENT / MISSING / WRONG with
`file:line`. The reference is the llms.txt section "Choose OpenTelemetry or the Grafana SDK" and the
[public guide](https://grafana.com/docs/grafana-cloud/observe-and-act/agent-observability/get-started/opentelemetry/).
Step 3 item 0 (the `.env` takes effect) applies here too. For the `otel` calls, only its `OTEL_*`
probe matters: they do not use `AGENTO11Y_ENDPOINT`. In a mixed process (Step 2), Step 3 runs
item 0 in full for the SDK calls.

What a third-party instrumentation library writes depends on its installed version. Read the
installed package source or changelog, or capture spans with an in-memory exporter. Do not decide
from the library name alone. If you cannot read the installed version (no lockfile, package not
installed), say which results depend on it.

| # | Check | Symptom when it fails |
|---|-------|-----------------------|
| O1 | A tracer provider with an OTLP exporter is initialized before the first model call, in the app code or by a zero-code agent (Step 1, item 5). The exporter points at the stack's OTLP endpoint, with a token that has `traces:write`. Export env vars alone do not instrument the app | nothing is exported |
| O2 | `agento11y.record=true` is on the resource or on the GenAI spans. To set it on the resource, append `,agento11y.record=true` to an existing `OTEL_RESOURCE_ATTRIBUTES` value; do not replace the value. A span-level `false` excludes that span, even under a resource marker. If the SDK also records generations in this process, follow Step 2: a resource marker then needs a span-level `false` on the SDK's own spans. Confirm the marker is on the exported resource or span (for example, with an in-memory exporter): some resource constructors ignore `OTEL_RESOURCE_ATTRIBUTES` | spans reach the stack's traces, but no conversation appears |
| O3 | Each span that should become a generation has `gen_ai.provider.name` and `gen_ai.request.model`. Every inference span (`gen_ai.operation.name` is `chat`, `generate_content`, or `text_completion`) needs them. An `invoke_agent` span needs them only when no child model span produces a generation for that turn; otherwise do not add them, because the agent span would become a second generation with the same answer. Ingest skips an `invoke_agent` span that names neither. Older libraries write `gen_ai.system` instead of `gen_ai.provider.name`; ingest accepts it | the span does not become a generation. Tool-call spans never create standalone generations; that is expected |
| O4 | Content capture is the developer's decision, as in Step 3 item 10: ask before you turn it on. If the developer wants it, it is on and writes `gen_ai.input.messages` and `gen_ai.output.messages` as span attributes in the GenAI message format. Check the values the installed library expects: Python instrumentations built on `opentelemetry-util-genai` write span content only with `OTEL_SEMCONV_STABILITY_OPT_IN=gen_ai_latest_experimental` and `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT` set to `SPAN_ONLY` or `SPAN_AND_EVENT`. With that opt-in, the value `true` logs a warning and captures nothing; without it, `true` can send content as log events only. Sensitive content is removed before export | the conversation appears without messages. Content sent only as OTLP logs does not populate conversations |
| O5 | `gen_ai.conversation.id` is reused across the calls of one conversation; take it from the app's own conversation or session (a span processor can set it when the library has no hook). `gen_ai.agent.name` is set on inference spans, and `gen_ai.agent.version` too when the app has a version (O8 requires it) | each trace becomes its own conversation; the agent is listed under the resource's `service.name` |
| O6 | Collectors (Alloy, OTel Collector) keep the span and resource attributes, and sampling keeps the GenAI spans | generations are missing, or the attributes from O2–O5 are lost on the way |
| O7 | Re-check that the SDK does not also record these calls, especially after adding the SDK for SDK-only APIs (Step 2). A call that both paths record is `broken` (Step 2) | duplicate generations |
| O8 | Only if the developer wants Performance charts. The agent's Performance tab reads only the `gen_ai.client.token.usage` and `gen_ai.client.operation.duration` metrics, not spans. It keeps only metrics whose `gen_ai.agent.name` matches the agent and whose `gen_ai.agent.version` matches a version that the agent's generations declare (Prometheus labels `gen_ai_agent_name` and `gen_ai_agent_version`). So the inference spans need `gen_ai.agent.version` (O5), and the metrics need the same name and version. This also needs a meter provider with an OTLP metric exporter (in the app code or by a zero-code agent) and a token with `metrics:write`. The standard GenAI client metrics do not define the agent attributes, and libraries that emit the newer `gen_ai.client.inference.*` metrics feed nothing to this tab. Report what the installed library exports and do not promise Performance charts on this path. If the metrics lack the attributes, adding a meter provider does not make the tab appear | conversations appear, but the agent page has no Performance tab |

On this path, `chat` is a valid `gen_ai.operation.name`. Step 3 item 6b applies only to the SDK's
`operation_name`. Never rename `chat` to an SDK value such as `generateText` or `streamText`: the
OTel path accepts only `chat`, `generate_content`, `text_completion`, and `invoke_agent`.

## Step 4 — Recommend (the report)

Emit the report using llms.txt's output contract (its "Output contract" section): top opportunities
first, and per opportunity — exact `file:line`, why it matters, a concrete diff proposal, a test
plan, and any risk. Rank by severity: `.env` not taking effect (#0 — nothing lands at all) first,
then missing OTel provider (metrics data loss), then broken export, then missing `agent_version`,
then coverage gaps. For `otel`, rank item 0, O1–O3, O6, and O7 first (nothing lands without item 0
and O1–O3, O6 can drop the spans or their attributes on the way, and O7 duplicates data), then O4
and O5, then O8 if the developer wants Performance charts. Every recommendation cites a
`file:line`. Then stop and ask before applying anything.

## Step 5 — Apply, then verify (the loop)

Only after the developer confirms a diff. Bounded to ~3–4 iterations.

1. **Apply** the confirmed diff (Edit/Write). Order of preference: provider wrapper → framework
   adapter → hand-instrument the core SDK — only what exists for the language. Add/update a focused
   test for the changed instrumentation. Preserve flush/shutdown lifecycle. Never touch app logic.
   Pull exact usage from llms.txt / the per-language README / `examples/getting-started/*`.
   For `otel` calls, change only the OTel setup and span attributes (O1–O6, and O8 when it
   applies), including adding a tracer provider when O1 found none. Do not wrap those calls with
   the SDK; add the SDK only for the cases that Step 2 names.
2. **Run the app** for one turn to generate traffic (ask the developer to run it, or run it if
   there's a clean entrypoint and they approve). **If the run can't happen** — missing provider API
   key, no clean entrypoint, needs a full runtime — do **not** work around it by changing the model
   or adding a provider. Stop the loop, report the wiring as verified-by-construction (imports
   resolve, providers build, client + handler construct), and tell the developer the one thing left
   is to run one turn themselves with their key. A verified-by-construction result is a fine outcome.
3. **Verify — two independent channels, don't conflate them.** Instrumentation sends data on two
   separate paths, and confirming one says **nothing** about the other.

   **First, confirm gcx reads the same tenant the app writes to.** A verification against the wrong
   tenant is worse than no verification — an empty `agents list` gets misread as "data isn't landing"
   when it is, just elsewhere. Before drawing any conclusion from a gcx query: read the app's
   `AGENTO11Y_ENDPOINT` + `AGENTO11Y_AUTH_TENANT_ID` from its `.env`, then check `gcx config
   current-context` and that the active context points at that same stack/tenant. If it doesn't (e.g.
   context is `local` but the app writes to a Cloud stack), switch context or ask the developer to
   `gcx login` to the right stack — do not guess the login command. (If the gcx token is merely
   expired, that blocks Step 5 verification only, not writing the code — say so and continue.) On
   the `otel` path the app has no `AGENTO11Y_*` vars. Ask the developer which stack's OTLP tile the
   `OTEL_*` values came from, and compare that stack with the gcx context. The endpoint names only
   the region, and the instance ID is inside the base64 header next to the token, so never decode
   `OTEL_EXPORTER_OTLP_HEADERS`: the output would show the token.

   **Channel A — generations** (the SDK ingest client → `/api/v1/generations:export`). Carries the
   prompt, response, tokens, cost, model, finish_reason. This is what gcx can read.
   - **Via gcx:** `gcx agento11y agents list` (does the agent appear?);
     `gcx agento11y agents get <agent-name>` (is `generation_count` climbing?). To find the run's
     conversation, either `gcx agento11y conversations list --limit <n>` (most-recent first, no
     filters — the quickest post-run check) or `gcx agento11y conversations search --filters
     'agent = "<agent-name>"'` (`--filters` alone is enough; `--from`/`--to`, RFC3339, are **optional**
     and only needed to narrow a busy tenant). Then `gcx agento11y conversations get <conversation-id>`
     or `gcx agento11y generations get <generation-id>` — check tokens, finish reason, cost, and (for
     a multi-agent pipeline) that `parent_generation_ids` reproduce the DAG. This proves generation
     ingest + `set_result` are wired. **It does NOT prove OTel is wired.**

   **`otel` path:** the Channel A checks work, because Agent Observability turns the opted-in spans
   into generations. The exception is the `parent_generation_ids` DAG check: the OTel path does not
   set parent links, and it has no workflow steps. Do not report those as gaps. Spans arrive
   asynchronously, so if nothing appears, wait briefly and query again before you change code. If
   no generation lands, check the exporter's errors, then O2, O3, and O6. Without
   `gen_ai.agent.name` (O5), the agent is listed under the resource's `service.name`, so look for
   it there too. If a generation lands without messages, check O4. Skip Channel B below: it checks
   the SDK's providers, spans, and metrics, and O1 already covers the tracer provider. Check
   metrics only for O8. First read the attributes on the exported metrics, for example with a
   console or in-memory metric exporter: without `gen_ai.agent.name` and `gen_ai.agent.version`
   the tab cannot appear, so report that and stop. Otherwise run the app with the metric
   exporter's debug log on, confirm `POST …/v1/metrics → 2xx`, then open the agent's page. If no
   Performance tab appears after a few minutes, the metrics' agent name or version differs from
   those on the spans.

   **Channel B — OTel spans/metrics, SDK path only** (the TracerProvider/MeterProvider → OTLP
   exporter → `/v1/traces`, `/v1/metrics`). Carries latency/token/cost **metrics**. This is
   checklist #1, the #1 silent failure — and **gcx cannot see it** (traces/metrics land in the
   stack's Tempo/Prometheus, not the Agent Observability ingest API). So verify Channel B
   separately, by the strongest signal available, in this order:
   - **By construction (always do this):** confirm in the applied code that both a TracerProvider
     **and** a MeterProvider are created *before* the SDK client and shut down after it. This is
     static but reliable — a missing/late/no-op MeterProvider is exactly checklist #1.
   - **At runtime, if you can observe it:** run the **real app** (not an isolated probe script) with
     the OTel/urllib3 debug log on, and confirm you see **both** `POST …/v1/traces → 2xx` **and**
     `POST …/v1/metrics → 2xx`. The SDK emits spans automatically from `start_generation`/`end` (one
     traces POST per generation) and metrics on an interval — a clean shutdown flush surfaces both.
     **Traces and metrics are separate exports: seeing only `/v1/metrics` does NOT mean traces work,
     and vice-versa. Never claim "traces/metrics verified" from a probe that only exercised one of
     them** — that is the exact trap that reports Channel B as done when half of it was never sent. If
     you write a throwaway verification script, it must build the TracerProvider **and** MeterProvider
     and record a real generation, or just instrument the app and read its debug output.
   - **In the UI:** the stack's **Performance / metrics** view populates from metrics; **traces** land
     in the stack's **Tempo** (Explore → Tempo, filter by `service.name`). On the SDK path, if
     conversations appear (Channel A) but Performance is empty, the MeterProvider is missing or no-op
     → back to checklist #1. Do not report OTel as wired on the strength of `generations get` alone,
     nor on metrics alone.
   - **SDK path only. Trace shows in Tempo but NOT inside the conversation (no "T" icon):** the
     span is landing but `gen_ai.operation.name` is an unrecognized value (e.g. `"chat"`) → the UI
     classifies it as `unknown` and can't attach it to the conversation node. This is checklist
     #6b — fix `operation_name` to a recognized value (or omit it for the default) and re-run. On
     the `otel` path, never apply this fix: `chat` is valid there (Step 3b).
4. If a signal is missing, diagnose the next gap from what the checks showed, propose the fix, and
   loop back to step 1. After ~3–4 iterations without full signal, stop and report exactly what
   lands, what doesn't, and what to check next (app stderr for `agento11y:` warnings, credentials).

## Step 6 — Hand off

Once generations land and metrics populate, instrumentation is done — that's the prerequisite for
everything else. On the `otel` path, it is done when generations with the expected content land;
metrics count only if the developer asked for Performance charts (O8). Point the developer at the
next step: `agento11y-test-starter` to build an **offline test suite** for the agent (useful before
shipping *and* for regression-testing new versions once it's live), and `agento11y-prod-setup` to
set up **online eval rules + guards** on real traffic once it's deployed. The split is offline test
suite vs online rules/guards — not before-traffic vs after-traffic.

## Note — keeping this skill in sync

The SDK reference (env vars, provider snippets, field lists, workflow-step schema, adapter matrix) is
intentionally **not** duplicated here — it lives in agento11y's `llms.txt` "Path B" and the
per-language READMEs, which are the shipped source of truth. This skill holds only decision logic
(state classification + gap checklist + the gcx verification loop). When a user-facing semantic
changes (new SDK field, renamed env var, new framework adapter), update `llms.txt` (and its onboarding
wizard copy); this skill points at llms.txt **by section heading, not line number** (line numbers
drift as llms.txt is edited), so no re-pointing is needed unless a heading itself is renamed. If you
find yourself pasting a provider snippet into this file, stop — fetch llms.txt instead. The
`references/instrumentation.md` fallback is deliberately minimal for the same reason.

Step 3b is the exception: it mirrors the llms.txt section "Choose OpenTelemetry or the Grafana SDK"
and the public guide
[Instrument agents with OpenTelemetry](https://grafana.com/docs/grafana-cloud/observe-and-act/agent-observability/get-started/opentelemetry/).
Update it when either one changes.
