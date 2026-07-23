---
name: agento11y-instrument
description: >
  Sets up and instruments a developer's own LLM app or agent to send generations and
  agentic workflow to Grafana Agent Observability (the Agent Observability SDKs) — greenfield setup,
  fixing broken instrumentation, or filling gaps in existing instrumentation. Uses gcx
  for the parts a static prompt can't do: `gcx login` / `gcx cloud stacks` to find the
  stack, and `gcx agento11y agents|conversations|generations` to VERIFY that data actually
  lands — so it iterates (instrument → run → verify → fix) until generations arrive, not
  blindly. Reads the app's code, detects language/framework, classifies instrumentation
  state (none / partial / broken), then runs a fixed gap checklist whose #1 item is the
  silent failure no other prompt catches: the SDK emits OTel spans/metrics but never
  creates a TracerProvider/MeterProvider, so without them all metrics go to a no-op and are
  lost. Also checks agent_version (required for per-version Performance charts), set_result
  completeness, SYNC vs STREAM, parent_generation_ids DAG links, and workflow-step coverage.
  Recommends changes citing file:line and, only with explicit confirmation, applies minimal
  diffs that don't change app behavior. Pulls SDK reference from sigil-sdk's llms.txt rather
  than restating it, and hands off to `agento11y-test-starter` once data flows. It does NOT
  write test suites or set up tenant evaluations, rules, or guards — offline test suites are
  `agento11y-test-starter`, tenant eval rules + guards are `agento11y-prod-setup`;
  does NOT install coding-agent telemetry plugins (that is llms.txt "Path A"); does NOT mint
  or store credentials or invent endpoints. Trigger on phrases like "instrument my app",
  "send my agent's traces to Grafana", "set up AI observability for my app", "my generations
  aren't showing up", "why is Performance empty", "add Agent Observability to my code", "fix my instrumentation".
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
lives in sigil-sdk's `llms.txt` "Path B". Fetch it rather than restating it here; this file holds
the flow and the decision logic. A minimal fallback lives in
[references/instrumentation.md](references/instrumentation.md) for when the fetch is unavailable.

## Rules

- **Reference, don't restate.** Fetch SDK detail from
  `https://raw.githubusercontent.com/grafana/sigil-sdk/main/llms.txt` (Path B). Only inline decision
  logic here. If the fetch fails, fall back to [references/instrumentation.md](references/instrumentation.md).
- **Never invent an endpoint or a token.** Read them from the environment (`AGENTO11Y_ENDPOINT`,
  `AGENTO11Y_AUTH_TENANT_ID`, `AGENTO11Y_AUTH_TOKEN`, `OTEL_EXPORTER_OTLP_ENDPOINT`,
  `OTEL_EXPORTER_OTLP_HEADERS`) or ask the developer. Never fabricate a URL or mint a token.
- **Two targets: Grafana Cloud and local dev.** Detect which the app is aimed at, don't assume Cloud.
  A local endpoint (e.g. `http://localhost:8080` for a local Agent Observability instance, OTLP at
  `http://localhost:4318`) is legitimate for development — if the app already points there, respect
  it; do not force a Cloud URL. For Cloud, the developer supplies the endpoint + token (Step 0).
  **Caveat:** the gcx verification loop (Step 5) reads a Cloud tenant — it only confirms data landing
  for a Cloud target. For a local target, verify against the local instance / its UI instead and say
  so.
- **Write `AGENTO11Y_*` env vars, never `SIGIL_*`.** `SIGIL_*` is a deprecated legacy fallback. Do
  this **even if sibling apps or existing `.env` files in the repo use `SIGIL_*`** — matching a stale
  local convention perpetuates it. If the app already reads `SIGIL_*`, add the `AGENTO11Y_*` names
  (the SDK still honors both) and note the old ones are deprecated. Do not "match the siblings."
- **The gcx command group is `gcx agento11y`.** The old name `aio11y` (still the internal Go package
  name) does **not** exist as a command — an invocation using aio11y instead of agento11y fails.
  Every verification command uses the `agento11y` group. Do not emit the old aio11y command name even
  if prior knowledge suggests it.
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
  the app does and what gets observed, which defeats the point.
