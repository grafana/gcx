#!/usr/bin/env python3
"""Check, patch and save a share-finding draft via GCX; never retry mutations."""

import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
from typing import NamedTuple

STRUCTURED_ERRORS_ENV = {"GCX_AGENT_MODE": "1"}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def nonempty(value):
    return isinstance(value, str) and bool(value.strip())


class DraftError(ValueError):
    def __init__(self, errors):
        self.errors = errors
        super().__init__("; ".join(f"{e['path']}: {e['message']}" for e in errors))


def validation_errors(draft, context, container):
    errors = []

    def check(ok, path, message):
        if not ok:
            errors.append({"path": path, "message": message})
        return ok

    if not check(isinstance(draft, dict), "/", "Draft must be an object"):
        return errors
    check(draft.get("context") == context, "/context", "Draft context does not match pinned context")
    check(draft.get("container") == container, "/container", "Draft container does not match pinned Incident")
    check(not (draft.keys() - {"context", "container", "entities", "evidence_links", "gaps"}),
          "/", "Unsupported draft fields")
    gaps = draft.get("gaps", [])
    check(isinstance(gaps, list) and all(nonempty(g) for g in gaps), "/gaps", "Expected an array of nonempty strings")
    entities, links = draft.get("entities"), draft.get("evidence_links")
    entities = entities if check(isinstance(entities, list), "/entities", "Expected an array") else []
    links = links if check(isinstance(links, list), "/evidence_links", "Expected an array") else []
    refs, existing = {}, set()
    for i, entity in enumerate(entities):
        path = f"/entities/{i}"
        if not check(isinstance(entity, dict), path, "Each entity must be an object"):
            continue
        ref, kind = entity.get("ref"), entity.get("type")
        if check(nonempty(ref), path + "/ref", "Entity ref must be nonempty"):
            if check(ref not in refs, path + "/ref", "Entity refs must be unique"):
                refs[ref] = entity
        check(kind in ("observation", "hypothesis", "impact", "action"), path + "/type", "Invalid entity type")
        allowed = {"ref", "type", "text", "existing_id"}
        allowed |= {"source_ref"} if kind == "observation" else set()
        allowed |= {"prompted_by"} if kind == "action" else set()
        for field in sorted(entity.keys() - allowed):
            check(False, path + "/" + field, f"{kind} {ref!r} does not allow {field}")
        check(nonempty(entity.get("text")), path + "/text", "Missing entity text")
        for field in ("existing_id", "source_ref", "prompted_by"):
            if field in entity:
                check(nonempty(entity[field]), path + "/" + field, "Expected a nonempty string; omit if absent")
        if nonempty(entity.get("existing_id")):
            check(entity["existing_id"] not in existing, path + "/existing_id", "Existing entities must have distinct refs")
            existing.add(entity["existing_id"])
    for i, entity in enumerate(entities):
        if isinstance(entity, dict) and nonempty(entity.get("prompted_by")):
            check(refs.get(entity["prompted_by"], {}).get("type") == "observation",
                  f"/entities/{i}/prompted_by", "Prompting ref must identify a retained observation")
    seen = set()
    for i, link in enumerate(links):
        path = f"/evidence_links/{i}"
        if not check(isinstance(link, dict), path, "Each evidence link must be an object"):
            continue
        check(not (link.keys() - {"source", "target", "stance", "rationale"}), path, "Unsupported evidence-link fields")
        source, target = link.get("source"), link.get("target")
        if check(nonempty(source), path + "/source", "Missing evidence source"):
            check(refs.get(source, {}).get("type") == "observation", path + "/source", "Evidence source must be an observation")
        if check(nonempty(target), path + "/target", "Missing evidence target"):
            check(refs.get(target, {}).get("type") in ("hypothesis", "impact"), path + "/target", "Evidence target must be a claim")
        check(link.get("stance") in ("supports", "challenges", "context"), path + "/stance", "Invalid evidence stance")
        check(isinstance(link.get("rationale", ""), str), path + "/rationale", "Expected a string")
        if nonempty(source) and nonempty(target):
            check((source, target) not in seen, path, "Duplicate evidence endpoints")
            seen.add((source, target))
    return errors


def validate(draft, context, container):
    errors = validation_errors(draft, context, container)
    if errors:
        raise DraftError(errors)


def read_json(path):
    raw = path.read_bytes()
    return json.loads(raw), hashlib.sha256(raw).hexdigest()


