#!/usr/bin/env python3
"""Knowledge Graph agent test skill: run each question with and without the graph.

Runs Claude Code headless (`claude -p`) once per question, configuration and
repetition. Every run starts in a new empty directory, with no plugins, skills,
MCP servers, CLAUDE.md or memory, and can run nothing except gcx (through the
skill's logging wrapper) and read the files gcx writes into that directory.

usage:
  python3 kgkit/run.py questions.json --out results/ [--reps 5] [--model claude-opus-5-5]
                       [--effort high] [--parallel 1] [--pause 15] [--budget-usd 3] [--only Q1,Q2]
                       [--graph-prompt first|available|none]
                       [--configs with-graph,without-graph] [--dry-run]

Python 3.9+ and the standard library only.
"""
import argparse, json, os, shutil, subprocess, sys, tempfile, time
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone

KIT = os.path.dirname(os.path.abspath(__file__))
WRAPPER_DIR = os.path.join(KIT, "bin")
CONFIGS = ("without-graph", "with-graph")

FORMAT = {
    "set": "End your answer with the list you found, one item per line, between a line that says ANSWER and a line that says END.",
    "number": "End your answer with the number alone, between a line that says ANSWER and a line that says END.",
}
# The without-graph prompt carries no sources line. An earlier version told it to
# "answer from metrics, traces and logs only", and agents read that as a to-do list:
# about 3.8 extra trace queries and 17-25 s per run, which inflated the graph's lead.
SOURCES = {
    "without-graph": "",
    "with-graph": "",  # set from --graph-prompt in main()
}
GRAPH_PROMPTS = {
    # default: the agent is told the graph exists and to start there, so the test
    # measures what the graph adds when it is used
    "first": ("The Knowledge Graph is available through `gcx kg`. It holds the services, databases "
              "and other components in this stack and how they connect. Start with it to find the "
              "services and relationships involved, then confirm with metrics, traces or logs where "
              "you need to."),
    # tells the agent the graph is there but leaves the choice to it
    "available": ("The Knowledge Graph is available through `gcx kg`. It holds the services, databases "
                  "and other components in this stack, how they connect, and their health. Use it where "
                  "it helps, alongside metrics, traces and logs."),
    # says nothing: tests whether gcx's own help leads the agent to the graph
    "none": "",
}


def build_prompt(spec, q, config):
    """The two prompts differ only in the with-graph sources line. Everything else is identical."""
    parts = [f"Using gcx against my Grafana Cloud stack, answer this question: {q['question']}",
             f"Time window: {spec['window']['from']} to {spec['window']['to']} (UTC). Use exactly this window."]
    if SOURCES[config]:
        parts.append(SOURCES[config])
    if spec.get("stack_notes"):
        parts.append("Notes about this stack: " + spec["stack_notes"])
    parts.append(q.get("answer_format") or FORMAT[q.get("answer_type", "set")])
    return "\n\n".join(parts)


def child_env(real_gcx, config, log_path, context, workdir):
    # drop the parent session's Claude Code variables so the child starts clean
    env = {k: v for k, v in os.environ.items()
           if not k.startswith(("CLAUDECODE", "CLAUDE_CODE", "CLAUDE_AGENT"))}
    env.update(KGKIT_REAL_GCX=real_gcx, KGKIT_MODE=config, KGKIT_LOG=log_path,
               PATH=WRAPPER_DIR + os.pathsep + env.get("PATH", ""),
               TMPDIR=workdir)  # gcx writes large results to TMPDIR; keep them readable
    if context:
        env["KGKIT_CONTEXT"] = context
    if COMMANDS_FILE:
        env["KGKIT_COMMANDS"] = COMMANDS_FILE
    return env


COMMANDS_FILE = None


