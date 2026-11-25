#!/usr/bin/env python3
"""Knowledge Graph agent test skill: score the runs and write the reports and a scorecard.

usage:
  python3 kgkit/score.py questions.json --out results/

Reads results/runs/*, and writes:
  results/summary.md     the short read: headline, summary table, one row per
                         question, where each side missed, caveats. Names your
                         services, so check before sending it outside your team.
  results/report.md      full detail, every run (keep it private)
  results/scorecard.json counts and numbers only, safe to share

Scoring, per run:
  correct   every must-include item is named and no must-not-include item is
  partly    some must-include items are named and no must-not-include item is
  wrong     none of the must-include items, or any must-not-include item
  no answer the run ended without an ANSWER ... END block
  failed    the agent process failed or timed out before answering, or gcx's
            login expired during the run so it couldn't query the stack
Runs that reached the graph in the without-graph configuration, or ran shell
commands the wrapper didn't log, are discarded.

Tokens: every run records input, output, cache-write and cache-read tokens.
Totals are dominated by cache reads, which say little about effort, so the
reports compare output tokens and fresh input (input + cache writes) instead.
"""
import argparse, glob, json, os, random, re, shlex, statistics
from collections import defaultdict

GRAPH_TOKENS = {"kg", "knowledge-graph"}
DENIED = "Permission to use Bash has been denied"
# gcx's error when its OAuth login can't be refreshed (same pattern in run.py)
LOGIN_EXPIRED = re.compile(r"token refresh failed: .{0,120}re-authentication required")


# ---------- reading one run ----------

def read_stream(path):
    commands, results, final, duration = [], {}, None, 0
    for line in open(path):
        try:
            m = json.loads(line)
        except ValueError:
            continue
        if m.get("type") == "system" and m.get("subtype") == "init":
            results["__model__"] = m.get("model", "")
        if m.get("type") == "assistant":
            for c in m["message"].get("content", []):
                if c.get("type") == "tool_use":
                    commands.append({"id": c["id"], "tool": c["name"], "input": c.get("input", {})})
        elif m.get("type") == "user":
            for c in m["message"].get("content", []) if isinstance(m["message"].get("content"), list) else []:
                if isinstance(c, dict) and c.get("type") == "tool_result":
                    content = c.get("content")
                    if isinstance(content, list):
                        content = " ".join(x.get("text", "") for x in content if isinstance(x, dict))
                    results[c["tool_use_id"]] = str(content or "")
        elif m.get("type") == "result":
            # A session can end with a short follow-up turn (a background task
            # finishing); each result line times only its own turn, so add them up.
            # Cost is already cumulative, so the last line's cost stands.
            duration += m.get("duration_ms") or 0
            final = m
    for c in commands:
        c["result"] = results.get(c["id"], "")
    if final is not None:
        final["duration_ms"] = duration
        final["model_id"] = results.get("__model__", "")
    return commands, final


def read_gcx_log(path):
    rows = []
    if os.path.exists(path):
        for line in open(path):
            parts = line.rstrip("\n").split("\t", 3)
            if len(parts) == 4:
                try:
                    argv = shlex.split(parts[3])
                except ValueError:
                    argv = parts[3].split()
                rows.append({"mode": parts[1], "status": parts[2], "argv": argv})
    return rows


GRAPH_SUBSTRINGS = ("asserts:", "knowledgegraph", "kg.ext.grafana", "kgatlas", "kgschema")


def is_graph_call(argv):
    return any(a in GRAPH_TOKENS for a in argv) or any(s in a for a in argv for s in GRAPH_SUBSTRINGS)