def apply_patch(draft, patch, digest):
    require(isinstance(patch, dict) and set(patch) == {"draft_sha256", "edits"}, "Expected draft_sha256 and edits")
    require(patch["draft_sha256"] == digest, "Patch targets a different draft; review the current file")
    require(isinstance(patch["edits"], list), "edits must be an array")
    result = copy.deepcopy(draft)
    for i, edit in enumerate(patch["edits"]):
        prefix = f"edits[{i}]"
        require(isinstance(edit, dict), f"{prefix}: expected an object")
        op, path = edit.get("op"), edit.get("path")
        require(op in ("add", "replace", "remove"), f"{prefix}: unsupported operation")
        require(set(edit) == ({"op", "path"} if op == "remove" else {"op", "path", "value"}), f"{prefix}: invalid fields")
        require(isinstance(path, str) and path.startswith("/"), f"{prefix}: expected JSON Pointer")
        tokens = path[1:].split("/")
        for token in tokens:
            require("~" not in token.replace("~0", "").replace("~1", ""), f"{prefix}: invalid pointer escape")
        tokens = [t.replace("~1", "/").replace("~0", "~") for t in tokens]
        require(tokens[0] in ("entities", "evidence_links", "gaps"), f"{prefix}: only model content may be edited")
        if tokens[0] in ("entities", "evidence_links"):
            is_entity = tokens[0] == "entities"
            width = 2 if is_entity else 3
            require(len(tokens) >= width, f"{prefix}: target an entity ref or source/target refs")
            items = result.get(tokens[0])
            require(isinstance(items, list), f"{prefix}: expected an array")
            identity = {"ref": tokens[1]} if is_entity else {"source": tokens[1], "target": tokens[2]}
            matches = [n for n, item in enumerate(items) if isinstance(item, dict)
                       and all(item.get(k) == v for k, v in identity.items())]
            require(len(matches) <= 1, f"{prefix}: ambiguous target {identity}")
            whole = len(tokens) == width
            if whole and op in ("add", "replace"):
                value = edit["value"]
                require(isinstance(value, dict) and all(value.get(k) == v for k, v in identity.items()),
                        f"{prefix}: value must preserve target identity {identity}")
            if whole and op == "add":
                require(not matches, f"{prefix}: target already exists {identity}")
                items.append(copy.deepcopy(edit["value"]))
                continue
            require(len(matches) == 1, f"{prefix}: target not found {identity}")
            require(whole or tokens[width] not in identity, f"{prefix}: identity fields cannot be edited")
            tokens = [tokens[0], str(matches[0]), *tokens[width:]]
        parent = result
        for token in tokens[:-1]:
            if isinstance(parent, list):
                require(token.isascii() and token.isdecimal() and str(int(token)) == token and int(token) < len(parent), f"{prefix}: invalid array index")
                parent = parent[int(token)]
            else:
                require(isinstance(parent, dict) and token in parent, f"{prefix}: path not found")
                parent = parent[token]
        key = tokens[-1]
        if isinstance(parent, list):
            if key == "-" and op == "add":
                index = len(parent)
            else:
                require(key.isascii() and key.isdecimal() and str(int(key)) == key, f"{prefix}: invalid array index")
                index = int(key)
            require(index < len(parent) or (op == "add" and index == len(parent)), f"{prefix}: array index out of range")
            if op == "add":
                parent.insert(index, copy.deepcopy(edit["value"]))
            elif op == "remove":
                parent.pop(index)
            else:
                parent[index] = copy.deepcopy(edit["value"])
        else:
            require(isinstance(parent, dict), f"{prefix}: path is not a collection")
            require(op == "add" or key in parent, f"{prefix}: path not found")
            if op == "remove":
                del parent[key]
            else:
                parent[key] = copy.deepcopy(edit["value"])
    return result


def message_fields(message, context, container):
    require(isinstance(message, dict), "Message must be an object")
    require(not (message.keys() - {"context", "container", "body", "see"}), "Unsupported message fields")
    require(message.get("context") == context and message.get("container") == container, "Message destination does not match pinned context and Incident")
    require(nonempty(message.get("body")), "Message body must be nonempty")
    if "see" in message:
        require(nonempty(message["see"]), "Omit see when absent")
        require(not message["see"].lstrip().lower().startswith("see:"), "see contains the pointer only; the preview adds See:")
    return message["body"], message.get("see")