def write_command_list(real_gcx, out):
    """gcx's own list of command paths, so the wrapper can tell a command's verb from
    the arguments after it when it blocks commands that change the stack."""
    p = subprocess.run([real_gcx, "commands", "--flat", "--jq", ".commands[].full_path"],
                       capture_output=True, text=True, timeout=120)
    paths = [json.loads(l) if l.startswith('"') else l for l in p.stdout.splitlines() if l.strip().startswith(('"gcx', "gcx"))]
    if p.returncode != 0 or not paths:
        print("warning: couldn't read gcx's command list; the wrapper will block more than it needs to", file=sys.stderr)
        return None
    path = os.path.join(os.path.abspath(out), "gcx-commands.txt")
    with open(path, "w") as fh:
        fh.write("\n".join(paths) + "\n")
    return path


def claude_cmd(prompt, args):
    return ["claude", "-p", prompt,
            "--output-format", "stream-json", "--verbose",
            "--no-session-persistence", "--safe-mode", "--restricted", "--strict-mcp-config",
            "--disable-slash-commands",
            "--tools", "Bash,Read,Grep,Glob",
            "--allowedTools", "Bash(gcx *)", "Read", "Grep", "Glob",
            "--permission-mode", "dontAsk",
            "--model", args.model, "--effort", args.effort,
            "--max-budget-usd", str(args.budget_usd)]


