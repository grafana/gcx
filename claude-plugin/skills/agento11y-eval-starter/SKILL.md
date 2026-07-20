---
name: agento11y-eval-starter
description: >
  Use early in an AI-agent project — before ship, before real traffic — to decide which
  evaluations to set up and to scaffold a starter experiment. Reads the agent's own code (system
  prompt, tools, task), recommends specific evaluators with reasons that cite real lines, and writes
  a labeled draft test suite as an Agent Observability suite YAML. It also assesses how runnable the agent is: for
  an easily-invoked agent it generates a runner stub (run_experiment.py) with one hole to fill and
  can optionally run it (only with permission, only against the endpoint the developer configured);
  for agents that need a harness or a full runtime it points to the existing eval infra instead of
  emitting a runner that can't call the agent. It never creates tenant-level evaluators, rules, or
  guards.
---

# agento11y eval starter

Help a developer who has an AI agent but no evaluation set up yet. The hard part isn't
running an experiment — it's knowing **what to evaluate** and having **cases to test
against** before there's any traffic. Answer both, grounded in the agent's actual code.

Always produce:

1. A ranked, justified **evaluator recommendation** for this agent.
2. A starter **suite YAML** the developer reviews and extends.

Then, depending on how runnable the agent is (Step 1):

3. For an easily-invoked agent, a **runner stub** (`run_experiment.py`) that wires the suite
   to the SDK with one hole to fill — and optionally run it (Step 6), only with permission.
   For an agent that needs a harness or full runtime, point to the existing eval infra instead
   of a runner that can't actually call it.

This skill is language-agnostic — the reading, recommending, and YAML it produces do not
depend on the agent's language. What differs is how runnable the agent is: recommendations +
YAML always apply, but the runner (Step 4) and the optional run (Step 6) adapt to whether the
agent has a clean function seam or needs a harness / full stack. For deeper run-side patterns
(binding existing generations, cross-process verifiers) point to the per-language run skill
(Python: `agento11y-experiments`).

> Note: `agento11y-experiments` currently ships in the grafana/sigil-sdk repo
> (`python/skills/agento11y-experiments/`), not in this gcx bundle yet — install it from there for
> now. Consolidating it into the gcx bundle is pending.

## Rules

- Do not create, enable, or modify evaluators, rules, or guards in any Agent Observability tenant. No
  control-plane writes. (Running an offline experiment only publishes that run's scores — it
  does not create tenant-level evaluators/rules/guards — but only do it via Step 6.)
- Do not rewrite the agent's prompt, optimize, or redeploy.
- Never run the experiment without asking first (Step 6). Never run against a target the
  developer did not configure — use their `AGENTO11Y_ENDPOINT` + credentials (Grafana Cloud); if
  they are not set, ask for them, do not invent an endpoint.
- Never mint, generate, or store credentials. The developer owns their Grafana Cloud
  ingestion token; read it from the environment or ask them to paste it — do not create one.
- Never present the generated cases as validated. They are a draft to review and extend.
- Agent Observability is a Grafana Cloud product. Do not hardcode or assume any endpoint (no `localhost`);
  the developer supplies their Cloud endpoint and token.
- If a required input is missing (entrypoint, prompt, tools), ask the developer — don't guess.

## Step 1 — Read the agent

Find and read these in the target repo, and record the file path and line range of each:

1. The agent entrypoint (where the model is invoked).
2. The system prompt / instructions.
3. The tool / function definitions the agent can call.
4. One or two real user requests and what a correct answer looks like.

Every recommendation must cite one of these locations.

Also assess **runnability** — how hard it is to invoke this agent for one input and get its
output — because it decides what Step 4 produces. Classify it as one of:

- **easy — clean function seam.** A single call takes an input and returns text, with no live
  services (a small Python/TS agent, an LLM call behind one function). The generated runner
  works as-is.