def audit(commands, gcx_rows, config):
    """Count gcx calls by kind and look for any way round the wrapper."""
    bash = [c for c in commands if c["tool"] == "Bash"]
    denied = [c for c in bash if DENIED in c["result"]]
    executed = [c for c in bash if DENIED not in c["result"]]
    ok_rows = [r for r in gcx_rows if r["status"] == "ok"]
    out = {
        "bash_commands": len(bash),
        "denied_commands": len(denied),
        "gcx_calls": len(ok_rows),
        "gcx_graph_calls": sum(is_graph_call(r["argv"]) for r in ok_rows),
        "gcx_blocked": sum(r["status"] == "blocked" for r in gcx_rows),
        "leak": False, "leak_reason": "",
    }
    if config == "without-graph" and out["gcx_graph_calls"]:
        out["leak"], out["leak_reason"] = True, "a graph command reached gcx"
    # a shell command that isn't gcx counts as a leak unless every part of it is a
    # read-only local helper (Claude Code allows these without asking, e.g. `which gcx`)
    safe = {"which", "echo", "ls", "pwd", "cat", "head", "tail", "grep", "wc", "sed", "sort", "uniq",
            "jq", "true", "type", "command", "cut", "tr", "awk", "date", "paste", "column", "nl", "fold", "rev", "cd", "pwd"}

    def stray_cmd(cmd):
        # split on shell operators outside quotes only (PromQL, Cypher and jq use | inside quotes)
        try:
            lex = shlex.shlex(cmd, posix=True, punctuation_chars=True)
            lex.whitespace_split = True
            tokens = list(lex)
        except ValueError:
            # unparseable quoting (often a Cypher or PromQL string); judge by the leading command
            return not cmd.lstrip().startswith("gcx ")
        groups, cur = [], []
        for tok in tokens:
            if tok in ("|", "||", "&&", ";", "&", "|&"):
                groups.append(cur)
                cur = []
            else:
                cur.append(tok)
        groups.append(cur)
        # a leading NAME=value is a plain shell variable (e.g. reusable gcx flags), not a command
        groups = [g[1:] if g and re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", g[0]) else g for g in groups]
        return any(g and g[0] != "gcx" and g[0] not in safe for g in groups)
    stray = [c["input"].get("command", "") for c in executed if stray_cmd(c["input"].get("command", ""))]
    if stray:
        out["leak"], out["leak_reason"] = True, "a non-gcx shell command ran: " + stray[0][:80]
    if len(executed) > len(gcx_rows):
        out["leak"], out["leak_reason"] = True, "more shell commands ran than the wrapper logged"
    return out


# ---------- scoring an answer ----------

def answer_block(text):
    blocks = re.findall(r"^\s*\**ANSWER\**\s*:?\s*$(.*?)^\s*\**END\**\s*$", text or "", re.M | re.S)
    if not blocks:
        return None
    items = []
    for line in blocks[-1].splitlines():
        s = re.sub(r"^\s*(?:[-*•]|\d+[.)])\s*", "", line).replace("`", "").replace("**", "").strip()
        if s:
            items.append(s.lower())
    return items


def matches(alias, item):
    return re.search(r"(?<![a-z0-9_-])" + re.escape(alias.lower()) + r"(?![a-z0-9_-])", item) is not None


def score_answer(q, text):
    items = answer_block(text)
    if items is None:
        return "no answer", {}
    if q.get("answer_type") == "number":
        nums = re.findall(r"-?\d+(?:\.\d+)?", " ".join(items).replace(",", ""))
        if not nums:
            return "wrong", {"value": None}
        v = float(nums[0])
        return ("correct" if abs(v - q["value"]) <= q.get("tolerance", 0) else "wrong"), {"value": v}
    found = [g for g in q["must_include"] if any(matches(a, it) for a in g for it in items)]
    bad = [g for g in q.get("must_not_include", []) if any(matches(a, it) for a in g for it in items)]
    extra = [it for it in items if not any(matches(a, it) for g in q["must_include"] for a in g)]
    detail = {"items": items, "found": [g[0] for g in found], "forbidden": [g[0] for g in bad]}
    if bad or not found:
        return "wrong", detail
    if len(found) == len(q["must_include"]) and not (q.get("exact") and extra):
        return "correct", detail
    return "partly", detail


# ---------- statistics ----------

def median(xs):
    return statistics.median(xs) if xs else None


def p_at_least(k, n):
    """Chance of winning at least k of n questions if the graph made no difference (a coin flip each)."""
    from math import comb
    return sum(comb(n, i) for i in range(k, n + 1)) / 2 ** n


def bootstrap_ratio(per_q, key, reps=5000, seed=1):
    """95% interval for mean(with)/mean(without), resampling questions, not runs."""
    qs = [q for q in per_q if per_q[q]["with-graph"][key] and per_q[q]["without-graph"][key]]
    if len(qs) < 2:
        return None
    rnd, ratios = random.Random(seed), []
    for _ in range(reps):
        pick = [rnd.choice(qs) for _ in qs]
        w = statistics.mean(x for q in pick for x in per_q[q]["with-graph"][key])
        wo = statistics.mean(x for q in pick for x in per_q[q]["without-graph"][key])
        ratios.append(w / wo)
    ratios.sort()
    return ratios[int(0.025 * reps)], ratios[int(0.975 * reps)]


def fmt(x, nd=2):
    return "-" if x is None else f"{x:.{nd}f}"


# ---------- main ----------

CONFIGS = ("without-graph", "with-graph")
METRICS = ("seconds", "turns", "gcx_calls", "gcx_graph_calls", "output_tokens", "fresh_input_tokens", "cache_read_tokens")
MARK = {"correct": "●", "partly": "◐", "wrong": "✕", "no answer": "○", "failed": "!"}
MARK_ORDER = ["correct", "partly", "wrong", "no answer", "failed"]


def ratio(w, wo):
    return None if w is None or not wo else w / wo


def fmt_ratio(r):
    return "-" if r is None else f"{r:.2f}×"


def fmt_tokens(x):
    if x is None:
        return "-"
    return f"{x / 1000:.1f}k" if x >= 1000 else f"{x:.0f}"


def marks(verdicts):
    return "".join(MARK[v] for v in sorted(verdicts, key=MARK_ORDER.index)) or "-"


def answer_summary(q):
    if q.get("answer_type") == "number":
        return f"the number {q.get('value')}"
    n = len(q.get("must_include", []))
    s = f"{n} item{'s' if n != 1 else ''}"
    if q.get("exact"):
        s += ", no extras allowed"
    if q.get("must_not_include"):
        s += f", {len(q['must_not_include'])} to leave out"
    return s


def read_runs(args, qs):
    runs = []
    for meta_path in sorted(glob.glob(os.path.join(args.out, "runs", "*.meta.json"))):
        meta = json.load(open(meta_path))
        base = meta_path[:-len(".meta.json")]
        commands, final = read_stream(base + ".stream.jsonl")
        aud = audit(commands, read_gcx_log(base + ".gcx.log"), meta["config"])
        q = qs[meta["question_id"]]
        text = (final or {}).get("result", "")
        login_expired = any(LOGIN_EXPIRED.search(c["result"]) for c in commands)
        if login_expired:
            verdict, detail = "failed", {}  # it couldn't query the stack, so its answer says nothing
        elif final:
            verdict, detail = score_answer(q, text)
        elif meta.get("status") in ("failed", "timeout"):
            verdict, detail = "failed", {}
        else:
            verdict, detail = "no answer", {}
        # usage on the last result line; it covers the session, like total_cost_usd
        usage = (final or {}).get("usage", {}) or {}
        fresh = None
        if usage.get("input_tokens") is not None:
            fresh = (usage.get("input_tokens") or 0) + (usage.get("cache_creation_input_tokens") or 0)
        runs.append({**meta, **aud, "verdict": verdict, "detail": detail, "login_expired": login_expired,
                     "seconds": ((final or {}).get("duration_ms") or meta["wall_seconds"] * 1000) / 1000,
                     "turns": (final or {}).get("num_turns"),
                     "model_id": (final or {}).get("model_id"),
                     "input_tokens": usage.get("input_tokens"),
                     "output_tokens": usage.get("output_tokens"),
                     "cache_write_tokens": usage.get("cache_creation_input_tokens"),
                     "cache_read_tokens": usage.get("cache_read_input_tokens"),
                     "fresh_input_tokens": fresh,
                     "budget_hit": (final or {}).get("subtype") == "error_max_budget_usd",
                     "final_text_tail": text[-600:]})
    return runs


def group(kept):
    per_q = defaultdict(lambda: defaultdict(lambda: defaultdict(list)))
    for r in kept:
        d = per_q[r["question_id"]][r["config"]]
        d["verdicts"].append(r["verdict"])
        if r["verdict"] == "failed":
            continue  # a crashed run says nothing about time or tokens
        for k in METRICS:
            if r.get(k) is not None:
                d[k].append(r[k])
    return per_q


def plural(n, word):
    return f"{n} {word}{'' if n == 1 else 's'}"


def discarded_text(runs):
    """Which runs were discarded and why, e.g. '1 with-graph run because more shell commands ran than the wrapper logged'."""
    gone = defaultdict(int)
    for r in runs:
        if r["leak"]:
            gone[(r["config"], r["leak_reason"].split(":")[0])] += 1
    return "; ".join(f"{plural(n, c + ' run')} because {reason}" for (c, reason), n in sorted(gone.items()))


def graph_used(per_q, qid):
    """The agent used the graph on a question if the median with-graph run called it at least once."""
    calls = per_q[qid]["with-graph"]["gcx_graph_calls"]
    return bool(calls) and median(calls) >= 1


def pooled(per_q, qids, c, key):
    xs = [x for q in qids for x in per_q[q][c][key]]
    return median(xs)


def write_summary(path, spec, plan, health, qs, per_q, runs, kept):
    ids = list(qs)
    used = [q for q in ids if graph_used(per_q, q)]
    unused = [q for q in ids if q not in used]
    reps = max((len(per_q[q][c]["verdicts"]) for q in ids for c in CONFIGS), default=0)
    n_runs = len(kept)

    def correct(qids, c):
        return sum(per_q[q][c]["verdicts"].count("correct") for q in qids), sum(len(per_q[q][c]["verdicts"]) for q in qids)

    focus = used or ids
    cw, nw = correct(focus, "with-graph")
    cwo, nwo = correct(focus, "without-graph")
    t_w, t_wo = pooled(per_q, focus, "with-graph", "seconds"), pooled(per_q, focus, "without-graph", "seconds")
    o_w, o_wo = pooled(per_q, focus, "with-graph", "output_tokens"), pooled(per_q, focus, "without-graph", "output_tokens")
    f_w, f_wo = pooled(per_q, focus, "with-graph", "fresh_input_tokens"), pooled(per_q, focus, "without-graph", "fresh_input_tokens")
    scope = (f"the {len(used)} question{'s' if len(used) != 1 else ''} where the agent used the graph ({', '.join(used)})"
             if used else "all questions (the agent never used the graph, so this compares two telemetry-only runs)")

    # headline: say what happened on the questions the graph could affect, in plain words
    if not used:
        head = "The agent never used the graph, so this test can't show what the graph adds"
    elif cw > cwo:
        head = f"Where the agent used the graph, it got more answers fully right: {cw} of {nw} runs against {cwo} of {nwo}"
    elif cw == cwo and t_w is not None and t_wo and t_w < t_wo:
        head = f"Where the agent used the graph, answers were as accurate and {fmt_ratio(ratio(t_w, t_wo))} the time"
    elif cw == cwo:
        head = f"Where the agent used the graph, accuracy was the same: {cw} of {nw} runs fully right on both sides"
    else:
        head = f"Where the agent used the graph, it got fewer answers fully right: {cw} of {nw} runs against {cwo} of {nwo}"

    L = [f"# {head}", "",
         f"{spec.get('stack')} · window {spec['window']['from']} to {spec['window']['to']} · "
         f"{plan.get('model', '?')}, effort {plan.get('effort', '?')} · {len(ids)} questions × {reps} runs × 2 sides = {n_runs} runs kept", "",
         "## The question", "",
         "Does an agent working through gcx give better answers when it can use the Knowledge Graph? Both sides got the same "
         "question, time window and notes, and the same telemetry. Only one side could call the graph"
         + {"first": ", and it was told to start there.", "available": ", and it was told the graph was available.",
            "none": ", and it wasn't told the graph existed."}.get(plan.get("graph_prompt"), "."), "",
         f"## Summary, on {scope}", "",
         "| | With the graph | Without | Ratio |", "|---|---|---|---|",
         f"| Fully correct | {cw} of {nw} | {cwo} of {nwo} | |",
         f"| Median time per run | {fmt(t_w, 0)} s | {fmt(t_wo, 0)} s | {fmt_ratio(ratio(t_w, t_wo))} |",
         f"| Median output tokens | {fmt_tokens(o_w)} | {fmt_tokens(o_wo)} | {fmt_ratio(ratio(o_w, o_wo))} |",
         f"| Median fresh input tokens | {fmt_tokens(f_w)} | {fmt_tokens(f_wo)} | {fmt_ratio(ratio(f_w, f_wo))} |", ""]
    if len(focus) >= 3:
        ci = bootstrap_ratio({q: per_q[q] for q in focus}, "seconds")
        if ci:
            L.append(f"95% interval for the time ratio, resampling questions: {ci[0]:.2f}× to {ci[1]:.2f}×.")
            L.append("")

    L += ["## Question by question", "",
          f"Each mark is one run: {MARK['correct']} correct, {MARK['partly']} partly, {MARK['wrong']} wrong, "
          f"{MARK['no answer']} no answer, {MARK['failed']} failed. Times and tokens are medians, without → with the graph.", "",
          "| | What we asked | Answer | Without | With | Time (s) | Output tokens | Graph calls |",
          "|---|---|---|---|---|---|---|---|"]
    for q in ids:
        w, wo = per_q[q]["with-graph"], per_q[q]["without-graph"]
        L.append(f"| {q} | {qs[q]['question']} | {answer_summary(qs[q])} | {marks(wo['verdicts'])} | {marks(w['verdicts'])} | "
                 f"{fmt(median(wo['seconds']), 0)} → {fmt(median(w['seconds']), 0)} | "
                 f"{fmt_tokens(median(wo['output_tokens']))} → {fmt_tokens(median(w['output_tokens']))} | "
                 f"{fmt(median(w['gcx_graph_calls']), 0)} |")
    L.append("")

    L += ["## Where each side missed", ""]
    for c, label in (("without-graph", "Without the graph"), ("with-graph", "With the graph")):
        misses = []
        for q in ids:
            v = per_q[q][c]["verdicts"]
            bits = [f"{v.count(k)} {k}" for k in MARK_ORDER[1:] if v.count(k)]
            if bits:
                answered_wrong = any(v.count(k) for k in ("partly", "wrong", "no answer"))
                tag = " (graph not called, so run-to-run variation)" if q not in used and answered_wrong else ""
                misses.append(f"- **{q}**: {', '.join(bits)}{tag}")
        n_miss = sum(len(per_q[q][c]["verdicts"]) - per_q[q][c]["verdicts"].count("correct") for q in ids)
        L += [f"**{label}: {plural(n_miss, 'run')} not fully right.**", ""] + (misses or ["- none"]) + [""]

    easy = [q for q in ids if all(per_q[q][c]["verdicts"] and set(per_q[q][c]["verdicts"]) == {"correct"} for c in CONFIGS)]
    cav = [f"- **Small sample.** {reps} runs per question per side: one odd run moves a question's score by 1/{reps or 1}. Read differences as a direction."]
    if unused:
        cav.append(f"- **Graph not used on {len(unused)} of {len(ids)} questions** ({', '.join(unused)}). Differences there come from run-to-run variation, not the graph, so the summary leaves them out.")
    if easy:
        cav.append(f"- **{len(easy)} of {len(ids)} questions were answered correctly on every run by both sides** ({', '.join(easy)}). They can't separate the two; harder questions would.")
    failed = sum(r["verdict"] == "failed" for r in runs)
    if failed:
        cav.append(f"- **{plural(failed, 'run')} failed** before answering, and {'is' if failed == 1 else 'are'} left out of time and tokens. Check `report.md` before reading the results.")
    expired = sum(r.get("login_expired", False) for r in runs)
    if expired:
        cav.append(f"- **The gcx login expired in {plural(expired, 'run')}.** Run `gcx login`, then the same run.py command again: "
                   "it reruns only those.")
    refused = [r for r in kept if r["config"] == "without-graph" and r.get("gcx_blocked")]
    if refused:
        n = sum(r["gcx_blocked"] for r in refused)
        cav.append(f"- **{plural(n, 'command')} refused in {plural(len(refused), 'without-graph run')}**, "
                   "where the agent reached for graph data. Each refusal costs a turn, which slows that side a little.")
    if len(runs) - len(kept):
        cav.append(f"- **{plural(len(runs) - len(kept), 'run')} discarded:** {discarded_text(runs)}. Discarded runs are left out of every count.")
    if health:
        cav.append(f"- **Graph health: {health['rating']}.** {health['summary']}")
        for p in health.get("groups", []):
            cav.append(f"  - {p['property']}={p['value']}: {p['connected']} of {p['services']} services have relationships in the graph.")
    if spec.get("answer_key_written"):
        cav.append(f"- **Answer keys:** {spec['answer_key_written']}")
    cav.append("- **Tokens:** output and fresh input (input + cache writes). Cache reads are left out: they dominate totals and say little about effort.")
    L += ["## Caveats", ""] + cav + [""]

    nxt = []
    if easy and len(easy) * 2 >= len(ids):
        nxt.append("- Swap the questions both sides always get right for multi-hop ones: blast radius, hidden dependencies, an incident with a written root cause.")
    if health and health.get("rating") != "green":
        nxt.append("- Fix the graph warnings above, then rerun the affected questions.")
    if plan.get("graph_prompt") == "first":
        nxt.append("- Rerun with `--graph-prompt none` to see whether the agent finds the graph without being told.")
    if nxt:
        L += ["## Next", ""] + nxt + [""]
    with open(path, "w") as fh:
        fh.write("\n".join(L) + "\n")
    return L


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("questions")
    ap.add_argument("--out", required=True)
    args = ap.parse_args()
    spec = json.load(open(args.questions))
    qs = {q["id"]: q for q in spec["questions"]}
    plan = json.load(open(os.path.join(args.out, "plan.json"))) if os.path.exists(os.path.join(args.out, "plan.json")) else {}
    health = json.load(open(os.path.join(args.out, "health.json"))) if os.path.exists(os.path.join(args.out, "health.json")) else None

    runs = read_runs(args, qs)
    run_qids = {r["question_id"] for r in runs}
    qs = {k: v for k, v in qs.items() if k in run_qids}  # only the questions that ran
    kept = [r for r in runs if not r["leak"]]
    per_q = group(kept)

    summary = write_summary(os.path.join(args.out, "summary.md"), spec, plan, health, qs, per_q, runs, kept)

    lines = [f"# Knowledge Graph agent test skill: full results", "",
             f"Stack: {spec.get('stack')}. Window: {spec['window']['from']} to {spec['window']['to']}.",
             f"Agent: Claude Code {plan.get('claude_version', '?')}, model {', '.join(sorted({r['model_id'] or plan.get('model', '?') for r in runs})) or plan.get('model', '?')}, effort {plan.get('effort', '?')}. gcx: {plan.get('gcx_version', '?')}. Graph prompt: {plan.get('graph_prompt', '?')}.",
             f"Runs: {len(runs)} recorded, {len(runs) - len(kept)} discarded"
             + (f" ({discarded_text(runs)})" if len(runs) - len(kept) else "")
             + f", {sum(r['verdict'] == 'failed' for r in runs)} failed.", ""]
    if health:
        lines += [f"Graph health: **{health['rating']}**. {health['summary']}", ""]
    lines += ["## Per question", "",
              "| Question | Type | Config | Runs | Correct | Partly | Wrong | No answer | Failed | Median seconds | Median output tokens | Median fresh input | Median cache reads | Median gcx calls | of which graph |",
              "|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|"]
    for qid in qs:
        for c in CONFIGS:
            d = per_q[qid][c]
            v = d["verdicts"]
            lines.append(f"| {qid} | {qs[qid]['type']} | {c} | {len(v)} | {v.count('correct')} | {v.count('partly')} | {v.count('wrong')} | {v.count('no answer')} | {v.count('failed')} | "
                         f"{fmt(median(d['seconds']), 0)} | {fmt_tokens(median(d['output_tokens']))} | {fmt_tokens(median(d['fresh_input_tokens']))} | {fmt_tokens(median(d['cache_read_tokens']))} | "
                         f"{fmt(median(d['gcx_calls']), 0)} | {fmt(median(d['gcx_graph_calls']), 0)} |")
    lines += ["", "## Every run", "",
              "| Run | Verdict | Seconds | Turns | Output tokens | Fresh input | Cache reads | gcx calls | graph | blocked | denied | Leak | Answer |",
              "|---|---|---|---|---|---|---|---|---|---|---|---|---|"]
    for r in runs:
        ans = "; ".join(r["detail"].get("items", [])[:8]) if r["detail"].get("items") else str(r["detail"].get("value", ""))
        lines.append(f"| {r['run_id']} | {r['verdict']} | {fmt(r['seconds'], 0)} | {r['turns']} | {fmt_tokens(r['output_tokens'])} | {fmt_tokens(r['fresh_input_tokens'])} | {fmt_tokens(r['cache_read_tokens'])} | "
                     f"{r['gcx_calls']} | {r['gcx_graph_calls']} | {r['gcx_blocked']} | {r['denied_commands']} | {r['leak_reason'] or 'no'} | {ans[:160]} |")
    lines += ["", "## Answer keys", ""]
    for q in qs.values():
        lines.append(f"- **{q['id']} ({q['type']})** {q['question']} Source: {q.get('source') or 'not given in the questions file'}")
    with open(os.path.join(args.out, "report.md"), "w") as fh:
        fh.write("\n".join(lines) + "\n")

    faster = sum(1 for q in qs if graph_used(per_q, q) and per_q[q]["with-graph"]["seconds"] and per_q[q]["without-graph"]["seconds"]
                 and median(per_q[q]["with-graph"]["seconds"]) < median(per_q[q]["without-graph"]["seconds"]))
    scorecard = {
        "kit_version": "0.2", "agent": "claude-code", "claude_version": plan.get("claude_version"),
        "model": plan.get("model"), "effort": plan.get("effort"), "gcx_version": plan.get("gcx_version"),
        "graph_prompt": plan.get("graph_prompt"),
        "graph_health": health["rating"] if health else "not-checked",
        "runs_recorded": len(runs), "runs_discarded_for_leakage": len(runs) - len(kept),
        "questions": [{"type": qs[qid]["type"], **{c: {
            "runs": len(per_q[qid][c]["verdicts"]),
            "correct": per_q[qid][c]["verdicts"].count("correct"),
            "partly": per_q[qid][c]["verdicts"].count("partly"),
            "wrong": per_q[qid][c]["verdicts"].count("wrong"),
            "no_answer": per_q[qid][c]["verdicts"].count("no answer"),
            "failed": per_q[qid][c]["verdicts"].count("failed"),
            "median_seconds": median(per_q[qid][c]["seconds"]),
            "median_output_tokens": median(per_q[qid][c]["output_tokens"]),
            "median_fresh_input_tokens": median(per_q[qid][c]["fresh_input_tokens"]),
            "median_gcx_calls": median(per_q[qid][c]["gcx_calls"]),
            "median_graph_calls": median(per_q[qid][c]["gcx_graph_calls"])} for c in CONFIGS}}
            for qid in qs],
        "questions_graph_used": sum(graph_used(per_q, q) for q in qs),
        "questions_faster_with_graph": faster, "questions_compared": len(qs),
        "comments": "",
    }
    with open(os.path.join(args.out, "scorecard.json"), "w") as fh:
        json.dump(scorecard, fh, indent=1)
    print("\n".join(summary))
    print(f"\nWrote {os.path.join(args.out, 'summary.md')}, report.md and scorecard.json")


if __name__ == "__main__":
    main()