def run_one(job, spec, args, real_gcx):
    q, config, rep = job["q"], job["config"], job["rep"]
    run_id = f"{q['id']}__{config}__r{rep}"
    runs_dir = os.path.join(args.out, "runs")
    stream_path = os.path.join(runs_dir, run_id + ".stream.jsonl")
    meta_path = os.path.join(runs_dir, run_id + ".meta.json")
    if os.path.exists(meta_path) and not args.force:
        return run_id, "skipped (already done)"
    workdir = tempfile.mkdtemp(prefix="kgkit-")
    prompt = build_prompt(spec, q, config)
    env = child_env(real_gcx, config, os.path.join(runs_dir, run_id + ".gcx.log"), spec.get("gcx_context"), workdir)
    if q.get("block_commands"):
        env["KGKIT_BLOCK_ALWAYS"] = ",".join(q["block_commands"])
    started = time.time()
    status = "ok"
    try:
        with open(stream_path, "w") as fh:
            p = subprocess.run(claude_cmd(prompt, args), cwd=workdir, env=env, stdout=fh,
                               stderr=subprocess.PIPE, text=True, timeout=args.timeout)
        exit_code, stderr = p.returncode, p.stderr[-2000:]
        if exit_code != 0:
            status = "failed"  # used to be reported as ok, which hid version and flag errors
    except subprocess.TimeoutExpired:
        exit_code, stderr, status = None, "", "timeout"
    meta = {"run_id": run_id, "question_id": q["id"], "config": config, "rep": rep,
            "model": args.model, "effort": args.effort, "prompt": prompt, "graph_prompt": args.graph_prompt,
            "started_utc": datetime.fromtimestamp(started, timezone.utc).isoformat(),
            "wall_seconds": round(time.time() - started, 1), "exit_code": exit_code,
            "status": status, "stderr_tail": stderr, "workdir": workdir}
    with open(meta_path, "w") as fh:
        json.dump(meta, fh, indent=1)
    shutil.rmtree(workdir, ignore_errors=True)  # nothing a later run could read
    if args.pause:
        time.sleep(args.pause)  # spread the load on the stack between runs
    return run_id, f"{status} in {meta['wall_seconds']}s"


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("questions")
    ap.add_argument("--out", required=True)
    ap.add_argument("--reps", type=int, default=5,
                    help="runs per question per configuration; with 3, one odd run moves a question's score by a third")
    ap.add_argument("--model", default="claude-opus-5-5",
                    help="a full model ID, so the pilot and the full run use the same model; stronger models "
                         "make more use of the graph, so a weaker one understates what it adds")
    ap.add_argument("--effort", default="high")
    ap.add_argument("--parallel", type=int, default=1,
                    help="runs at a time; keep at 1 on a production stack so the test doesn't add noticeable query load")
    ap.add_argument("--pause", type=int, default=15, help="seconds to wait after each run, to spread query load")
    ap.add_argument("--budget-usd", type=float, default=3.0, help="hard cap per run")
    ap.add_argument("--timeout", type=int, default=1800, help="seconds per run")
    ap.add_argument("--only", help="comma-separated question ids")
    ap.add_argument("--all", action="store_true", help="also run the optional extra questions")
    ap.add_argument("--configs", default=",".join(CONFIGS))
    ap.add_argument("--force", action="store_true", help="rerun runs that already have results")
    ap.add_argument("--dry-run", action="store_true", help="print the prompts and exit")
    ap.add_argument("--gcx", help="path to the gcx binary to test (default: the one on PATH)")
    ap.add_argument("--graph-prompt", choices=tuple(GRAPH_PROMPTS), default="first",
                    help="first (default): tell the agent the graph exists and to start with it, so the test measures "
                         "what the graph adds when used; available: say it exists and leave the choice to the agent; "
                         "none: say nothing, to test whether gcx's own help leads the agent to the graph")
    args = ap.parse_args()
    SOURCES["with-graph"] = GRAPH_PROMPTS[args.graph_prompt]

    spec = json.load(open(args.questions))
    real_gcx = (os.path.abspath(args.gcx) if args.gcx else
                shutil.which("gcx", path=os.environ.get("PATH", "").replace(WRAPPER_DIR + os.pathsep, "")))
    if not real_gcx:
        sys.exit("gcx not found on PATH")
    # gcx installs bundled skill files without the execute bit; the wrapper needs it
    wrapper = os.path.join(WRAPPER_DIR, "gcx")
    if not os.access(wrapper, os.X_OK):
        os.chmod(wrapper, os.stat(wrapper).st_mode | 0o755)
    if args.only:
        questions = [q for q in spec["questions"] if q["id"] in args.only.split(",")]
    else:  # optional extras run only with --all
        questions = [q for q in spec["questions"] if args.all or not q.get("optional")]
    configs = args.configs.split(",")

    # interleave: every repetition covers every question, and the order of the
    # two configurations alternates, so neither always runs first
    jobs = []
    for rep in range(1, max(q.get("reps", args.reps) for q in questions) + 1):
        for i, q in enumerate(questions):
            if rep > q.get("reps", args.reps):  # e.g. the incident question asks for 5 runs
                continue
            order = configs if (rep + i) % 2 else list(reversed(configs))
            jobs += [{"q": q, "config": c, "rep": rep} for c in order]

    if args.dry_run:
        for q in questions:
            for c in configs:
                print(f"===== {q['id']} / {c}\n{build_prompt(spec, q, c)}\n")
        print(f"{len(jobs)} runs planned")
        return

    os.makedirs(os.path.join(args.out, "runs"), exist_ok=True)
    global COMMANDS_FILE
    COMMANDS_FILE = write_command_list(real_gcx, args.out)
    with open(os.path.join(args.out, "plan.json"), "w") as fh:
        json.dump({"questions_file": os.path.abspath(args.questions), "reps": args.reps, "model": args.model,
                   "effort": args.effort, "configs": configs, "gcx": real_gcx, "graph_prompt": args.graph_prompt,
                   "parallel": args.parallel, "pause_seconds": args.pause,
                   "gcx_version": subprocess.run([real_gcx, "--version"], capture_output=True, text=True).stdout.strip(),
                   "claude_version": subprocess.run(["claude", "--version"], capture_output=True, text=True).stdout.strip(),
                   "started_utc": datetime.now(timezone.utc).isoformat()}, fh, indent=1)
    print(f"{len(jobs)} runs, {args.parallel} at a time", flush=True)
    with ThreadPoolExecutor(max_workers=args.parallel) as pool:
        for run_id, msg in pool.map(lambda j: run_one(j, spec, args, real_gcx), jobs):
            print(f"{datetime.now(timezone.utc):%H:%M:%S} {run_id}: {msg}", flush=True)


if __name__ == "__main__":
    main()