def operations(draft, ids):
    for kind in ("observation", "hypothesis", "impact"):
        for entity in draft["entities"]:
            if entity["type"] != kind or "existing_id" in entity:
                continue
            args = ["create-observation"] if kind == "observation" else ["create-claim", "--type", kind]
            args += ["--text", entity["text"]]
            if entity.get("source_ref"):
                args += ["--source-ref", entity["source_ref"]]
            yield "entities", entity["ref"], args, "observation" if kind == "observation" else "claim"
    for index, link in enumerate(draft["evidence_links"]):
        args = ["attach-evidence-link", "--source", ids[link["source"]],
                "--target", ids[link["target"]], "--stance", link["stance"]]
        if link.get("rationale"):
            args += ["--rationale", link["rationale"]]
        yield "evidence_links", str(index), args, "evidence_link"
    for entity in draft["entities"]:
        if entity["type"] != "action" or "existing_id" in entity:
            continue
        if entity.get("prompted_by"):
            args = ["promote-observation-to-action", ids[entity["prompted_by"]]]
            kind = "observation"
        else:
            args, kind = ["create-action"], "action"
        yield "entities", entity["ref"], args + ["--text", entity["text"]], kind


def read_document(stdout):
    decoder, rest, receipts = json.JSONDecoder(), stdout.strip(), []
    while rest:
        obj, end = decoder.raw_decode(rest)
        require(isinstance(obj, dict), "Unexpected GCX output")
        if obj.get("class") != "hint":
            receipts.append(obj)
        rest = rest[end:].lstrip()
    require(len(receipts) == 1, "Expected exactly one receipt")
    return receipts[0]


MODEL_FIELDS = {
    "observations": {"id": str, "text": str, "content_version": int},
    "claims": {"id": str, "type": str, "text": str, "content_version": int},
    "actions": {"id": str, "text": str, "content_version": int},
    "evidence_links": {"id": str, "source_id": str, "target_id": str, "stance": str,
                       "source_version": int, "target_version": int},
}


def read_model(context, container):
    process = subprocess.run(["gcx", "irm", "opmodel", "containers", "get", container, "--context", context,
                              "--output", "json"], capture_output=True, text=True, timeout=60,
                             env={**os.environ, **STRUCTURED_ERRORS_ENV})
    if process.returncode != 0:
        try:
            error = read_document(process.stdout)
        except ValueError:
            error = {}
        detail = error.get("error") if error.get("type") == "gcx.error" else None
        require(process.returncode == 1 and isinstance(detail, dict)
                and detail.get("summary") == "op model entity or container not found"
                and detail.get("details") == f"container {container} not found",
                f"Model read failed (GCX exited {process.returncode}): {(process.stdout + process.stderr).strip()[:300]}")
        return {"revision": None, **{group: [] for group in MODEL_FIELDS}}
    model = read_document(process.stdout)
    require(model.get("type") == "gcx.irm.opmodel.container" and model.get("schema_version") == "2",
            "Unexpected model schema")
    require(model.get("container") == container and nonempty(model.get("revision")), "Model container or revision mismatch")
    for group, fields in MODEL_FIELDS.items():
        require(isinstance(model.get(group), list) and all(
            isinstance(item, dict) and all(isinstance(item.get(f), t) for f, t in fields.items())
            for item in model[group]), f"Unexpected model {group}")
    return model


def entity_key(kind, text, extra):
    return kind, " ".join(text.split()).removesuffix("."), extra


class ModelIndex(NamedTuple):
    kinds: dict
    keys: dict
    links: dict
    withdrawn: dict


def model_index(model):
    kinds, keys, links, withdrawn = {}, {}, {}, {}
    for group, identity in (("observations", lambda e: ("observation", e.get("source_ref") or "")),
                            ("claims", lambda e: (e["type"], None)),
                            ("actions", lambda e: ("action", (e.get("origin") or {}).get("observation_id")))):
        for item in model[group]:
            kind, extra = identity(item)
            kinds[item["id"]] = kind, item["content_version"]
            keys.setdefault(entity_key(kind, item["text"], extra), item["id"])
    for link in model["evidence_links"]:
        triple = link["source_id"], link["target_id"], link["stance"]
        current = all(kinds.get(link[end + "_id"], (None, None))[1] == link[end + "_version"]
                      for end in ("source", "target"))
        if link.get("withdrawal") is not None:
            withdrawn.setdefault(triple, link["id"])
        elif current:
            links.setdefault(triple, link["id"])
    return ModelIndex(kinds, keys, links, withdrawn)


