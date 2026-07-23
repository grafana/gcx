---
name: agento11y-prod-setup
description: >
  Sets up production evaluation and guardrails for a DEPLOYED AI agent in Grafana
  Agent Observability, grounded in the agent's own code and its real ingested traffic.
  The judgment layer on top of the `agento11y` skill: it reads the agent's source
  (system prompt, tools, entrypoint) AND samples its live traffic via gcx, checks
  what evaluators/rules/guards already exist, then recommends only what's missing —
  online eval rules (score live conversations for regressions) and guards (warn-first
  request-path policies that redact / tool-filter and may later be promoted to deny).
  It drafts reviewable YAML and, only with explicit confirmation, applies via
  `gcx agento11y`. New guards start disabled + warn. It DOES create tenant-level objects —
  that is the point — but every write is confirmed. It never rewrites or redeploys the
  agent. Trigger on phrases like "set up production evaluation", "my agent is in prod
  what should I evaluate", "catch quality regressions", "add guardrails to my agent",
  "redact PII from my agent", "block dangerous tools", "set up online evals and guards".
allowed-tools: Bash, Read, Write, Edit
---

# Agent Observability — production evals & guards setup

The production counterpart to `agento11y-test-starter` (which runs pre-ship, on code alone,
producing an offline test suite). This skill runs **after** ship, when the agent has real
traffic, and sets up the two production surfaces the starter deliberately leaves out:

- **Online eval rules** — evaluators that score ingested live conversations, so regressions
  surface without hand-reviewing every conversation.
- **Guards** (hook-rules) — policies on the request path that `warn`, redact (`transform`), or
  block tools (`tool_filter`) in real time, and can later be promoted to `deny`.

## What this skill does that `agento11y` doesn't

The sibling `agento11y` skill is the **mechanics** layer: exact CLI flags, evaluator/rule YAML
shapes, create-or-update semantics, the online-eval setup steps. It assumes you already know
*what* to create.

This skill is the **judgment** layer. It answers *which* rules and guards this specific agent
needs, by grounding in two evidence sources a generic checklist can't use:

1. **The agent's code** — its system prompt, tools, and how it handles user data. Half the value
   is here; read and cite it (`file:line`).
2. **The agent's real traffic** — because it's deployed, you can see what it actually does in
   prod, not just what the code says it might.

Two gaps this skill fills beyond `agento11y`:

- **Recommendation from evidence** — `agento11y` starts once you know what to create; this decides.
- **Guards** — `agento11y` documents evaluators and rules but not guards (hook-rules), even though
  `gcx agento11y guards` exists. This skill carries the guard shapes
  (`transform` / `tool_filter` / `action_on_fail`) itself.

For any mechanical detail — exact flags, evaluator/rule YAML fields, the setup flow — defer to
the `agento11y` skill and to `gcx agento11y <sub> --help` rather than restating it here.

## Rules

- Every connection to the stack goes through **`gcx agento11y`** — never raw HTTP, never a hand-held
  token. `gcx` owns Cloud auth (via `gcx login`). Prerequisite: `gcx` installed and authenticated;
  if it isn't, say so and stop.
- **Confirm the target stack before any WRITE (Step 0 + Step 5).** Reads run freely once you've
  shown the context; writes (upsert evaluators, create/update rules and guards) need an explicit yes on the
  target stack. `gcx` may be pointed at the wrong stack, and this skill creates tenant-level
  objects.
- **Check before recommending.** Always list what already exists first
  (`gcx agento11y evaluators list`, `rules list`, `guards list`) and never recommend a duplicate.
  Compare by **semantic equivalence**, not just id/name — see Step 2.
- This skill **does** create tenant-level objects — that is its job, the one thing that separates
  it from `agento11y-test-starter`. But every creation is **explicit and confirmed**: show the exact
  YAML, get a yes, then create it with the matching `gcx agento11y` command. A yes for one object
  is not a yes for the next.
