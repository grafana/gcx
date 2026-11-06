# Publish failure and recovery

Use this when a message or model command fails or returns a warning. Treat
GCX receipts as authoritative and keep message delivery separate from model
saving. Preserve the pinned `name` context, Incident id, agent identity, and
approved message (if any) and requested model scope. Never bypass the proxy or
effective OAuth gates.

An intentionally skipped update is not a delivery failure and needs no message
receipt. Never send a skipped update to recover model writes, or relabel a failed
or ambiguous message attempt as an intentional skip.

## Message delivery

Use the message helper's result and journal: `saved` confirms Incident storage,
not Slack delivery. A stopped attempt may still have recorded a receipt or made
a request. Do not resend a delivered update to recover a model failure.
Missing confirmation is not proof that no message exists.

If the command failed during local validation, before any request, correct the
invocation and retry once without changing the confirmed wording or destination.
For a definite HTTP rejection, stop and report the failure. Retry only once
the cause is addressed, keeping the confirmed wording and destination. An
authentication error must not trigger a switch of context, credentials,
container, or backend path to get around it.

After a timeout, connection reset, or other ambiguous transport failure, do not
automatically retry. The message may already have been saved, and delivery has
no idempotency key. Inspect the incident first. If delivery remains uncertain,
stop and report the unresolved outcome; do not claim success or absence. A
prepared evidence draft can be retained, but do not save it while message
delivery is unconfirmed in this flow.

When delivery has been established as absent and the cause is addressed, use
`gcx irm incidents share` with the original confirmed body, optional `--see`,
agent identity, context, and Incident id. Never use `shares publish` for a
message-only retry. There is no citation flag or automatic Slack details reply.

## Model writes

Use the save helper's ID mapping and journal to identify completed operations
and pending writes without a confirmed outcome; it parses mutation receipts and
promotion IDs.
Retain the local-ref-to-id mapping and report which entities and relationships
were saved before the failure. Stop the remaining writes; do not replay the
whole draft or resend the Incident message.

Do not automatically retry a failed mutation after an ambiguous response: it
may have created an entity despite the missing receipt. Inspect the model for
that Incident in the pinned context before proposing recovery. On a conflict,
review the reported domain rule and reconsider the intended write. Do not
impersonate a human or replay mutations to bypass rejection. Never infer a
minted id from a local ref or reuse an id from another container. Resume only
the remaining in-scope writes once their prerequisites and the outcome of the
failed write are known. If an outcome remains uncertain, stop and report it
rather than risk a duplicate or ask for routine package approval.