- **Do not assume language symmetry.** Verify the provider wrapper / framework adapter actually
  exists for the app's language before recommending it (Python has the most adapters, JS fewer, Go
  only google-adk, Java/.NET core + providers + google-adk). If it doesn't exist, hand-instrument
  with the core SDK. Prefer, in order: **provider wrapper → framework adapter → hand-instrumentation**.
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

The app needs, in its environment before the SDK starts:
`AGENTO11Y_ENDPOINT`, `AGENTO11Y_AUTH_TENANT_ID`, `AGENTO11Y_AUTH_TOKEN` (generation ingest) and
`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS` (traces/metrics). First check what's
already set (including any existing `.env`) — if all are present, skip to Step 1.

> **When any value is missing, do NOT just list the variable names and ask — hand the developer the
> exact place to get each one (link + clicks), every time.** The concrete sources are in point 3
> below; surface them proactively. The most common failure of this skill is naming
> `OTEL_EXPORTER_OTLP_ENDPOINT`/`OTEL_EXPORTER_OTLP_HEADERS` and leaving the developer to guess — the
> answer is the stack OTLP tile + "Generate now", which precomputes both. Give that first.

**First, decide the target.** Is the app aimed at **Grafana Cloud** or a **local dev instance**?
Look at any existing endpoint in the env / `.env` / sibling apps. If it already points at
`localhost` (a local Agent Observability instance), that's a local-dev target — keep it, and skip
the Cloud/gcx credential steps below (there's no Cloud token to fetch; the local instance's own
config applies). The gcx verification loop in Step 5 only works for a Cloud target — for local, note
that and verify against the local instance instead. The rest of this step is the Cloud path.

**Cloud path — what gcx does for you (run these):**

1. `gcx config current-context` — is there a working context? If not, **just ask the developer to log
   in to the stack they want the instrumentation to connect to** — e.g. "run `gcx login` against your
   stack." Do not fabricate the login command yourself (don't guess the host or flags); let them run
   their own login (the Agent Observability setup screen gives them the exact command, or they use
   `gcx login`). Instrumentation itself needs no gcx login — only Step 5 verification does, so this
   never blocks writing the code.
2. `gcx cloud stacks list`, then `gcx cloud stacks get <stack-slug>` — identify the target stack and
   its URLs. This gives you the stack to point the developer at, and confirms which tenant the Step 5
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
   token come from `https://<stack>.grafana.net/plugins/grafana-sigil-app` → Connection tab. When the
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

- **`none` (greenfield)** — no SDK import anywhere. Full setup from scratch.
- **`partial`** — an SDK client is constructed and some generations are wrapped, but coverage is
  incomplete (no OTel providers, no `agent_version`, no workflow steps for an agentic pipeline, no
  parent links). Run the full checklist; recommend + apply only the gaps.
- **`broken`** — the SDK is present but wrong: metrics silently lost (no MeterProvider), export
  misconfigured, legacy `SIGIL_*` vars, stream/non-stream mode mismatch, `set_result`/`SetResult`
  never called, or `rec.err()`/`Err()` never checked. Fix first, then gap-check.

All three paths converge on the same checklist (Step 3); they differ only in how much is already done.

## Step 3 — Run the instrumentation gap checklist

Walk each item against the code. Record PRESENT / MISSING / WRONG with `file:line`. This mechanical
audit is the skill's core value. Items 1, 2, 5, 6 fail **silently** (no error) — always check them.
Items 3, 7, 8 mean data lands but analysis is degraded. For the fix, read the named **section** of
the fetched llms.txt (locate it by its heading — do not trust line numbers, they drift).