- New guards are always drafted **`enabled: false`** and **`action_on_fail: "warn"`** — even
  hard-policy ones. Never draft a first-time `deny` guard, and never ship one enabled: a false
  positive blocks real users. The developer flips to `deny` + enabled themselves, later, after
  watching it in warn mode (Step 6).
- New online rules start with a **conservative `sample_rate`** (e.g. `0.1`), not `1.0` — an
  `llm_judge` over 100% of traffic costs real money.
- Prefer **starting from an evaluator template** (`gcx agento11y templates list`, then
  `gcx agento11y templates get`) over authoring
  a new evaluator. Only write a fresh one when nothing fits.
- Do not rewrite the agent's prompt, optimize, or redeploy. This skill configures observation and
  guardrails around the agent, not the agent itself.
- If a required input is missing (the agent's name as Agent Observability sees it, or `gcx` auth),
  ask — don't guess.

## Step 0 — Confirm the target stack

Before reading traffic or writing anything, show the developer where `gcx` is pointed. The active
context may not be the stack they think:

```
gcx config current-context        # the active context name
gcx config view                   # its server URL, org-id, auth method
```

Display the resolved **context name, server URL, and org-id**. Two thresholds:

- **Before reads** (traffic sampling, inventory): show the resolved context so the developer sees
  where the discovery ran. A wrong stack here wastes effort but doesn't change anything.
- **Before any write** (Step 5): require an **explicit yes** that this is the intended production
  stack. Writes are what create tenant objects, so this confirmation is the hard gate.

If it's wrong, stop — the developer switches with `gcx config use-context <name>`, or you pass
`--context <name>` on every `gcx` call. (Watch for `localhost` / dev-looking servers — a strong
sign the active context is not their prod stack.)

## Step 1 — Read the code and sample real traffic

Two evidence sources. Do both; every later recommendation cites one of them.

**Code** (as `agento11y-test-starter` Step 1). Find and record file:line for: the entrypoint, the
system prompt, the tool/function definitions, and how it handles user data. This tells you what
*could* go wrong. **The code is the authoritative source for the system prompt and tools** —
content capture is often off in production, so the ingested traffic frequently has an empty
`system_prompt` and may omit tool definitions. Never conclude "this agent has no system prompt"
from the traffic; read it from the code.

**Traffic**, via `gcx` — this tells you what *does* go wrong:

1. Find the agent as Agent Observability sees it: `gcx agento11y agents list` (and `agents get` /
   `agents list-versions`) to get the exact `agent_name` — this is the `match.agent_name` you'll target. (Tip:
   `agents list` prints a leading hint line before the JSON; set `GCX_AGENT_MODE=true` or skip
   that line if you parse it.)
