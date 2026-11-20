#!/usr/bin/env python3
"""Knowledge Graph agent test skill: share your scorecard with Grafana.

usage:
  python3 kgkit/share.py results/scorecard.json --questions my-questions.json --dry-run
  python3 kgkit/share.py results/scorecard.json --questions my-questions.json \
      --agent "Claude Code, sonnet" --contact you@example.com --comments "..."

What it does:
  1. Reads the scorecard and checks it holds only the fields score.py writes
     (question types, counts and numbers). Anything else stops the send.
  2. If you pass --questions, checks that no service name, alias or other
     answer-key text from your questions file appears anywhere in what would
     be sent, including your comments.
  3. Prints exactly what would be sent, field by field.
  4. Asks you to type `yes`. Nothing is sent otherwise.

  --dry-run  print the payload and stop. Never sends.
  --yes      skip the prompt. For an agent that has already shown its user the
             exact payload (from --dry-run) and been told to send it.

The destination form and its field IDs live in kgkit/share_config.json, so the
form can be swapped without changing this script. Python 3.9 standard library only.
"""
import argparse, json, math, os, re, sys, urllib.error, urllib.parse, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))

# Exactly the fields score.py writes. Anything else in the file stops the send.
TOP_LEVEL = {
    "kit_version": "str", "agent": "str", "claude_version": "str", "model": "str",
    "effort": "str", "gcx_version": "str", "graph_health": "str",
    "runs_recorded": "num", "runs_discarded_for_leakage": "num",
    "questions": "list", "questions_won_on_cost_and_time": "num",
    "questions_compared": "num", "comments": "str",
}
CONFIGS = ("without-graph", "with-graph")
PER_CONFIG = {"runs", "correct", "partly", "wrong", "no_answer",
              "median_cost_usd", "median_seconds", "median_gcx_calls"}
KNOWN_TYPES = {"callers", "dependencies", "blast-radius", "finding-the-right-system", "control"}
HEALTH = {"green", "amber", "red", "not-checked"}
# Version and model strings are short and made of these characters only.
SAFE_META = re.compile(r"^[A-Za-z0-9 ._:()/+\-]{0,200}$")
EMAIL = re.compile(r"^[^@\s]+@[^@\s]+\.[^@\s]+$")
MAX_FREE_TEXT = 2000


class Refuse(Exception):
    pass


def is_num(x):
    return x is None or (isinstance(x, (int, float)) and not isinstance(x, bool) and math.isfinite(x))


def check_scorecard(sc):
    """Return a cleaned copy of the scorecard, or raise Refuse naming the problem."""
    if not isinstance(sc, dict):
        raise Refuse("the scorecard is not a JSON object")
    extra = set(sc) - set(TOP_LEVEL)
    if extra:
        raise Refuse(f"the scorecard has fields score.py doesn't write: {', '.join(sorted(extra))}. "
                     "Remove them, or re-run score.py.")
    out = {}
    for k, kind in TOP_LEVEL.items():
        if k not in sc:
            continue
        v = sc[k]
        if kind == "num" and not is_num(v):
            raise Refuse(f"`{k}` should be a number")
        if kind == "str" and v is not None and not isinstance(v, str):
            raise Refuse(f"`{k}` should be text")
        if kind == "str" and k != "comments" and v is not None and not SAFE_META.match(v):
            raise Refuse(f"`{k}` doesn't look like a version or model string: {v[:80]!r}")
        out[k] = v
    if out.get("graph_health") not in HEALTH | {None}:
        raise Refuse(f"`graph_health` should be one of {', '.join(sorted(HEALTH))}")
    qs = sc.get("questions", [])
    if not isinstance(qs, list):
        raise Refuse("`questions` should be a list")
    out["questions"] = []
    for i, q in enumerate(qs, 1):
        if not isinstance(q, dict):
            raise Refuse(f"question {i} is not an object")
        extra = set(q) - {"type", *CONFIGS}
        if extra:
            raise Refuse(f"question {i} has fields score.py doesn't write: {', '.join(sorted(extra))}")
        t = q.get("type")
        # Question types come from the questions file, so a hand-written type could
        # carry a service name. Only the skill's own types are sent; others become "other".
        clean = {"type": t if t in KNOWN_TYPES else "other"}
        for c in CONFIGS:
            d = q.get(c, {})
            if not isinstance(d, dict):
                raise Refuse(f"question {i}, `{c}` is not an object")
            extra = set(d) - PER_CONFIG
            if extra:
                raise Refuse(f"question {i}, `{c}` has fields score.py doesn't write: {', '.join(sorted(extra))}")
            for k, v in d.items():
                if not is_num(v):
                    raise Refuse(f"question {i}, `{c}.{k}` should be a number")
            clean[c] = d
        out["questions"].append(clean)
    return out


def answer_key_terms(path):
    """Every service name, alias and question word worth guarding from the questions file."""
    spec = json.load(open(path))
    terms = set()

    def add(s):
        if isinstance(s, str):
            s = s.strip()
            if len(s) >= 3:
                terms.add(s.lower())

    for k in ("stack", "stack_notes"):
        v = spec.get(k)
        for s in (v if isinstance(v, list) else [v]):
            add(s)
    for q in spec.get("questions", []):
        for key in ("must_include", "must_not_include"):
            for group in q.get(key, []):
                for alias in (group if isinstance(group, list) else [group]):
                    add(alias)
        add(q.get("type"))
        # backticked or hyphenated names in the question text itself, e.g. `checkout-api`
        for s in re.findall(r"`([^`]+)`", q.get("question", "")):
            add(s)
        for s in re.findall(r"\b[a-z0-9]+(?:[-_.][a-z0-9]+)+\b", q.get("question", "").lower()):
            add(s)
    # The skill's own type names and config names are fine to send.
    return terms - KNOWN_TYPES - set(CONFIGS) - {"other"}