| # | Check | Silent-failure symptom | llms.txt section |
|---|-------|------------------------|------------------|
| 1 | OTel TracerProvider **and** MeterProvider created before the SDK client (verify by construction + Performance view / OTLP POSTs — **not** via gcx, which can't see OTel; see Step 5) | spans/metrics go to no-op → all latency/token/cost metrics lost. The #1 failure. | "OTel setup (required)" |
| 2 | Providers shut down after `shutdown()` | last batch of spans/metrics dropped on exit | "OTel setup (required)" |
| 3 | `agent_name` + `agent_version` set on generations / handlers | per-version Performance charts break (join on `gen_ai.agent.version`) | "Sigil architecture and ingest model", "Telemetry fields to prioritize" |
| 4 | `set_result`/`SetResult` includes response_id, response_model, finish/stop reason, full token usage (incl. `cache_read_input_tokens`, `cache_write_input_tokens`, `reasoning_tokens`), **and `input`/`output` populated with `Message` objects** (system+user prompt in `input`, model reply in `output`) | charts/cost blank; wrong `cache_creation_input_tokens` name silently ignored; **empty `input`/`output` → the conversation thread shows "No messages in this turn" — tokens land but there is no visible prompt/response** | "Implementation rules", "Telemetry fields to prioritize" |
| 5 | `rec.err()`/`Err()` checked after the recorder closes | SDK validation/enqueue errors are silent → generations never arrive, no signal | "Implementation rules" |
| 6 | SYNC (non-stream) vs STREAM (stream) set correctly | streaming metrics (TTFT) corrupted | "Sigil architecture and ingest model", "Implementation rules" |
| 6b | `operation_name` is a **recognized** value — `generateText` (SYNC default), `streamText` (STREAM default), `embeddings`, `execute_tool`, `framework_chain`, `framework_retriever`. Best: omit it and take the SDK default. Do **not** invent one like `"chat"` | the span reaches Tempo but the UI classifies `gen_ai.operation.name` as `unknown` → the conversation renders a synthetic generation node **with no attached span** → the trace does not show in the conversation and the "T" (trace) icon is absent, even though `trace_id`/`span_id` are set. Silent, like #1 | "Sigil architecture and ingest model", "Implementation rules" |
| 7 | `parent_generation_ids` set on multi-agent / fan-in generations | no dependency DAG; upstream eval failures don't propagate | "Multi-agent dependency tracking" |
| 8 | Workflow steps emitted for agentic pipelines with non-LLM nodes | execution graph invisible; node input/output state lost. Use the adapter if one exists, else `enqueue_workflow_step`; never both for one node (duplicates) | "Workflow step instrumentation (agentic pipelines)" |
| 9 | Env vars are `AGENTO11Y_*` (not legacy `SIGIL_*`); client built config-free when env present | drift; duplicated config | "Environment" |
| 10 | Content-capture mode intentional (SDK default `no_tool_content`); no secrets in `tags`/`metadata`/`user_id` | PII/secrets leak into exports | "Content capture", "Tags, metadata, and user id" |
| 11 | Client tags low-cardinality; end-user identity via `user_id`, not a tag | high-cardinality tags blow up metric labels | "Tags, metadata, and user id", "Implementation rules" |

## Step 4 — Recommend (the report)

Emit the report using llms.txt's output contract (its "Output contract" section): top opportunities
first, and per opportunity — exact `file:line`, why it matters, a concrete diff proposal, a test
plan, and any risk. Rank by severity: missing OTel provider first (data loss), then broken export, then missing
`agent_version`, then coverage gaps. Every recommendation cites a `file:line`. Then stop and ask
before applying anything.

## Step 5 — Apply, then verify (the loop)

Only after the developer confirms a diff. Bounded to ~3–4 iterations.

1. **Apply** the confirmed diff (Edit/Write). Order of preference: provider wrapper → framework
   adapter → hand-instrument the core SDK — only what exists for the language. Add/update a focused
   test for the changed instrumentation. Preserve flush/shutdown lifecycle. Never touch app logic.
   Pull exact usage from llms.txt / the per-language README / `examples/getting-started/*`.
2. **Run the app** for one turn to generate traffic (ask the developer to run it, or run it if
   there's a clean entrypoint and they approve). **If the run can't happen** — missing provider API
   key, no clean entrypoint, needs a full runtime — do **not** work around it by changing the model
   or adding a provider. Stop the loop, report the wiring as verified-by-construction (imports
   resolve, providers build, client + handler construct), and tell the developer the one thing left
   is to run one turn themselves with their key. A verified-by-construction result is a fine outcome.
3. **Verify — two independent channels, don't conflate them.** Instrumentation sends data on two
   separate paths, and confirming one says **nothing** about the other:

   **Channel A — generations** (the SDK ingest client → `/api/v1/generations:export`). Carries the
   prompt, response, tokens, cost, model, finish_reason. This is what gcx can read.
   - **Cloud target → via gcx:** `gcx agento11y agents list` (does the agent appear?);
     `gcx agento11y agents get <agent-name>` (is `generation_count` climbing?);
     `gcx agento11y conversations search --filters 'agent = "<agent-name>"' --from <t0> --to <t1>`
     (both `--from`/`--to` required, RFC3339) then `gcx agento11y conversations get <conversation-id>`
     or `gcx agento11y generations get <generation-id>` — tokens, finish reason, and cost populated.
     This proves generation ingest + `set_result` are wired. **It does NOT prove OTel is wired.**
   - **Local-dev target:** gcx can't read a local instance — confirm the app printed no `agento11y:`
     export errors and check the local instance / its UI for the new conversation.

   **Channel B — OTel spans/metrics** (the TracerProvider/MeterProvider → OTLP exporter →
   `/v1/traces`, `/v1/metrics`). Carries latency/token/cost **metrics**. This is checklist #1, the
   #1 silent failure — and **gcx cannot see it** (traces/metrics land in the stack's Tempo/Prometheus,
   not the Agent Observability ingest API). So verify Channel B separately, by the strongest signal
   available, in this order:
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
     in the stack's **Tempo** (Explore → Tempo, filter by `service.name`). If conversations appear
     (Channel A) but Performance is empty, the MeterProvider is missing or no-op → back to checklist
     #1. Do not report OTel as wired on the strength of `generations get` alone, nor on metrics alone.
   - **Trace shows in Tempo but NOT inside the conversation (no "T" icon):** the span is landing but
     `gen_ai.operation.name` is an unrecognized value (e.g. `"chat"`) → the UI classifies it as
     `unknown` and can't attach it to the conversation node. This is checklist #6b — fix
     `operation_name` to a recognized value (or omit it for the default) and re-run.
4. If a signal is missing, diagnose the next gap from what the checks showed, propose the fix, and
   loop back to step 1. After ~3–4 iterations without full signal, stop and report exactly what
   lands, what doesn't, and what to check next (app stderr for `agento11y:` warnings, credentials).

## Step 6 — Hand off

Once generations land and metrics populate, instrumentation is done — that's the prerequisite for
everything else. Point the developer at the next step: `agento11y-test-starter` to build an
**offline test suite** for the agent (useful before shipping *and* for regression-testing new
versions once it's live), and `agento11y-prod-setup` to set up **online eval rules + guards** on
real traffic once it's deployed. The split is offline test suite vs online rules/guards — not
before-traffic vs after-traffic.

## Note — keeping this skill in sync

The SDK reference (env vars, provider snippets, field lists, workflow-step schema, adapter matrix) is
intentionally **not** duplicated here — it lives in sigil-sdk's `llms.txt` "Path B" and the
per-language READMEs, which are the shipped source of truth. This skill holds only decision logic
(state classification + gap checklist + the gcx verification loop). When a user-facing semantic
changes (new SDK field, renamed env var, new framework adapter), update `llms.txt` (and its onboarding
wizard copy); this skill points at llms.txt **by section heading, not line number** (line numbers
drift as llms.txt is edited), so no re-pointing is needed unless a heading itself is renamed. If you
find yourself pasting a provider snippet into this file, stop — fetch llms.txt instead. The
`references/instrumentation.md` fallback is deliberately minimal for the same reason.