- **in-process — needs a harness.** No 3-line call, but there is an injectable seam (e.g. a
  Go engine whose LLM client is an interface you can fake, callable from a test binary).
  Runnable in-process for a smoke/behavioral eval, but not a 3-line Python `run_agent`.
- **full-stack — needs the whole runtime.** The agent needs live backends/auth/queues and is
  exercised over HTTP against a running stack (tools that hit real datasources, a long
  multi-step loop). Existing eval infra likely runs it via a dedicated harness and polling,
  not a function.

Signals that push toward `in-process`/`full-stack`: the agent isn't Python; tools hit real
APIs/datasources; there is already a dedicated eval harness, Docker stack, or
scenario/ground-truth files in the repo. If a repo already has eval scenarios with expected
outputs, note them — they are better test cases than anything generated, and later steps
should point to or reuse them.

Separately, note the **agent's language**, because the Agent Observability experiments SDK exists only in
**Python and Go** (not JS/TS, Java, or .NET yet). This is independent of runnability — a TS
agent can be trivially runnable yet have no experiments SDK in its language. If the agent is
Python or Go, the runner is native. If it is any other language, say so plainly: the runner
must be Python or Go (calling the agent across a process boundary), or the developer waits for
experiments support in their language. Do not imply a native JS/Java/.NET experiments API
exists.

## Step 2 — Choose evaluators

The SDK models an evaluator as an id plus a **kind**. There are two kinds:

- `llm_judge` — a model grades the output against a rubric (relevance, helpfulness,
  groundedness, tone, task completion, format, safety, and similar judgment calls).
- `deterministic` — code decides pass/fail (exact/substring match, JSON validity, schema
  or regex shape, length bounds, "not empty", a required field is present).

Pick 3–6 (not more). Map each to what you read in Step 1, and give a one-line `why` per pick
that cites a file:line. Common mappings:

| If the agent… | Evaluator | Kind |
| --- | --- | --- |
| answers open-ended user requests | `relevance`, `helpfulness` | `llm_judge` |
| retrieves / cites sources / does RAG | `groundedness` | `llm_judge` |
| must stay on-topic / in-scope | `task_adherence` | `llm_judge` |
| must emit JSON or a fixed shape | `json_valid` / `schema_match` | `deterministic` |
| must follow a specific output format | `format_adherence` | `llm_judge` |
| calls tools | `tool_call_correct` | `llm_judge` (or `deterministic` if the correct call is checkable in code) |
| handles user data that could be echoed | `pii_leak` | `llm_judge` |
| produces public-facing text | `toxicity` | `llm_judge` |
| must always return something | `response_not_empty` | `deterministic` |

Evaluator ids are yours to choose — pick clear, stable ids. Be concrete about what "defining"
each one means, because it is not an Agent Observability control-plane action here: in the offline SDK flow
an evaluator is **code the developer writes in the runner** (Step 4). An `llm_judge` is a
function that calls a model and returns a score; a `deterministic` one is a plain code check.
`sigil.Evaluator(evaluator_id=..., kind=...)` is only the label attached to the score, not the
logic. (Forking a predefined template is a separate online-eval path, not needed here.)

This skill targets the **offline** phase: run these evaluators as offline experiments against
the suite from Step 3, before there is traffic. Do not recommend live/online evaluation to an
agent with no traffic.

Once the agent ships and has traffic, the same evaluation criteria can also be applied online
— as Agent Observability Rules over ingested conversations, or as SDK guard hooks on the request path — but
those are separate Agent Observability surfaces, configured elsewhere, and out of scope for this skill.
Mention this only as a one-line "next, once you have traffic" note; do not instruct on it here.

## Step 3 — Write the suite YAML

Write a suite file in the target repo (suggest `evals/<agent>-starter.yaml`). It must load
with the SDK's `TestSuite.from_yaml(...)`, so match this schema exactly:

- Top level: `suite_id` (required), plus optional `name`, `version`, `description`, `tags`,
  `changelog`, and `cases` (a list). `version` defaults to `1.0.0`.
