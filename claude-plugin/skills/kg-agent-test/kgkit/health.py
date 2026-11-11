#!/usr/bin/env python3
"""Knowledge Graph agent test skill: check the graph is healthy enough to test.

usage:
  python3 kgkit/health.py questions.json --out results/

Runs gcx's own checks for the whole stack and for every service the questions
name, then confirms each service is in the graph, in the right environment,
with relationships. Writes results/health.json and prints a rating:

  green  everything passed: go ahead
  amber  warnings: go ahead, but note them on your scorecard
  red    a question's service is missing from the graph or has no
         relationships, or the stack check failed: fix that first, or pick
         another question (a test against a broken map measures the break)

Optional: add "graph_groups" to the questions file to check connectivity per
business unit, platform or team, e.g.
  "graph_groups": {"property": "<label>", "env": "production", "values": ["<a>", "<b>"]}
Two queries per value, so keep the list short on a production stack.
"""
import argparse, json, os, subprocess, sys


GCX = "gcx"


def gcx(args, context):
    cmd = [GCX] + (["--context", context] if context else []) + args
    p = subprocess.run(cmd, capture_output=True, text=True, timeout=600)
    return p.returncode, p.stdout + "\n" + p.stderr


def first_json(text, want=None):
    """gcx prints hint objects before the body; return the first object with the wanted key."""
    dec, pos = json.JSONDecoder(), 0
    while True:
        pos = text.find("{", pos)
        if pos < 0:
            return None
        try:
            obj, _ = dec.raw_decode(text[pos:])
            if isinstance(obj, dict) and (want is None or want in obj or "spilled_to" in obj):
                if "spilled_to" in obj:
                    obj = json.load(open(obj["spilled_to"]))
                return obj
        except ValueError:
            pass
        pos += 1


def gcx_version_note():
    """Compare the installed gcx with the latest release. Returns (rating, note) or (None, None) when current.

    The with-graph configuration relies on gcx's own help to lead the agent to the graph, so an old
    gcx weakens the test without any error."""
    import re, urllib.request
    code, out = gcx(["--version"], None)
    m = re.search(r"version v?(\d+)\.(\d+)\.(\d+)", out)
    if not m:
        return "amber", "Couldn't read the gcx version."
    mine = tuple(int(x) for x in m.groups())
    if mine == (0, 0, 0):
        return None, "gcx is a development build; skipped the version check."
    try:
        req = urllib.request.Request("https://api.github.com/repos/grafana/gcx/releases/latest",
                                     headers={"Accept": "application/vnd.github+json", "User-Agent": "kgkit"})
        tag = json.load(urllib.request.urlopen(req, timeout=10))["tag_name"]
    except Exception:
        return None, "Couldn't reach GitHub to check for a newer gcx; check `gcx --version` against github.com/grafana/gcx/releases."
    latest = tuple(int(x) for x in re.findall(r"\d+", tag)[:3])
    if mine < latest:
        return "amber", (f"gcx {'.'.join(map(str, mine))} is older than the latest release ({tag}). Upgrade before testing "
                         "(`brew upgrade gcx`, or download it from github.com/grafana/gcx/releases): newer gcx help "
                         "is what tells an agent when to use the graph.")
    return None, None


def graph_neighbours(name, env, window, context):
    """Count the service's relationships in the graph directly (gcx v1 'graph query', older 'entities query')."""
    q = f'MATCH (s:Service {{name:"{name}", env:"{env}"}})-[r]-(d) RETURN s, d LIMIT 500'
    # gcx 1.x 'graph query' returns {"columns", "rows"}; older 'entities query' returns {"entities", "edges"}
    for sub, key in ((["kg", "graph", "query"], "rows"), (["kg", "entities", "query"], "entities")):
        code, out = gcx(sub + [q, "--from", window["from"], "--to", window["to"], "-o", "json"], context)
        body = first_json(out, key)
        if code == 0 and body is not None:
            n = len(body.get("rows", [])) if key == "rows" else len(body.get("edges", []))
            return n, " ".join(sub)
    return None, None


def names_in(body):
    """Collect entity names from a graph query body, whichever shape gcx returns."""
    found = set()

    def walk(x):
        if isinstance(x, dict):
            if isinstance(x.get("name"), str):
                found.add(x["name"])
            for v in x.values():
                walk(v)
        elif isinstance(x, list):
            for v in x:
                walk(v)
    walk(body.get("rows") if "rows" in body else body.get("entities", body))
    return found