2. Sample recent conversations: `gcx agento11y conversations search --filters 'agent = "<name>"'`
   (add `status = "error"`, time windows, `tool.name`, `eval.passed = false`) and
   `gcx agento11y generations get <id>` for detail. Look for long tool loops, over-refusals, PII
   echoed back, off-topic drift, malformed outputs, error clusters.
   - **Some agents have generations but no conversations** (e.g. single-shot agents whose spans
     aren't grouped into a conversation). If `conversations search` returns empty but `agents get`
     shows a non-zero `generation_count`, don't stop — sample generations directly
     (`gcx agento11y generations get <id>`) and use `selector: all_assistant_generations` for rules
     rather than a conversation-scoped one.

**Minimum evidence bar.** Aim for **≥20 recent conversations over ≥7 days** before drafting
anything. Fewer than that and you risk overfitting one odd conversation into a production rule or
guard: if the window is thin, either stop and say so, or proceed but mark every recommendation
**low-confidence** and lean on drafts (disabled guards, low `sample_rate`) rather than anything
that intervenes. A recommendation from a single conversation is a hypothesis, not a rule. If the
agent has essentially no traffic, stop — this is the wrong skill; `agento11y-test-starter` (offline
suite) is the right one until traffic exists.

## Step 2 — Inventory what already exists

Before proposing anything: `gcx agento11y evaluators list`, `gcx agento11y rules list`,
`gcx agento11y guards list` (add `-o yaml` to see full definitions). For each concern you're about to
raise, decide whether something already covers it — by **semantic equivalence, not id or name**.
Two objects are effectively the same when they share:

- the same **surface** (both rules, or both guards),
- the same **target** (overlapping `match`, especially `agent_name` / `selector`),
- the same **intent** (evaluator kind + what it checks, or the guard's policy),
- the same **action** (rule scoring vs. guard `warn`/`deny`/`transform`/`tool_filter`).

If an existing object matches on those, don't create a second one — say it's covered and stop, or
propose an `update` to the existing one. A different id over identical intent+target+action is a
duplicate, and duplicate guards/rules double cost and can conflict.

## Step 3 — Recommend rules and guards

Map each observation to the surface that fits. **Online rules** *observe* (score, detect
regressions, no user impact); **guards** *intervene* (block/redact on the live path). Pick the
surface by whether you want to watch or to stop.

| If, in code or traffic, the agent… | Surface | Shape (prefer a predefined template) |
| --- | --- | --- |
| gives answers whose quality can drift | online **rule** | fork `template.helpfulness` / `template.relevance` (`llm_judge`) over `user_visible_turn` |
| does RAG / cites sources | online **rule** | fork `template.groundedness` (`llm_judge`) |
| must emit JSON / a fixed shape | online **rule** | fork `template.json_valid` (`json_schema`) |
| over-refuses or drifts off-topic | online **rule** | `regex` / `llm_judge` on `all_assistant_generations` |
| public-facing text | online **rule** | fork `template.toxicity` / `template.pii` (`llm_judge`) |
| echoes user data with PII/secrets | **guard** | `transform` (regex → `[REDACTED:...]`) |
| can call dangerous tools (shell, delete, write) | **guard** | `tool_filter` with `blocked_names` globs (e.g. `Bash(*rm*)`) |
| is subject to prompt-injection / hard policy | **guard** | `llm_judge` evaluator; draft `warn`, later promotable to `deny` |

Template ids above are the **expected** global blueprints — they can vary by deployment and
version, so always resolve the current set with `gcx agento11y templates list` before using a name;
don't trust a hardcoded id. Pick 3–6, ranked. Each gets a one-line `why` citing a **file:line or a
conversation/generation id** from Step 1. For rule mechanics (selectors, match keys, evaluator
kinds, templates), the `agento11y` skill is the reference — don't restate it here.

**Every candidate lands in exactly one of three states — and the decision is final for this run:**

- **Recommended** — worth setting up now; goes to Step 4 (draft) and Step 5 (apply).
- **Considered, not recommended** — you evaluated it and it's not worth it (low value, no
  evidence, would just add cost/noise). Record it with a one-line why, and **do NOT draft or apply
  it**. Don't quietly re-add it later under a different framing — if you're tempted to, it belongs
  in Recommended, so put it there and own the reasoning. Bias toward fewer objects: recommend only
  what earns its place. "It's harmless in warn mode" is not a reason to create something — an
  unused guard/rule is still cost and noise.
- **Skipped (duplicate)** — the stack already covers it (Step 2 semantic-equivalence check);
  don't create a second one.

The set you draft in Step 4 and apply in Step 5 is **exactly** the Recommended list — nothing
from the other two states leaks in.

## Step 4 — Draft the definitions as YAML

**First, check whether `./agento11y-prod/` already has drafts from an earlier run** (`ls
agento11y-prod/**` if it exists). If it does, **read them before writing** — don't blind-overwrite
(a plain `Write` over an existing file also just errors). Treat a prior draft as a peer proposal:
reconcile rather than replace. If a previous run made a deliberate, well-reasoned choice — e.g.
dropped an email-redaction pattern because "this agent's job is to email people, so redacting
every address is all-false-positive noise" — that judgment is usually right; keep it and fold in
only what's genuinely new. Overwrite a prior draft only when yours is clearly better, and say why.

Write the definitions to a **local scratch directory**, `./agento11y-prod/`
(`evaluators/<id>.yaml`, `rules/<id>.yaml`, `guards/<id>.yaml`). These are **working drafts, not
committed artifacts**: they exist so the developer can review a diff before you apply it, and
their source of truth after apply is the stack, not the repo. Add `agento11y-prod/` to `.gitignore`
(or write under the OS temp dir) so they aren't accidentally committed — they hold the applied
config redundantly and can carry regexes/prompts the repo shouldn't own. They are exactly what
you'll pass to `gcx agento11y <kind> create -f` (for evaluators: `upsert -f`). Use the
**top-level-fields** YAML shape that the `create -f`/`upsert -f` commands expect (not the
`apiVersion/kind/spec` manifest that the `get -o yaml` commands emit — don't round-trip get
output into create).

**Rules and evaluators**: follow the `agento11y` skill's input format exactly. Start an evaluator
from a template (`gcx agento11y templates get <id> -o yaml`), give it your own `evaluator_id`, and
**always include a `version`** — it is required on create (a date like `"2026-07-15"` or a semver
works; existing evaluators use dates). Omitting it fails with `version is required`. Rule starts
enabled at a low `sample_rate`.

**Guards** — the shape the `agento11y` skill omits (`gcx agento11y guards create -f guard.yaml`; the
resource `Kind` is `HookRule`).

> **Hard gate: do not draft a guard until you have captured the exact accepted create-file shape
> from the current `gcx` version.** Run `gcx agento11y guards create --help` and inspect a real
> definition (`gcx agento11y guards get <id> -o yaml`, or `guards list -o yaml`); if none exists, get
> the schema from `--help` / the resource definition. Only then write a guard file.

This skill (not the `agento11y` skill) carries the guard shape, and field names/nesting can drift by
`gcx` version — so the captured shape is the source of truth, not the snippet below. The snippet
is **illustrative only** (verified against gcx v0.4.0: a `transform` guard with `rule_id`,
`enabled`, `priority`, `action_on_fail`, and `transform.patterns[].{id,regex,replacement}` is
accepted as-is). On create the server fills defaults you don't set — notably `selector: all`,
`phase: preflight`, and `short_circuit: false` — so a `guards get -o yaml` right after create
shows more fields than you sent; that's expected, not drift. A guard starts **disabled** and in
**warn**:

```yaml
# ILLUSTRATIVE — confirm fields with `gcx agento11y guards create --help` before use.
# PROD-SETUP DRAFT — creates a TENANT-LEVEL guard (HookRule) via gcx agento11y guards create.
# Starts disabled + warn on purpose: watch it on real traffic before enabling / switching to deny.
rule_id: guard.<agent>.<policy>
enabled: false
priority: 10           # lower runs first
action_on_fail: warn   # always draft as warn (see below)
# choose at least one of transform / tool_filter / evaluator_ids:
transform:
  patterns:
    - id: ssn
      regex: "\\b\\d{3}-\\d{2}-\\d{4}\\b"
      replacement: "[REDACTED:ssn]"
# tool_filter:
#   blocked_names: ["shell_exec", "Bash(*rm*)"]
# evaluator_ids: ["<policy-judge-id>"]
```
`phase` is `preflight` (default) or `postflight`. **Always draft `action_on_fail: warn`, even for
a hard-policy guard** (prompt-injection, deny-list): a first-time `deny` enabled on live traffic
blocks real users on a false positive. A policy-judge guard references an evaluator id (create the
evaluator first); the developer changes it to `deny` only later, after watching the false-positive
rate in warn mode (Step 6). This skill never drafts an enabled `deny` guard.

## Step 5 — Confirm, then apply with `gcx`

> **`upsert`/`create`/`update` write to the stack — never run them before the developer's explicit yes
> (step 2).** The one thing you CAN run before the yes is `evaluators test -f <request>.yaml`,
> which tests a judge config **without persisting it** (pass `kind`, `config`, `output_keys`,
> `generation_id` in the file — no evaluator need exist yet). Use it to tune the judge (step 1).
> There is **no CLI dry-run** for rules or guards — their safety comes from shipping guards
> `disabled` + `warn` and rules at a low `sample_rate`, not from a preview.

Per object the developer wants, in dependency order (evaluators → rules → guards, since a
rule/guard referencing an evaluator needs it to exist first):

1. **For an `llm_judge` evaluator, tune the prompt before creating it — this is the real work,
   not a formality.** A judge is only as good as its prompt, so don't create it on first draft and
   move on. Loop: pick 1–2 real generations you know the right answer for
   (`gcx agento11y generations get <id>`), run the draft config with
   `gcx agento11y evaluators test -f <request>.yaml -g <gen-id>` (tests without persisting), and
   **read both the verdict AND the rationale**. If either disagrees with what you expected, adjust
   the `system_prompt`/`user_prompt` and re-test. Repeat until verdict and rationale both hold on
   your known examples. Only then does the evaluator move to step 2. (This tuning loop deserves its
   own dedicated flow; keep it lightweight here.)
2. **Confirm.** Restate the target stack from Step 0 (context name + server), show the exact YAML,
   and get an explicit yes for that object. A yes for one object is not a yes for the next. Nothing
   is written before this yes.
3. **Apply** via gcx, only after the yes: `gcx agento11y evaluators upsert -f evaluators/<id>.yaml`,
   then `gcx agento11y rules create -f rules/<id>.yaml`, then
   `gcx agento11y guards create -f guards/<id>.yaml`. Evaluators are create-or-update (same id
   updates). Pass `--context <name>` on every call if the confirmed stack isn't the default
   context. gcx handles auth — no tokens here.
   - **Verify each created guard came back `enabled: false` and `action_on_fail: warn`.** The
     create output (or `gcx agento11y guards get <id> -o yaml`) echoes the stored object — check it.
     If a guard landed `enabled: true`, that's a mistake (the draft carried the wrong value): fix
     the YAML to `enabled: false` and `update` it immediately. A first-time guard live on traffic
     is exactly what this skill must never ship.
   - A judge-model 404 when testing/scoring is usually a stack-side misconfiguration (the tenant's
     judge model id is dead), not your evaluator — flag it; the online rule will hit the same broken
     judge at runtime until it's fixed.
4. If `gcx` reports it isn't authenticated, stop and ask the developer to run `gcx login`; do not
   fall back to raw HTTP.

## Step 6 — Summarize and hand off

Output, in this order:

1. The three states from Step 3, kept distinct: **Recommended** (each with its surface, evaluator
   kind, and `why` — file:line or conversation/generation id); **Considered, not recommended**
   (each with its one-line why); **Skipped as duplicates** (what on the stack already covered it).
   Don't move an item between states between Step 3 and here.
2. What was created vs. left as a draft: for each **Recommended** object, the YAML path and whether
   it was applied via gcx. (Considered-not-recommended items were never drafted, so they have no
   path — don't list them here.)
3. The follow-through the developer still owns:
   - **Guards:** watch the `warn` guards on real traffic; flip to `deny` + `enabled` only once the
     false-positive rate looks acceptable — `gcx agento11y guards update <id> -f ...`. That flip is
     theirs to make, not this skill's.
   - **Rules:** raise `sample_rate` once scores look sane and cost is understood
     (`gcx agento11y rules update`); add alerting on regressions if wanted.
   - Inspect everything in Agent Observability (rules/guards/evaluators pages, the conversation
     Quality view) or via the `gcx agento11y` list and get commands.
4. A one-line pointer back: for pre-ship offline evaluation of a new agent or version,
   `agento11y-test-starter` is the counterpart; for control-plane mechanics, the `agento11y` skill.
