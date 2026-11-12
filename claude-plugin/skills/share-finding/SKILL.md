---
name: share-finding
description: >-
  Internal to Grafana Labs; run only when the user invokes it by name.
  Share an investigation finding with the incident team: offer a short status
  update to send, skip, or correct, and automatically save its operational
  model. Use when the user says "share this", "share this finding", "share with
  the team", "let the team know", "post this", "share the root cause", or asks
  to publish the current investigation conclusion. One finding per share;
  not for full-thread summaries.
effort: medium
user-invocable: true
disable-model-invocation: true
---

# Share a finding

Ask whether to send the status update, skip it, or correct it. Send an approved
update immediately. Whether sent or intentionally skipped, complete the
supporting model automatically without asking the user to review evidence or
approve the model.
Do not wait for model assembly before asking or sending the approved message.

The main agent owns user interaction, full model content review, and publication.
The worker drafts and checks the model. The request to share authorizes saving
the supporting model; the status-update choice controls only the message. Keep
both within the requested finding and scope. Sharing does not authorize
remediation, claim assessment, impact recovery, or evidence verification.

## Step 0 — resolve the Incident

Use the caller-owned Incident id verbatim from an `…/incidents/<ID>` URL or
explicit Incident reference. Never use an alert-group PK or invent an id.
If the Incident is missing, ask for it and stop.

Resolve the current GCX context, then look up the Incident in it:

```bash
gcx --agent config current-context
```

```bash
gcx --agent --context <context> irm incidents get <incident-id> \
  --json metadata.name,spec.title,spec.status
```

`<context>` is the `current-context` value from the first command. Write that
name out in full in this and every later command; shell variables do not
survive between commands. Each exit code is a gate: exit 0 continues; non-zero
stops with an unshared draft and the failing command. Do not mask failures with
`|| echo`, parse pretty JSON with `tail -1`, substitute an id, or bypass the
Grafana IRM plugin proxy. Keep this context and Incident id for every later
write and recovery. Never re-resolve the active context between them; if the
value is lost, stop rather than use a new default.

Use a `See:` pointer already established in the investigation. Resolve only a
missing detail essential to the message. Leave optional source gaps for the
model draft; do not delay the message for a line-number lookup.

## Step 1 — pin down the finding and its scope

**The thread has already established the finding — lift it, don't re-derive
it.** Do not re-verify evidence, re-open the causal chain, or re-rank
hypotheses: if the thread stated it, treat it as given. Your work here is
selection and wording, not analysis.

