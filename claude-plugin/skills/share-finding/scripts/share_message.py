#!/usr/bin/env python3
"""Preview or send one canonical status update; journal once and never retry."""

import argparse
import json
from pathlib import Path
import subprocess
import sys

from save_model import message_fields, nonempty, read_json, read_message_receipt, record, require


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("message", type=Path, help="JSON with context, container, body and optional see (pointer only)")
    for flag in ("context", "container"):
        parser.add_argument("--" + flag, required=True)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--preview", action="store_true")
    mode.add_argument("--approved-sha256", help="Hash of the preview the user approved; sends exactly that file")
    for flag in ("actor-id", "actor-name"):
        parser.add_argument("--" + flag)
    parser.add_argument("--journal", type=Path, help="Fresh JSONL file; existing journals block sending")
    args = parser.parse_args(argv)
    result = {"status": "stopped", "journal": str(args.journal) if args.journal else None}
    try:
        require(nonempty(args.context) and nonempty(args.container), "Context and Incident must be nonempty")
        message, digest = read_json(args.message)
        body, see = message_fields(message, args.context, args.container)
        if args.preview:
            print(json.dumps({"status": "ready", "message_sha256": digest,
                              "preview": body + ("\nSee: " + see if see else "")}))
            return 0
        require(digest == args.approved_sha256, "Message changed since approval; preview and confirm the new wording")
        require(nonempty(args.actor_id) and nonempty(args.actor_name) and args.journal is not None,
                "Sending requires actor-id, actor-name and journal")
        command = ["gcx", "irm", "incidents", "share", args.container, "--context", args.context,
                   "--body", body, "--drafted-by-agent", args.actor_id,
                   "--drafted-by-agent-name", args.actor_name, "--output", "json"]
        if see:
            command += ["--see", see]
        with args.journal.open("x", encoding="utf-8") as journal:
            record(journal, {"event": "start", "message": message, "message_sha256": digest})
            record(journal, {"event": "pending", "argv": command})
            try:
                process = subprocess.run(command, capture_output=True, text=True, timeout=60)
                record(journal, {"event": "response", "returncode": process.returncode,
                                 "stdout": process.stdout, "stderr": process.stderr})
                receipt = read_message_receipt(process.stdout, args.container)
                result["message_id"] = receipt["message_id"]
                record(journal, {"event": "saved", "receipt": receipt})
                require(process.returncode == 0, f"GCX exited {process.returncode}; inspect recorded receipt")
                require(not receipt.get("warnings"), "GCX returned warnings; inspect recorded receipt")
                result["status"] = "saved"
                record(journal, {"event": "complete", "receipt": receipt})
            except (OSError, ValueError, subprocess.TimeoutExpired, KeyboardInterrupt) as error:
                result.update(error=str(error) or "Interrupted",
                              recovery="Inspect the journal and Incident before recovery; do not resend automatically.")
                record(journal, {"event": "stopped", "summary": result})
    except (OSError, ValueError) as error:
        result.update(error=str(error), recovery="Inspect any existing journal before another send.")
    print(json.dumps(result))
    return 0 if result["status"] == "saved" else 1


if __name__ == "__main__":
    sys.exit(main())
