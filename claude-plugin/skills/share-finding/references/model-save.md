# Reconcile and save the supporting model

Use the worker's validated file and hash. Python 3.10+ is required.

## Review the full draft

Read the full artifact, even when the message is unchanged and the worker reports
no gaps. Review every entity and evidence link against the investigation, final
message, and user scope. If the update was skipped, use the requested finding
and scope instead of a sent message.

Check factual grounding, uncertainty, evidence meaning, relevant counterevidence,
and established next steps. Reconcile user changes and worker gaps. Check the
draft's fidelity to the supplied investigation; do not repeat the investigation
or fetch new evidence. Preserve unresolved uncertainty rather than inventing
evidence. Apply content corrections as small edits using the procedure below.

The save helper validates the final graph, including edits. No separate
main-agent validation call is needed.

## Check for existing findings

Read the current model after the message is sent or explicitly skipped. The
session may never have loaded it; sharing can come first. If this session holds
a complete model from an earlier `load-op-context` pull or share for the
same context, stack and Incident, refresh it from its consumed revision with
that skill's `list-events` procedure. Otherwise, or if that procedure is not
available, read one full snapshot:

```bash
gcx --agent --context "$name" irm opmodel containers get <incident-id>
```

Read the complete response, following a `gcx.spill_reference` to its file. A
container-not-found error means nothing has been saved for this Incident yet;
Step 0 already resolved the Incident, so treat the model as empty. Any other
failed or incomplete read stops model writes; it is not evidence that the model
is empty. Keep what you read, with its revision, as this session's baseline for
later shares and catch-ups.

Compare each draft entity with the current model by meaning and type, including
scope and time. For observations, compare the fact, not only the source. An
existing observation that reports the same fact, such as a responder's Slack
report that the investigation went on to confirm, is a match: add a ref with its
`existing_id` and link it to the claims it bears on. Keep the draft's own
observation as well only when it adds a measurement or source the existing one
lacks. Repeated measurements at different times are distinct. For actions,
compare the intended work and prompting observation, not text alone. For claims,
reuse only an existing claim that states the same cause and effect, or the same
harm and scope. A draft claim that adds a cause, mechanism, condition or scope
to an existing claim is new, even when it shares wording or evidence; keep it
and link its evidence. A draft claim that combines parts of two existing claims
matches neither. Reuse a unique confirmed match by adding its `existing_id` with
a hash-bound patch. Do not edit its content, status or judgments. If several
entities might match, stop before writing and report the ambiguity rather than
guess or create another one. Merge duplicate local refs in the draft before
mapping them to one existing ID, updating or removing their dependent links and
action prompts.

The helper rereads the model just before writing and handles exact repeats
itself. It reuses an entity with the same type and normalized text, plus the
same source for an observation or the same prompting observation for an action.
When several entities repeat it exactly, it reuses one of them. It skips a link
already active between the same entities with the same stance; links already
recorded need no patch. It stops before any write if an `existing_id` is not in
the current model or has the wrong type, if two draft entities repeat each
other, or if a draft link repeats a withdrawn link. Patch out a link that
repeats a withdrawn one; restoring a withdrawn interpretation is the user's
call, not part of sharing. Keep new links to reused entities. If every item already exists, the
save is complete with no model mutations; report that outcome.

This is best-effort protection against duplicates. The reread catches exact
repeats written after your refresh, but a reworded duplicate can still slip
through; only server-side idempotency can close that race.

## Small corrections

Keep the original file. If changes are needed, write a hash-bound patch:

```json
{
  "draft_sha256": "<worker hash>",
  "edits": [
    {"op": "replace", "path": "/entities/h1/text", "value": "Corrected claim"},
    {"op": "replace", "path": "/evidence_links/o1/h1/rationale", "value": "Corrected explanation"}
  ]
}
```

Paths use entity refs or evidence source/target refs, not array positions.
Use `add`, `replace`, or `remove`. Adding an entity at `/entities/o2` supplies
its full object with `ref: "o2"`; adding a link at `/evidence_links/o2/h1`
supplies its full object with matching endpoints. Gaps use `/gaps/<index>` or
`/gaps/-` to append. The helper resolves targets, checks the original hash and
validates the final graph. It leaves the original file untouched.
For deduplication, add an ID at `/entities/o1/existing_id`.

## Save once

After confirmed delivery or explicit skip:

```bash
python3 <skill-dir>/scripts/save_model.py <draft.json> \
  --draft-sha256 <worker-hash> --context "$name" --container <incident-id> \
  --message <message-receipts.jsonl> \
  --actor-id <agent-id> --actor-name '<agent-name>' --journal <model-receipts.jsonl>
```

Add `--patch <edits.json>` only for corrections. For an intentionally skipped
update, use `--skip-message` instead of `--message`. Use concrete, safely quoted
paths and the invoking product's identity. The message journal confirms delivery
and destination; the model carries no duplicate message or private citation list.

The helper validates and rereads the model before writes, saves in dependency
order and journals raw responses, receipts and IDs. Existing IDs are reused
without editing their entities. Its output lists the `reused` entities and
`skipped_links` it found in the reread. It does not send messages or perform
assessment, recovery, verification or remediation. Gaps stay private.

On `saved`, briefly report completion and meaningful gaps. When the output says
no writes were made, fix the cause with a patch, or report a failed read, and
rerun with a new journal. On any other `stopped`, preserve the journal and follow
[publish-recovery.md](publish-recovery.md); a new journal filename does not make
replay safe.