The user will either want to share the general conclusion of the thread, or
something more specific. **Match the message's scope to the request.** "This"
means the conclusion the user is pointing at — normally what was just
established in the last few exchanges, but you might want to add some extra
context if nothing was shared previously. If the last few messages have been
clarifying some detail after a more general conclusion that hasn't already been
shared, you may want to ask them to confirm what they mean. A pointed request
("share the cause", "share that X") gets exactly that delta — nothing riding
along, even if adjacent findings are tempting. A broad request ("share your
findings", "share what's happening") gets a short summary of what's established,
and may carry a small number of related findings. Explicit content requests in
the utterance ("with line numbers") override the style defaults below.

**Hold back what isn't ready.** Findings the thread still flags as open, or
that the user is still verifying, stay out of the message no matter how
dramatic — include them at most as clearly-unverified entities, and offer a
separate share for later. Sharing a claim the user wasn't ready to stand
behind is worse than omitting it.

## Step 2 — draft the message

The message is read by responders who have NOT seen this thread. Rules:

- **Lead with what's new to the audience, stated as a conclusion.** The thread
  usually shows what's already known — the alert or incident summary it started
  from; anchor that with a definite reference ("the installs", "the burn"),
  never re-announce it as news. It's also likely that the thread will contain
  the existing operational model state. The message states the conclusion; the
  entities explain the mechanism — one causal clause in plain words. Not a
  walkthrough of the code path, and never commentary about a document or your
  own analysis. "X is failing because Y isn't caught" beats "org Z's retry loop
  is hammering X" and beats "the RCA's claim about X is wrong". Name the systems
  in the causal chain by their real names; no shorthand invented mid-thread.
- **Hedge by table, not by deliberation.** Take the first row that matches and
  move on:

  | Condition                                                        | Prefix                     |
  | ---------------------------------------------------------------- | -------------------------- |
  | The request itself carries a hedge ("share that it looks like…") | mirror the request's hedge |
  | A human confirmed it, or demanded proof and received it          | `Confirmed:`               |
  | Anything else                                                    | `It looks like`            |

  Your own certainty is not confirmation. `Confirmed:` is prose only, not a
  backend assessment or verification.

- **Steer only a ranking the thread itself made.** If the thread converged on
  the real fix — the user endorsed it, or the analysis concluded one option is
  the fix and the others are workarounds — the steer is the most valuable
  sentence in the share. Both halves of "We should X rather than Y" must come
  from the thread: never invent the rejected alternative, and if the thread
  names an immediate mitigation and a long-term fix, don't recast them as
  rivals. Name the concrete change. When options are genuinely still open, share
  the finding without a steer. Describe a proposed fix as addressing the
  observed failure mechanism; do not promise it prevents every recurrence or
  guarantees an SLO outcome that the investigation has not established.
- **Use 2–4 short sentences, no more than 100 words.** Give the finding,
  explanation, and next step room to read naturally; split dense sentences
  rather than joining clauses to fit a sentence count. The word ceiling is not
  a target: leave secondary diagnostics and implementation detail for evidence.
  Then optionally one `See:` line, built from a pointer already established in
  the thread. Take the
  first available in this order, without weighing alternatives: repo `file:line`
  → repo `path (symbol)` → dashboard or saved query → omit the line. If you do not
  already have an exact line, cite `path (symbol)` instead of going to look one
  up — a symbol ages better than a line, which drifts between versions and
  checkouts. Never a path that exists only on this machine.
- **No customer names, environment names, figures, or IDs in the message** —
  they live in the entities ("a customer's provisioning loop", not "org acme's
  crossplane loop at 6.8k/hr on prod-x-y"). One exception: anything the user's
  request names. Never repeat a code symbol the `See:` pointer already carries
  — say it in plain words.
- **Don't assign agency the thread didn't establish.** If nobody knows who
  or why something changed, say "the toggles were disabled", never "the
  customer disabled the toggles".
- **No secrets, tokens, raw payloads, or customer PII** anywhere in the
  share.

Good:

> It looks like checkout latency is coming from the payments DB hitting
> max_connections after this afternoon's pool-size change. We should revert
> the pool-size change rather than scale the DB.
> See: payments/db/pool.go:112 (NewPool)

Bad (buries the conclusion, dumps figures, hedges nothing):

> Analysis of ingress telemetry shows elevated p99 (2.4s, +312%) on
> /checkout correlating with mysql Threads_connected=512/512 since deploy
> 7f3a1c2; connection pool saturation hypothesis under evaluation, potential
> remediation paths include pool resize or replica scaling.

The message is the words themselves — no attribution header ("Shared from
Claude Code:", "Agent update:", a greeting). Attribution travels in the
share's metadata, not its prose.

## Step 3 — offer the update and prepare the model concurrently

Write the body to `body.txt` in a fresh temporary directory, then preview the
update without sending it:

```bash
gcx --agent --context <context> irm incidents share <incident-id> \
  --body-file <body.txt> --see '<pointer>' \
  --drafted-by-agent <agent-id> --drafted-by-agent-name '<agent-name>' --dry-run
```

Pass the pointer without `See:`, or omit `--see` when there is none. Never pass
the text inline with `--body`. `--body-file` reads the file as written, apart
from trailing line breaks, so the send posts exactly the previewed text. Use the
invoking product's identity (`codex` / `Codex` or `claude-code` / `Claude Code`). The output's `preview` field shows the body and
optional `See:` line, and its `context`, `server` and `proxy_endpoint` fields
show where it goes. The server adds attribution for the authenticated user and the named
agent when posting. Keep the output's `digest`; Step 4 needs it.

Put this question before that preview:

> Would you like to send this status update to <incident-id> (<title>) on `<context>` (<server>), skip it, or make a correction?
>
> [Rendered preview]
>
> I'll save the supporting model automatically whether you send or skip the update.

Use **Send update**, **Skip update**, and **Make a correction** options where
available. Edits or silence alone do not authorize sending or mean skip.

Start one background worker using [model-draft.md](references/model-draft.md).
With inherited conversation context, pass only the finding/scope/exclusions,
resolved destination, agent identity, message as context, reference path, and
an assigned `draft.json` path in a fresh temporary directory. Otherwise include
the relevant investigation excerpts, including counterevidence and known
sources. The worker returns the checked draft path and its gaps; it handles
content quality while the user reviews.
If this session has already pulled the operational model for the same context,
stack and Incident, give the worker relevant existing entities and links with
their IDs so it can reuse them in the draft. Do not delay the status-update
question for a model refresh.

With nonblocking input, show the question first and dispatch the worker while
waiting. If the input tool blocks, launch the background worker first, then ask
as soon as the launch returns; do not wait for its draft.
Do not wait for the draft to ask or send. Mention background work only if it
started and the runtime can keep it running during the wait. If background work
is unavailable or fails, prepare the draft locally after send or skip.

While waiting, resolve specific worker gaps or conflicts with the offered
message when possible. Complete the full content review in Step 5 before saving
the model; do not delay sending an approved message for that review.

For a correction, rewrite `body.txt` and rerun the preview, which gives a new
digest. Reconfirm unless the user explicitly asks to send their exact
replacement. Send substantive scope
or finding changes to the worker and reconcile affected entities; a wording-only
edit does not require rebuilding the model. If the worker has finished, use
small edits as described in [model-save.md](references/model-save.md). A changed
Incident or context requires resolving and confirming the new destination and
replacing the old worker artifact. If the user cancels all sharing, stop the
worker and publish nothing.

## Step 4 — send or skip

For **Send update**, send immediately. First record the time with
`date -u +%Y-%m-%dT%H:%M:%SZ`; recovery needs it. Then send with the same
unchanged `body.txt`, the same pointer and the digest from the preview the user
approved:

```bash
gcx --agent --context <context> irm incidents share <incident-id> \
  --body-file <body.txt> --see '<pointer>' \
  --drafted-by-agent <agent-id> --drafted-by-agent-name '<agent-name>' \
  --expect-digest <digest>
```

gcx refuses to send if the text or destination changed. Take the digest only
from a preview the user approved. If it is lost, show the preview again and
ask; never pass the digest of a preview the user has not seen. Any other text
needs a new preview and approval. Effective OAuth user authentication is
required; never bypass the OAuth or proxy gates.

A `gcx.irm.message_receipt` with a `message_id` confirms the update; tell the
user immediately. For **Skip update**, leave the message unposted and continue.
On any other outcome, never retry automatically, because delivery has no
idempotency key; follow [publish-recovery.md](references/publish-recovery.md).
An ambiguous send is not an intentional skip.

## Step 5 — review the full model and save once

Save the model only after a message receipt confirms delivery or the user
explicitly chose **Skip update**. An unconfirmed or failed send blocks the save
until [publish-recovery.md](references/publish-recovery.md) settles it.

Wait for the worker to return; never read, edit or save a draft it may still be
writing. Then read the full draft and review its content using
[model-save.md](references/model-save.md). Review even when the message is
unchanged and the worker reports no gaps. Before writing, reconcile the draft
against the current operational model using the duplicate checks in that
reference, apply any small edits, and save.
Keep the model check after the message choice so it cannot delay a requested
status update.
`upsert-model` validates the draft, rereads the model to reuse exact repeats,
and writes only what is missing. Address errors it reports.

Keep working until the model is saved or a concrete failure prevents completion,
respecting later cancellation or scope restrictions. Briefly report the outcome
and meaningful gaps. On failure, keep what was
saved; a sent update stays sent and a skipped update stays unposted.