- Each case: `id` (required), plus optional `name`, `description`, `tags`, `category`,
  `input`, `expected`, `weight`, `metadata`. `input` and `expected` are free-form (a string
  or a mapping).

Derive cases from the agent's real task. Produce at least 6, weighted toward `edge` and
`adversarial` (generated cases skew easy). Use `category` values `happy`, `edge`,
`adversarial`. Keep the header comment verbatim.

Every case must actually reach the agent — test the agent, not its harness. From Step 1,
note where the entrypoint parses/validates input before the model runs (e.g. a JSON parse, a
CLI arg check). Do not generate cases that fail in that pre-agent layer (malformed JSON, wrong
CLI flags) as if they tested the agent; if such a boundary is worth covering, label it clearly
as a harness case in its notes, or leave it out.

```yaml
# STARTER DRAFT — review before use. Generated from your agent code (<file:line refs>).
# NOT validated. Add your own real cases; the edge/adversarial cases need your judgment on
# expected behavior. Loads with sigil TestSuite.from_yaml(...).
suite_id: <agent>-starter
name: <Agent> starter suite
version: 1.0.0
cases:
  - id: happy-basic-request
    category: happy
    tags: [smoke]
    input:
      prompt: "<a real request this agent is built to answer>"
    expected: "<what a good answer looks like, or a rubric note>"
  - id: edge-underspecified
    category: edge
    input:
      prompt: "<a vague / multi-part / boundary request>"
    expected: "<how the agent should handle ambiguity>"
  - id: adversarial-prompt-injection
    category: adversarial
    input:
      prompt: "<an injection / out-of-scope / data-extraction attempt>"
    expected: "<agent should refuse / stay in scope / not leak>"
```

## Step 4 — Write the runner stub (branch on runnability)

The suite YAML alone does not run anything. The Agent Observability SDK stores and aggregates scores, but
it does **not** run the agent or compute the evaluators — the developer writes both. Left at
just the YAML, a developer new to offline eval is still blocked ("now what?"). So generate a
**minimal bootstrap runner** — just enough to get one experiment running.

This is deliberately the simplest path, not the full run-side API. The `agento11y-experiments`
skill is the reference for everything beyond bootstrap — binding an already-instrumented
agent's real generations/conversations, auditable LLM-judge grading, cross-process verifiers
(`TrialRef`), pass@k/pass^k. Don't reproduce those patterns here; generate the minimal runner
and point to `agento11y-experiments` for depth.

What you generate depends on the runnability you assessed in Step 1:

- **easy** → generate the full `evals/run_experiment.py` below. One hole to fill
  (`run_agent`).
- **in-process** → do NOT emit a Python `run_agent` that can't actually call the agent (e.g.
  a Go agent). Generate the same experiment wiring, but make `run_agent` shell out to a small
  harness in the agent's language (or write that harness), and say plainly the seam is the
  injectable LLM client. Point to any existing test that already invokes the agent in-process
  as the template.
- **full-stack** → do NOT emit a runner that pretends to call the agent as a function. The
  agent runs via its existing eval infra (dedicated harness, Docker stack, HTTP + polling).
  Deliver the recommendations + YAML, and point to that infra and to any existing
  scenario/ground-truth files as the real test cases. Be explicit that isolated runs aren't
  the path here.

Also branch on **language** (the experiments SDK is Python/Go only):

- **Python or Go agent** → native runner (`run_experiment.py`, or the Go `sigil` package).
- **TS / Java / .NET agent** → there is no experiments SDK in that language. Deliver
  recommendations + YAML (they are language-neutral), and be honest about the run path: the
  runner must be Python or Go calling the agent across a process boundary (e.g. a Python
  runner that shells out to `node your-agent.js` and reads its output), or the developer waits
  for experiments support in their language. Offer the subprocess bridge only as a labeled
  option with its cost (serializing input to the CLI, parsing output), not as a clean default.

For an **easy Python/Go** agent, write `evals/run_experiment.py`. It must:

