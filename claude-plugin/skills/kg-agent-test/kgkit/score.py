#!/usr/bin/env python3
"""Knowledge Graph agent test skill: score the runs and write a report and a scorecard.

usage:
  python3 kgkit/score.py questions.json --out results/

Reads results/runs/*, writes results/report.md (full detail, keep it private)
and results/scorecard.json (counts and numbers only, safe to share).

Scoring, per run:
  correct   every must-include item is named and no must-not-include item is
  partly    some must-include items are named and no must-not-include item is
  wrong     none of the must-include items, or any must-not-include item
  no answer the run ended without an ANSWER ... END block (reported separately)
Runs that reached the graph in the without-graph configuration are discarded.
"""
import argparse, glob, json, os, random, re, shlex, statistics
from collections import defaultdict

GRAPH_TOKENS = {"kg", "knowledge-graph"}
DENIED = "Permission to use Bash has been denied"


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
        out["leak"], out["leak_reason"] = True, "graph command reached gcx"
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

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("questions")
    ap.add_argument("--out", required=True)
    args = ap.parse_args()
    spec = json.load(open(args.questions))
    qs = {q["id"]: q for q in spec["questions"]}
    plan = json.load(open(os.path.join(args.out, "plan.json"))) if os.path.exists(os.path.join(args.out, "plan.json")) else {}
    health = json.load(open(os.path.join(args.out, "health.json"))) if os.path.exists(os.path.join(args.out, "health.json")) else None

    runs = []
    for meta_path in sorted(glob.glob(os.path.join(args.out, "runs", "*.meta.json"))):
        meta = json.load(open(meta_path))
        base = meta_path[:-len(".meta.json")]
        commands, final = read_stream(base + ".stream.jsonl")
        aud = audit(commands, read_gcx_log(base + ".gcx.log"), meta["config"])
        q = qs[meta["question_id"]]
        text = (final or {}).get("result", "")
        verdict, detail = score_answer(q, text) if final else ("no answer", {})
        usage = (final or {}).get("usage", {})
        runs.append({**meta, **aud, "verdict": verdict, "detail": detail,
                     "cost_usd": (final or {}).get("total_cost_usd"),
                     "seconds": ((final or {}).get("duration_ms") or meta["wall_seconds"] * 1000) / 1000,
                     "turns": (final or {}).get("num_turns"),
                     "model_id": (final or {}).get("model_id"),
                     "output_tokens": usage.get("output_tokens"),
                     "budget_hit": (final or {}).get("subtype") == "error_max_budget_usd",
                     "final_text_tail": text[-600:]})

    kept = [r for r in runs if not r["leak"]]
    per_q = defaultdict(lambda: defaultdict(lambda: defaultdict(list)))
    for r in kept:
        d = per_q[r["question_id"]][r["config"]]
        d["verdicts"].append(r["verdict"])
        for k in ("cost_usd", "seconds", "turns", "gcx_calls", "gcx_graph_calls"):
            if r.get(k) is not None:
                d[k].append(r[k])

    lines = [f"# Knowledge Graph agent test skill: results", "",
             f"Stack: {spec.get('stack')}. Window: {spec['window']['from']} to {spec['window']['to']}.",
             f"Agent: Claude Code {plan.get('claude_version', '?')}, model {', '.join(sorted({r['model_id'] or plan.get('model', '?') for r in runs})) or plan.get('model', '?')}, effort {plan.get('effort', '?')}. gcx: {plan.get('gcx_version', '?')}.",
             f"Runs: {len(runs)} recorded, {len(runs) - len(kept)} discarded for reaching the graph in the without-graph configuration.", ""]
    if health:
        lines += [f"Graph health: **{health['rating']}**. {health['summary']}", ""]

    lines += ["## Per question", "",
              "| Question | Type | Config | Runs | Correct | Partly | Wrong | No answer | Median cost | Median seconds | Median gcx calls | of which graph |",
              "|---|---|---|---|---|---|---|---|---|---|---|---|"]
    won_both = won_cost = won_time = 0
    n_compared = 0
    for qid in qs:
        for c in ("without-graph", "with-graph"):
            d = per_q[qid][c]
            v = d["verdicts"]
            lines.append(f"| {qid} | {qs[qid]['type']} | {c} | {len(v)} | {v.count('correct')} | {v.count('partly')} | {v.count('wrong')} | {v.count('no answer')} | "
                         f"{fmt(median(d['cost_usd']))} | {fmt(median(d['seconds']), 0)} | {fmt(median(d['gcx_calls']), 0)} | {fmt(median(d['gcx_graph_calls']), 0)} |")
        w, wo = per_q[qid]["with-graph"], per_q[qid]["without-graph"]
        if w["cost_usd"] and wo["cost_usd"]:
            n_compared += 1
            cheaper = median(w["cost_usd"]) < median(wo["cost_usd"])
            faster = median(w["seconds"]) < median(wo["seconds"])
            won_cost += cheaper
            won_time += faster
            won_both += cheaper and faster

    def totals(c, key):
        xs = [x for q in per_q for x in per_q[q][c][key]]
        return statistics.mean(xs) if xs else None

    tot = {c: {"runs": sum(len(per_q[q][c]["verdicts"]) for q in per_q),
               "correct": sum(per_q[q][c]["verdicts"].count("correct") for q in per_q),
               "mean_cost_usd": totals(c, "cost_usd"), "mean_seconds": totals(c, "seconds"),
               "mean_gcx_calls": totals(c, "gcx_calls")} for c in ("without-graph", "with-graph")}
    ci_cost = bootstrap_ratio(per_q, "cost_usd")
    ci_time = bootstrap_ratio(per_q, "seconds")
    lines += ["", "## Overall", "",
              "| | Without the graph | With the graph |", "|---|---|---|",
              f"| Runs kept | {tot['without-graph']['runs']} | {tot['with-graph']['runs']} |",
              f"| Correct | {tot['without-graph']['correct']} of {tot['without-graph']['runs']} | {tot['with-graph']['correct']} of {tot['with-graph']['runs']} |",
              f"| Average cost per run | ${fmt(tot['without-graph']['mean_cost_usd'])} | ${fmt(tot['with-graph']['mean_cost_usd'])} |",
              f"| Average seconds per run | {fmt(tot['without-graph']['mean_seconds'], 0)} | {fmt(tot['with-graph']['mean_seconds'], 0)} |",
              f"| Average gcx calls per run | {fmt(tot['without-graph']['mean_gcx_calls'], 1)} | {fmt(tot['with-graph']['mean_gcx_calls'], 1)} |", ""]
    if n_compared:
        lines += [f"With the graph, the median run was cheaper on {won_cost} of {n_compared} questions, faster on {won_time}, and both on {won_both}.",
                  f"If the graph made no difference, winning at least {won_both} of {n_compared} on both counts would happen by chance about {p_at_least(won_both, n_compared):.0%} of the time (treating each question as a coin flip, which is generous to chance).", ""]
    if ci_cost:
        lines.append(f"Cost with the graph as a share of cost without: {fmt(tot['with-graph']['mean_cost_usd'] / tot['without-graph']['mean_cost_usd'])}x, 95% interval {fmt(ci_cost[0])} to {fmt(ci_cost[1])} (resampling questions).")
    if ci_time:
        lines.append(f"Time with the graph as a share of time without: {fmt(tot['with-graph']['mean_seconds'] / tot['without-graph']['mean_seconds'])}x, 95% interval {fmt(ci_time[0])} to {fmt(ci_time[1])}.")
    lines += ["", "With five questions these intervals are wide; read them as a direction, not a measurement.", "",
              "## Every run", "", "| Run | Verdict | Cost | Seconds | Turns | gcx calls | graph | blocked | denied | Leak | Answer |", "|---|---|---|---|---|---|---|---|---|---|---|"]
    for r in runs:
        ans = "; ".join(r["detail"].get("items", [])[:8]) if r["detail"].get("items") else str(r["detail"].get("value", ""))
        lines.append(f"| {r['run_id']} | {r['verdict']} | {fmt(r['cost_usd'])} | {fmt(r['seconds'], 0)} | {r['turns']} | {r['gcx_calls']} | {r['gcx_graph_calls']} | {r['gcx_blocked']} | {r['denied_commands']} | {r['leak_reason'] or 'no'} | {ans[:160]} |")
    lines += ["", "## Answer keys", ""]
    for q in qs.values():
        lines.append(f"- **{q['id']} ({q['type']})** {q['question']} Source: {q.get('source', '')}")
    with open(os.path.join(args.out, "report.md"), "w") as fh:
        fh.write("\n".join(lines) + "\n")

    scorecard = {
        "kit_version": "0.1", "agent": "claude-code", "claude_version": plan.get("claude_version"),
        "model": plan.get("model"), "effort": plan.get("effort"), "gcx_version": plan.get("gcx_version"),
        "graph_health": health["rating"] if health else "not-checked",
        "runs_recorded": len(runs), "runs_discarded_for_leakage": len(runs) - len(kept),
        "questions": [{"type": qs[qid]["type"], **{c: {
            "runs": len(per_q[qid][c]["verdicts"]),
            "correct": per_q[qid][c]["verdicts"].count("correct"),
            "partly": per_q[qid][c]["verdicts"].count("partly"),
            "wrong": per_q[qid][c]["verdicts"].count("wrong"),
            "no_answer": per_q[qid][c]["verdicts"].count("no answer"),
            "median_cost_usd": median(per_q[qid][c]["cost_usd"]),
            "median_seconds": median(per_q[qid][c]["seconds"]),
            "median_gcx_calls": median(per_q[qid][c]["gcx_calls"])} for c in ("without-graph", "with-graph")}}
            for qid in qs],
        "questions_won_on_cost_and_time": won_both, "questions_compared": n_compared,
        "comments": "",
    }
    with open(os.path.join(args.out, "scorecard.json"), "w") as fh:
        json.dump(scorecard, fh, indent=1)
    print("\n".join(lines[:60]))


if __name__ == "__main__":
    main()