def reconcile(draft, model):
    index = model_index(model)
    result = copy.deepcopy(draft)
    ids, reused, seen = {}, {}, {}
    prompting_entities = [e for e in result["entities"] if e["type"] != "action"]
    actions = [e for e in result["entities"] if e["type"] == "action"]
    for entity in prompting_entities + actions:
        ref, kind, prompt = entity["ref"], entity["type"], entity.get("prompted_by")
        extra = (entity.get("source_ref") or "") if kind == "observation" else ids.get(prompt, prompt)
        key = entity_key(kind, entity["text"], extra)
        require(key not in seen, f"{seen.get(key)} and {ref} repeat each other; merge them with a patch")
        seen[key] = ref
        if "existing_id" in entity:
            require(index.kinds.get(entity["existing_id"], (None,))[0] == kind,
                    f"{ref}: no {kind} {entity['existing_id']} in the current model")
        elif (prompt is None or prompt in ids) and key in index.keys:
            entity["existing_id"] = reused[ref] = index.keys[key]
        if "existing_id" in entity:
            ids[ref] = entity["existing_id"]
    kept, skipped = [], []
    for link in result["evidence_links"]:
        triple = ids.get(link["source"]), ids.get(link["target"]), link["stance"]
        existing = index.links.get(triple)
        require(existing or triple not in index.withdrawn, f"{link['source']}->{link['target']} repeats withdrawn "
                f"evidence link {index.withdrawn.get(triple)}; remove it with a patch")
        if existing:
            skipped.append({"source": link["source"], "target": link["target"],
                            "stance": link["stance"], "existing_id": existing})
        else:
            kept.append(link)
    result["evidence_links"] = kept
    return result, {"model_revision": model["revision"], "reused": reused, "skipped_links": skipped}


def read_message_receipt(stdout, container):
    receipt = read_document(stdout)
    require(receipt.get("type") == "gcx.irm.message_receipt"
            and receipt.get("schema_version") == "1", "Unexpected message receipt schema")
    require(receipt.get("incident_id") == container and nonempty(receipt.get("message_id")),
            "Missing message ID or wrong Incident in receipt")
    return receipt


def saved_message(path, context, container):
    events = [json.loads(line) for line in path.read_text().splitlines()]
    require(len(events) >= 2 and all(isinstance(e, dict) for e in events), "Invalid message journal")
    require(events[0].get("event") == "start" and events[-1].get("event") == "complete",
            "Message delivery is unconfirmed; do not treat it as a skipped update")
    receipt = read_message_receipt(json.dumps(events[-1].get("receipt")), container)
    return message_fields(events[0].get("message"), context, container)


def read_receipt(stdout, args, kind, container):
    receipt = read_document(stdout)
    require(receipt.get("type") == "gcx.irm.opmodel.mutation"
            and receipt.get("schema_version") == "2", "Unexpected mutation receipt schema")
    require(receipt.get("container") == container and receipt.get("action") == args[0],
            "Mutation receipt destination or action mismatch")
    target = receipt.get("target")
    require(isinstance(target, dict) and target.get("kind") == kind
            and nonempty(target.get("id")), "Missing or unexpected receipt target")
    require(nonempty(receipt.get("revision")), "Missing receipt revision")
    minted_id = target["id"]
    if args[0] == "promote-observation-to-action":
        require(target["id"] == args[1], "Promotion source mismatch")
        result = receipt.get("result")
        require(isinstance(result, dict) and nonempty(result.get("action_id")),
                "Promotion receipt missing result.action_id")
        minted_id = result["action_id"]
        require(minted_id != target["id"], "Promotion action ID equals source observation ID")
    return receipt, minted_id


def record(journal, event):
    journal.write(json.dumps(event) + "\n")
    journal.flush()
    os.fsync(journal.fileno())