- Load the suite with `TestSuite.from_yaml(...)`.
- Open an experiment (`sigil.experiment(...)`) and one `trial` per case.
- Call the agent through a single clearly-marked function `run_agent(case)` — **this is the
  one hole the developer fills**; wire it to the real entrypoint you found in Step 1.
- Include ONE recommended evaluator sketched end-to-end (prefer an `llm_judge` — a real model
  call that returns a JSON `{score, passed, explanation}`), so they see the shape and can copy
  it for the others. Reference the rest by name in a comment; do not stub all of them.
- Record I/O (`trial.record_io(...)`) and emit `trial.final_score(...)` with the evaluator.

Keep the header verbatim, and be honest in it about what still needs doing:

```python
#!/usr/bin/env python3
"""STARTER RUNNER — generated by agento11y-eval-starter, review before use.

Runs <agent> over evals/<agent>-starter.yaml as an Agent Observability experiment and publishes scores.

You still need to: (1) fill run_agent(case) to call YOUR agent; (2) tune the sketched
judge; (3) set real credentials — AGENTO11Y_ENDPOINT + AGENTO11Y_AUTH_TOKEN for your Grafana Cloud
stack. The SDK stores scores; it does not run the agent or the judge.

Set AGENTO11Y_INGEST_ACTOR to a stable value: the run and its trials must share one actor, or
trial creation fails with "401: experiment is owned by another actor".

    AGENTO11Y_ENDPOINT=... AGENTO11Y_AUTH_TOKEN=... AGENTO11Y_INGEST_ACTOR=ingest:sdk/python \
        python evals/run_experiment.py
"""
import json, os, time
from pathlib import Path
from dotenv import load_dotenv
from sigil_sdk import experiments as sigil

load_dotenv()
SUITE = Path(__file__).parent / "<agent>-starter.yaml"


def run_agent(case: sigil.TestCase) -> str:
    """THE ONE HOLE YOU FILL — call your agent for this case, return its output text."""
    raise NotImplementedError("wire this to your agent entrypoint (see Step 1 refs)")


def judge_<evaluator>(case_input, output) -> tuple[float, bool, str]:
    """Sketched llm_judge — a model call returning (score 0-1, passed, explanation). Tune it."""
    import litellm
    prompt = f"Grade <what this evaluator checks>. Return JSON {{\"score\":0-1,\"passed\":bool,\"explanation\":\"...\"}}.\n\nInput:\n{case_input}\n\nOutput:\n{output}"
    model = os.getenv("GRADER_MODEL") or os.getenv("MODEL_NAME")  # a LIVE model id; no default (dead ids 404)
    text = litellm.completion(model=model, messages=[{"role": "user", "content": prompt}],
                              temperature=0, max_tokens=300).choices[0].message.content or "{}"
    s, e = text.find("{"), text.rfind("}")
    d = json.loads(text[s:e + 1]) if s >= 0 else {}
    score = max(0.0, min(1.0, float(d.get("score", 0.0))))
    return score, bool(d.get("passed", score >= 0.6)), str(d.get("explanation", ""))


def main() -> None:
    suite = sigil.TestSuite.from_yaml(str(SUITE))
    verifier = sigil.Evaluator(evaluator_id="<evaluator>", version="draft-0", kind="llm_judge")
    candidate = {
        "agent_name": "<agent>",
        # Always send a declared agent_version. Without it Agent Observability auto-derives a version from
        # the system-prompt hash, so you can't reliably attribute scores to a version or
        # compare versions. Replace "v1" with your real version (git tag, prompt version,
        # semver...) — the "v1" fallback is a placeholder, not a version worth comparing.
        "agent_version": os.getenv("AGENT_VERSION", "v1"),
        "git_sha": os.getenv("GIT_SHA", ""),
        "model_name": os.getenv("MODEL_NAME", ""),
    }
    with sigil.experiment(name="<agent> starter", experiment_id=f"<agent>-starter-{int(time.time())}",
                          suite=suite, candidate=candidate, tags=["starter"],
                          actor=os.getenv("AGENTO11Y_INGEST_ACTOR", "ingest:sdk/python")) as exp:
        for case in suite.test_cases:
            with exp.trial(case) as trial:
                out = run_agent(case)
                trial.record_io(input=json.dumps(case.input), output=out,
                                model_provider="<provider>", model_name=os.getenv("MODEL_NAME", ""))
                score, passed, why = judge_<evaluator>(case.input, out)
                trial.final_score(score, passed=passed, explanation=why, evaluator=verifier)
                print(f"  {case.test_case_id}: score={score:.2f} passed={passed}")
    print(f"\nExperiment: {exp.url}")


if __name__ == "__main__":
    main()
```

