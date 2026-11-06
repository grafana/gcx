# Reconcile and save the supporting model

Use the worker's checked draft file.

## Review the full draft

Read the full draft, even when the message is unchanged and the worker reports
no gaps. Review every entity and evidence link against the investigation, final
message, and user scope. If the update was skipped, use the requested finding
and scope instead of a sent message.

Check factual grounding, uncertainty, evidence meaning, relevant counterevidence,
and established next steps. Reconcile user changes and worker gaps. Check the
draft's fidelity to the supplied investigation; do not repeat the investigation
or fetch new evidence. Preserve unresolved uncertainty rather than inventing
evidence. Apply content corrections as small edits using the procedure below.

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
matches neither. Reuse a unique confirmed match by adding its `existing_id` to
the draft entity. Do not edit its content, status or judgments. If several
entities might match, stop before writing and report the ambiguity rather than
guess or create another one. Merge duplicate local refs in the draft before
mapping them to one existing ID, updating or removing their dependent links and
action prompts.

`upsert-model` rereads the model just before writing and handles exact repeats
itself. It reuses an entity with the same type and the same text, ignoring
spacing and a final period but not case, plus the same source for an
observation or the same prompting observation for an action. When several
entities repeat it exactly, it reuses one of them. It skips a link already
active between the same entities with the same stance; links already recorded
need no edit. It rejects the draft before any write if an `existing_id` is not
in the current model or has the wrong type, if two draft entities repeat each
other, or if a draft link repeats a withdrawn link. Remove a link that repeats a
withdrawn one; restoring a withdrawn interpretation is the user's call, not part
of sharing. Keep new links to reused entities. If every item already exists, the
save is complete with no model mutations; report that outcome.

This is best-effort protection against duplicates. The reread catches exact
repeats written after your refresh, but a reworded duplicate can still slip
through; only server-side idempotency can close that race.

## Small corrections

Edit the draft file in place. Find an entity by its `ref` and a link by its
`source` and `target`, never by array position. To reuse an existing entity, add
its `existing_id`. When you remove or merge a ref, update or remove the links and
`prompted_by` values that name it. Then rerun the save command below with
`--dry-run`, which checks the edited draft against the current model and writes
nothing. A usage error (exit 2) lists every violation with its JSON path.

## Save once

Run this only after a message receipt confirms delivery or the user explicitly
skipped the update:

```bash
gcx --agent --context "$name" irm opmodel containers upsert-model <incident-id> \
  -f <draft.json> \
  --actor-kind agent --actor-id <agent-id> --actor-name '<agent-name>'
```

Use the concrete draft path and the invoking product's identity. The model
carries no copy of the message or private citation list.

The command validates the draft and rereads the model before writing, then
saves in dependency order: observations, claims, evidence links, actions.
Existing IDs are reused without editing their entities. It stops at the first
failed write. Its receipt maps refs to IDs under `entities` and lists the
`reused` entities and `skipped_links` it found in the reread. It does not send
messages or perform assessment, recovery, verification or remediation. Gaps
stay private.

On `saved`, briefly report completion and meaningful gaps. A usage error (exit
2) or a failed model read means nothing was written; fix the cause and rerun. On
`stopped` (exit 4), follow [publish-recovery.md](publish-recovery.md).