def group_connectivity(groups, window, context):
    """For each value of a grouping property (a business unit, platform or team label), count the
    services the graph holds and how many of them have any relationship. One group can be well
    connected while another has none, and questions drawn from the second test the gap, not the graph."""
    prop, env = groups["property"], groups.get("env")
    env_filter = f', env:"{env}"' if env else ""
    out = []
    for value in groups["values"]:
        match = f'(s:Service {{{prop}:"{value}"{env_filter}}})'
        counts = []
        for q in (f"MATCH {match} RETURN s LIMIT 2000", f"MATCH {match}-[r]-(d) RETURN s LIMIT 2000"):
            n = None
            for sub, key in ((["kg", "graph", "query"], "rows"), (["kg", "entities", "query"], "entities")):
                code, text = gcx(sub + [q, "--from", window["from"], "--to", window["to"], "-o", "json"], context)
                body = first_json(text, key)
                if code == 0 and body is not None:
                    n = len(names_in(body))
                    break
            counts.append(n)
        out.append({"property": prop, "value": value, "env": env, "services": counts[0], "connected": counts[1]})
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("questions")
    ap.add_argument("--out", required=True)
    ap.add_argument("--gcx", help="path to the gcx binary to use (default: the one on PATH)")
    args = ap.parse_args()
    global GCX
    if args.gcx:
        GCX = os.path.abspath(args.gcx)
    spec = json.load(open(args.questions))
    ctx, window = spec.get("gcx_context"), spec["window"]
    notes, rating = [], "green"

    def worse(r):
        nonlocal rating
        order = ["green", "amber", "red"]
        rating = max(rating, r, key=order.index)

    vrating, vnote = gcx_version_note()
    if vrating:
        worse(vrating)
    if vnote:
        notes.append(vnote)

    code, out = gcx(["kg", "status", "-o", "json"], ctx)
    status = first_json(out, "enabled") or {}
    if not status.get("enabled") or status.get("status") != "complete":
        worse("red")
        notes.append(f"Stack status: {status or out[:200]}")

    code, out = gcx(["kg", "diagnose", "-o", "json"], ctx)
    stack = first_json(out, "checks") or {"checks": [], "summary": {}}
    for c in stack["checks"]:
        if c["status"] == "fail":
            worse("red")
            notes.append(f"Stack check failed: {c['name']}: {c['detail'][:160]}")
        elif c["status"] == "warn":
            worse("amber")
            notes.append(f"Stack check warning: {c['name']}: {c['detail'][:160]}")

    services, seen = [], set()
    for q in spec["questions"]:
        for s in q.get("services", []):
            key = (s["name"], s.get("env"))
            if key not in seen:
                seen.add(key)
                services.append(s)
    per_service = []
    for s in services:
        flags = ["diagnose", "service", s["name"], "-o", "json"] + (["--env", s["env"]] if s.get("env") else [])
        code, out = gcx(["kg"] + flags, ctx)
        d = first_json(out, "checks") or {}
        resolved_env = (d.get("entity") or {}).get("env")
        row = {"service": s["name"], "env": s.get("env"), "diagnose_summary": d.get("summary"),
               "diagnose_resolved_env": resolved_env, "warnings": [], "failures": []}
        for c in d.get("checks", []):
            if c["status"] in ("warn", "fail"):
                row["warnings" if c["status"] == "warn" else "failures"].append(f"{c['name']}: {c['detail'][:160]}")
        if s.get("env") and resolved_env and resolved_env != s["env"]:
            row["warnings"].append(f"diagnose looked at the {resolved_env} instance, not {s['env']}; checked the graph directly instead")
        edges, via = graph_neighbours(s["name"], s.get("env") or "", window, ctx) if s.get("env") else (None, None)
        row["graph_edges_in_env"], row["checked_with"] = edges, via
        if edges == 0 or (edges is None and not d.get("entity")):
            row["failures"].append("service not found in the graph for this environment, or it has no relationships")
        if row["failures"]:
            worse("red")
        elif row["warnings"]:
            worse("amber")
        per_service.append(row)

    summary_bits = [f"Stack: {stack.get('summary', {}).get('passed', '?')} of {stack.get('summary', {}).get('total', '?')} checks passed."]
    for r in per_service:
        summary_bits.append(f"{r['service']} ({r['env']}): {r['graph_edges_in_env']} relationships in the graph"
                            + (f", {len(r['warnings'])} warning(s)" if r["warnings"] else "")
                            + (f", {len(r['failures'])} failure(s)" if r["failures"] else "") + ".")
    groups = []
    if spec.get("graph_groups"):
        groups = group_connectivity(spec["graph_groups"], window, ctx)
        for g in groups:
            if g["services"] is None:
                worse("amber")
                notes.append(f"Couldn't count services for {g['property']}={g['value']}.")
            elif g["services"] and not g["connected"]:
                worse("amber")
                notes.append(f"{g['property']}={g['value']}: {g['services']} services in the graph, none with relationships. "
                             "Questions from this group test the gap, not the graph; label them as such.")
    result = {"rating": rating, "summary": " ".join(summary_bits), "notes": notes,
              "stack_checks": stack.get("checks"), "services": per_service, "groups": groups}
    os.makedirs(args.out, exist_ok=True)
    with open(os.path.join(args.out, "health.json"), "w") as fh:
        json.dump(result, fh, indent=1)
    print(f"Graph health: {rating.upper()}")
    print(result["summary"])
    for n in notes:
        print(" -", n)
    for r in per_service:
        for w in r["warnings"] + r["failures"]:
            print(f" - {r['service']} ({r['env']}): {w}")
    for g in groups:
        print(f" - {g['property']}={g['value']}: {g['connected']} of {g['services']} services have relationships")
    return 0 if rating != "red" else 1


if __name__ == "__main__":
    sys.exit(main())