Use a **fresh `experiment_id`** per run (a timestamp works) — reusing an id created by a
different auth actor fails with `401: experiment is owned by another actor`.

## Step 5 — Summarize and hand off

Output, in this order:

1. The picked evaluators, each with its kind and its `why` (with file:line).
   Add a one-line "once you have traffic, these criteria can also run online (Agent Observability Rules or
   guard hooks) — separate surfaces, not set up here."
2. The paths to the two written files (`evals/<agent>-starter.yaml` and
   `evals/run_experiment.py`), and a one-line reminder to review the edge/adversarial cases
   and add real ones.
3. The three things they still do to run it: fill `run_agent(case)`, tune the sketched judge
   (and add the other recommended evaluators the same way), and set credentials
   (`AGENTO11Y_ENDPOINT` + `AGENTO11Y_AUTH_TOKEN`). State the boundary explicitly: this skill only
   bootstraps the first run; for anything past that — binding an already-instrumented agent's
   real generations, auditable LLM-judge grading, cross-process verifiers, repeated-sampling
   metrics — the `agento11y-experiments` skill is the reference.
4. One line confirming nothing was created in Agent Observability — recommendations and draft files only.

## Step 6 — Offer to run it (optional, only with permission)

Only offer this for an **easy** agent (clean function seam) **in Python or Go** (the languages
with an experiments SDK). For `in-process`/`full-stack` agents, or agents in a language with no
experiments SDK (TS/Java/.NET), don't offer to run — point to the existing harness/infra (or
the subprocess-bridge option) and stop; a real run there is out of scope for this skill.

After the summary, offer to run the starter experiment for them — do not run automatically.
Ask: "Want me to try running this now?" Only proceed if they say yes.

If they accept:

1. Help fill `run_agent(case)` — wire it to the real entrypoint from Step 1, so the runner
   actually calls their agent.
2. Preflight the environment and stop with a clear ask if anything is missing:
   - `AGENTO11Y_ENDPOINT` + `AGENTO11Y_AUTH_TOKEN`. If either is unset, ask the developer for it
     proactively — `AGENTO11Y_AUTH_TOKEN` is their Grafana Cloud ingestion API key (Cloud portal →
     stack → API keys), `AGENTO11Y_ENDPOINT` their stack endpoint. **Never mint, generate, or
     fabricate a token yourself, and never invent an endpoint** — the developer owns the
     credential and supplies it; you only read it from the environment or ask for it.
   - `MODEL_NAME` is a live model (dead model ids fail with a 404 not_found).
   - A stable `AGENTO11Y_INGEST_ACTOR` so run and trials share one actor (else `401: owned by
     another actor`).
   - A declared `AGENT_VERSION` (in the candidate). Without it Agent Observability auto-derives a version
     from the system-prompt hash, and the developer can't attribute scores to a version or
     compare versions — which is the whole point of the agent's Quality view. Confirm a real
     value (git tag / prompt version / semver), don't leave it defaulted.
3. Run against the endpoint the developer configured — never a target they didn't specify.
   Start with 1–2 cases as a smoke run before the full suite.
4. Show the per-case scores and the `exp.url`, and note this published one experiment's
   scores (no tenant evaluators/rules/guards were created).

If they decline, stop after the summary.