def save(draft, args, journal, report):
    ids = {e["ref"]: e["existing_id"] for e in draft["entities"] if "existing_id" in e}
    summary = {"status": "saving", "context": args.context, "container": args.container, **report,
               "entities": ids, "evidence_links": {}, "revision": None,
               "journal": str(args.journal)}
    base = ["--context", args.context,
            "--actor-kind", "agent", "--actor-id", args.actor_id,
            "--actor-name", args.actor_name, "--output", "json"]
    record(journal, {"event": "start", "context": args.context, "container": args.container,
                     "actor_id": args.actor_id, **report, "existing_ids": dict(ids), "draft": draft,
                     "message_journal": str(args.message) if args.message else None,
                     "message_skipped": args.skip_message})
    for group, ref, command, kind in operations(draft, ids):
        argv = ["gcx", "irm", "opmodel", "containers", command[0], args.container] + command[1:] + base
        record(journal, {"event": "pending", "group": group, "ref": ref, "argv": argv})
        try:
            process = subprocess.run(argv, capture_output=True, text=True, timeout=60)
            record(journal, {"event": "response", "group": group, "ref": ref,
                             "returncode": process.returncode,
                             "stdout": process.stdout, "stderr": process.stderr})
            receipt, minted_id = read_receipt(process.stdout, command, kind, args.container)
            summary[group][ref] = minted_id
            summary["revision"] = receipt["revision"]
            record(journal, {"event": "saved", "group": group, "ref": ref,
                             "id": minted_id, "receipt": receipt})
            require(process.returncode == 0, f"GCX exited {process.returncode}; inspect recorded receipt")
        except (OSError, ValueError, subprocess.TimeoutExpired, KeyboardInterrupt) as error:
            summary.update(status="stopped", stopped_at={"group": group, "ref": ref},
                           error=str(error) or "Interrupted",
                           recovery="Inspect the journal and model before recovery; do not replay this draft.")
            record(journal, {"event": "stopped", "summary": summary})
            return summary
    summary["status"] = "saved"
    record(journal, {"event": "complete", "summary": summary})
    return summary


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("draft", type=Path)
    for flag in ("context", "container"):
        parser.add_argument("--" + flag, required=True)
    parser.add_argument("--validate-only", action="store_true", help="Report all structural errors and the draft hash; never publish")
    parser.add_argument("--patch", type=Path, help="Hash-bound edits by entity ref or evidence source/target; original draft stays unchanged")
    parser.add_argument("--draft-sha256", help="Hash of the worker artifact being saved")
    message = parser.add_mutually_exclusive_group()
    message.add_argument("--message", type=Path, help="Completed share_message.py journal; confirms delivery and pinned destination")
    message.add_argument("--skip-message", action="store_true", help="User explicitly skipped the update")
    for flag in ("actor-id", "actor-name"):
        parser.add_argument("--" + flag)
    parser.add_argument("--journal", type=Path, help="New JSONL file; never overwritten or replayed")
    args = parser.parse_args(argv)
    digest, report, opened = None, {}, False
    try:
        require(nonempty(args.context) and nonempty(args.container), "Context and Incident must be nonempty")
        draft, digest = read_json(args.draft)
        if args.draft_sha256 is not None:
            require(digest == args.draft_sha256, "Draft changed since worker completion; inspect the current file")
        if args.patch:
            draft = apply_patch(draft, read_json(args.patch)[0], digest)
        if args.message:
            saved_message(args.message, args.context, args.container)
        validate(draft, args.context, args.container)
        if args.validate_only:
            print(json.dumps({"status": "valid", "draft_sha256": digest,
                              "draft_path": str(args.draft.resolve()), "gaps": draft.get("gaps", []), "errors": [],
                              "entities": len(draft["entities"]), "evidence_links": len(draft["evidence_links"])}))
            return 0
        require(nonempty(args.draft_sha256), "Saving requires the worker draft-sha256")
        require(args.message is not None or args.skip_message,
                "Saving requires a completed --message journal or an explicit --skip-message choice")
        require(nonempty(args.actor_id) and nonempty(args.actor_name) and args.journal is not None,
                "Saving requires actor-id, actor-name and journal")
        draft, report = reconcile(draft, read_model(args.context, args.container))
        validate(draft, args.context, args.container)
        with args.journal.open("x", encoding="utf-8") as journal:
            opened = True
            result = save(draft, args, journal, report)
        print(json.dumps(result))
        return 0 if result["status"] == "saved" else 1
    except (OSError, ValueError, subprocess.TimeoutExpired) as error:
        print(json.dumps({"status": "invalid" if args.validate_only else "stopped",
                          "error": str(error) if opened else f"{error}; no writes were made",
                          "errors": getattr(error, "errors", []), "draft_sha256": digest, **report,
                          "journal": str(args.journal) if opened else None,
                          "recovery": "Inspect any existing journal before recovery; do not replay writes." if opened
                          else "No writes were made; fix the cause and rerun."}))
        return 1


if __name__ == "__main__":
    sys.exit(main())