def find_leaks(fields, terms):
    hits = []
    for name, value in fields.items():
        low = value.lower()
        for t in terms:
            if re.search(r"(?<![a-z0-9_-])" + re.escape(t) + r"(?![a-z0-9_-])", low):
                hits.append((name, t))
    return hits


def load_config(path):
    cfg = json.load(open(path))
    url = cfg.get("form_url", "")
    entry = cfg.get("entry", {})
    missing = [k for k in ("scorecard", "agent", "contact", "comments") if not entry.get(k)]
    if missing:
        raise Refuse(f"{path} has no entry ID for: {', '.join(missing)}")
    return cfg, url, entry


def configured(url, entry):
    return ("REPLACE" not in url and url.endswith("/formResponse")
            and all(re.match(r"^entry\.\d+$", v) for v in entry.values()))


def main():
    ap = argparse.ArgumentParser(description="Share a Knowledge Graph agent test skill scorecard with Grafana.")
    ap.add_argument("scorecard", nargs="?", default="results/scorecard.json")
    ap.add_argument("--questions", help="your questions file, used to check no service name is sent (recommended)")
    ap.add_argument("--agent", default="", help="agent and model, e.g. 'Claude Code 2.1, sonnet'")
    ap.add_argument("--contact", default="", help="email for follow-up questions (optional)")
    ap.add_argument("--comments", default="", help="anything surprising (optional; added to the scorecard's own comments)")
    ap.add_argument("--config", default=os.path.join(HERE, "share_config.json"))
    ap.add_argument("--yes", action="store_true", help="send without the prompt (only after the user has seen the payload)")
    ap.add_argument("--dry-run", action="store_true", help="print the payload and stop; never sends")
    args = ap.parse_args()

    try:
        cfg, url, entry = load_config(args.config)
        scorecard = check_scorecard(json.load(open(args.scorecard)))

        comments = "\n".join(x for x in (scorecard.get("comments", "").strip(), args.comments.strip()) if x)
        scorecard["comments"] = ""  # sent once, in its own field
        agent = args.agent.strip() or " ".join(x for x in (scorecard.get("agent"), scorecard.get("model")) if x)
        contact = args.contact.strip()
        if contact and not EMAIL.match(contact):
            raise Refuse(f"--contact doesn't look like an email address: {contact!r}")
        if len(comments) > MAX_FREE_TEXT or len(agent) > 200:
            raise Refuse(f"comments are limited to {MAX_FREE_TEXT} characters and agent to 200")

        fields = {"scorecard": json.dumps(scorecard, indent=1), "agent": agent,
                  "contact": contact, "comments": comments}

        if args.questions:
            hits = find_leaks(fields, answer_key_terms(args.questions))
            if hits:
                listing = "; ".join(f"{name} contains {t!r}" for name, t in hits[:10])
                raise Refuse(f"text from your questions file would be sent: {listing}. "
                             "Edit the scorecard's comments or your --comments and try again.")
    except Refuse as e:
        print(f"Not sending: {e}", file=sys.stderr)
        return 2
    except (OSError, ValueError) as e:
        print(f"Not sending: couldn't read a file: {e}", file=sys.stderr)
        return 2

    retyped = [q for q in scorecard["questions"] if q["type"] == "other"]
    print("This is exactly what would be sent to Grafana:\n")
    for name, value in fields.items():
        print(f"--- {name} ---")
        print(value if value else "(empty)")
        print()
    print(f"Destination: {url}")
    if retyped:
        print(f"Note: {len(retyped)} question type(s) weren't one of the skill's own and are sent as \"other\".")
    if args.questions:
        print("Checked: no service name, alias or question text from your questions file appears above.")
    else:
        print("Warning: no --questions file given, so only the scorecard's structure was checked. "
              "Read your comments for service names before sending.")
    print()

    if args.dry_run:
        print("Dry run: nothing sent.")
        return 0
    if not configured(url, entry):
        print(f"Not sending: {args.config} still has placeholder form or entry IDs. "
              f"{cfg.get('fallback', '')}", file=sys.stderr)
        return 2
    if not args.yes:
        if not sys.stdin.isatty():
            print("Not sending: no terminal to confirm in. Show the user this payload, then re-run with --yes "
                  "once they agree.", file=sys.stderr)
            return 2
        if input("Type yes to send: ").strip().lower() != "yes":
            print("Not sent.")
            return 1

    body = urllib.parse.urlencode({entry[k]: v for k, v in fields.items()}).encode()
    req = urllib.request.Request(url, data=body, method="POST",
                                 headers={"Content-Type": "application/x-www-form-urlencoded"})
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            final = resp.geturl()
            status = resp.status
    except urllib.error.HTTPError as e:
        print(f"Not sent: the form answered HTTP {e.code}. {cfg.get('fallback', '')}", file=sys.stderr)
        return 3
    except urllib.error.URLError as e:
        print(f"Not sent: couldn't reach the form ({e.reason}). {cfg.get('fallback', '')}", file=sys.stderr)
        return 3
    if "accounts.google.com" in final or "ServiceLogin" in final:
        print("Not sent: the form asks for a Google sign-in, so it isn't open to you. "
              f"{cfg.get('fallback', '')}", file=sys.stderr)
        return 3
    print(f"Sent (HTTP {status}). Thank you.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
