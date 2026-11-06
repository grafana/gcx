# Publish failure and recovery

Use this when a message or model command fails. Treat
GCX receipts as authoritative and keep message delivery separate from model
saving. Preserve the pinned `name` context, Incident id, agent identity, and
approved message (if any) and requested model scope. Never bypass the proxy or
effective OAuth gates.

An intentionally skipped update is not a delivery failure and needs no message
receipt. Never send a skipped update to recover model writes, or relabel a failed
or ambiguous message attempt as an intentional skip.

## Message delivery

A `gcx.irm.message_receipt` with a `message_id` confirms Incident storage, not
Slack delivery. A failed attempt may still have made a request. Do not resend a
delivered update to recover a model failure. Missing confirmation is not proof
that no message exists.

If the command failed with a usage error (exit 2), nothing was sent: correct the
invocation and retry once without changing the confirmed wording or destination.
For a definite HTTP rejection, stop and report the failure. Retry only once
the cause is addressed, keeping the confirmed wording and destination. An
authentication error must not trigger a switch of context, credentials,
container, or backend path to get around it.

After a timeout, connection reset, or other ambiguous transport failure, do not
automatically retry. The message may already have been saved, and delivery has
no idempotency key. Inspect the incident first. If delivery remains uncertain,
stop and report the unresolved outcome; do not claim success or absence. Keep
the evidence draft, but do not save it while message delivery is unconfirmed in
this flow.

When delivery has been established as absent and the cause is addressed, rerun
the send command from Step 4 with the original confirmed body and pointer, agent
identity, context, and Incident id. There is no citation flag or automatic Slack
details reply.

## Model writes

A `stopped` receipt (exit 4) names the failed write in `stopped_at` and its
cause in `error`; earlier writes stay saved and are listed with their IDs.
Report which entities and relationships were saved before the failure. Do not
resend the Incident message.

Fix the cause, then rerun the same `upsert-model` command on the same draft.
It rereads the model, reuses what already landed, and creates only what is
missing, including a write whose response was lost. Keep the draft unchanged
apart from edits that fix the cause, so the rerun matches what landed. The same
applies when the command was interrupted without a receipt.

On a conflict, review the reported domain rule and reconsider the intended
write. Do not impersonate a human or rerun to bypass rejection. Never infer a
minted id from a local ref or reuse an id from another container. If the cause
cannot be fixed, stop and report the saved and unsaved parts.
