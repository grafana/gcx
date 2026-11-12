#!/usr/bin/env python3
"""Knowledge Graph agent test skill: remove the Knowledge Graph from gcx's help.

The gcx wrapper pipes the output of help and discovery commands (gcx --help,
gcx help-tree, gcx commands, gcx agent skills list, gcx providers list) through
this filter in the without-graph configuration, so the agent sees a gcx that
has no graph, rather than one whose graph commands it can see but not use.
Only help and discovery output passes through here, never query results.

Reads stdin, writes stdout. Handles text, a JSON document, or JSON lines.
Python 3.9+ and the standard library only.
"""
import json, re, sys

MENTION = re.compile(r"knowledge[ -]?graph|entity[ -]graph|\bkg\b|\basserts\b", re.I)
SENTENCE = re.compile(r"(?<=[.!?])\s+")


def scrub_text(s):
    """Drop the sentences of a string that mention the graph."""
    if not MENTION.search(s):
        return s
    lines = []
    for line in s.split("\n"):
        if not MENTION.search(line):
            lines.append(line)
            continue
        kept = [p for p in SENTENCE.split(line) if not MENTION.search(p)]
        if kept and len(kept) < len(SENTENCE.split(line)):
            lines.append(" ".join(kept))
    return "\n".join(lines)


def is_graph_entry(o):
    """A command, provider or skill entry that is the graph itself."""
    path = str(o.get("full_path") or o.get("path") or "")
    if path == "gcx kg" or path.startswith("gcx kg "):
        return True
    name = str(o.get("name") or o.get("use") or "")
    if name.split(" ")[0] in ("kg", "knowledge-graph"):
        return True
    label = " ".join(str(o.get(k) or "") for k in ("name", "description", "summary", "short"))
    return bool(name) and bool(MENTION.search(label))


def scrub_spill(path):
    """gcx writes large output to a file and prints a reference to it; the agent then
    reads the file directly, so filter the file in place too."""
    try:
        with open(path) as fh:
            body = json.load(fh)
        with open(path, "w") as fh:
            json.dump(scrub(body), fh)
    except (OSError, ValueError):
        pass


def scrub(o):
    if isinstance(o, dict):
        if isinstance(o.get("spilled_to"), str):
            scrub_spill(o["spilled_to"])
        if is_graph_entry(o):
            return None
        out = {}
        for k, v in o.items():
            c = scrub(v)
            if c is None and v is not None:
                continue
            out[k] = c
        return out
    if isinstance(o, list):
        return [c for c in (scrub(x) for x in o) if c is not None]
    if isinstance(o, str):
        return scrub_text(o)
    return o


def main():
    data = sys.stdin.read()
    try:
        sys.stdout.write(json.dumps(scrub(json.loads(data))) + "\n")
        return
    except ValueError:
        pass
    out = []
    for line in data.splitlines():
        try:
            obj = json.loads(line)
        except ValueError:
            if not MENTION.search(line):
                out.append(line)
            continue
        c = scrub(obj)
        if c is not None:
            out.append(json.dumps(c))
    sys.stdout.write("\n".join(out) + ("\n" if out else ""))


if __name__ == "__main__":
    main()
