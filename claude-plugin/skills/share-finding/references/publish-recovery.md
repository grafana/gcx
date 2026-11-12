# Publish failure and recovery

Use this when a message or model command fails. Treat
GCX receipts as authoritative and keep message delivery separate from model
saving. Preserve the pinned context name, Incident id, agent identity, and
approved message (if any) and requested model scope. Never bypass the proxy or
effective OAuth gates.

An intentionally skipped update is not a delivery failure and needs no message
receipt. Never send a skipped update to recover model writes. Never relabel a
failed or ambiguous message attempt as an intentional skip yourself; only the
user can make that choice.

## Message delivery

A `gcx.irm.message_receipt` with a `message_id` confirms Incident storage, not
Slack delivery. A failed attempt may still have made a request. Do not resend a
delivered update to recover a model failure. Missing confirmation is not proof
that no message exists.

Read the exit code and error summary to decide what happened:

- **Exit 2 with `the message or its destination changed since the preview`**
  means nothing was sent because the text, pointer, agent, Incident, context or
  server differs from what the user approved. Never rerun the preview just to get a
  new digest for the same send. Show the new preview and ask again.
- **Any other exit 2** is a local usage error. gcx rejected the flags or
  arguments and sent nothing. Fix only the flags and retry once. Never change
  the confirmed wording or destination to get past it.
- **Exit 3** is an authentication failure, and nothing was saved. Stop and report
  it. Never switch context, credentials, container, or backend path to get
  around it.
- **Exit 1 with `message sharing failed with HTTP 4xx`** means the server
  rejected the update, and nothing was saved. Stop and report the server's
  reason. If the body is too long, ask the user for shorter wording and preview
  it again. If the incident was not found, confirm the incident with the user.
  Never reword or redirect the update yourself.
- **Exit 1 with HTTP 5xx, exit 5, a timeout, a connection reset, or no receipt**
  is uncertain. The update may already be saved, and delivery has no
  idempotency key. Do not resend. If Step 0 showed the incident as resolved,
  Incident refuses status updates and reports a server error. Tell the user the
  update was not posted because the incident is resolved, and do not offer a
  resend. Otherwise, check the incident's activity:

```bash
gcx --agent --context <context> irm incidents list-activity <incident-id> \
  --kind keyUpdateAdded --limit 100 --json body,createdTime
```

This lists the incident's status updates, oldest first, and Incident returns at
most 100. If it returns 100, the newest updates may be missing, so the check
cannot settle delivery. Report the outcome as unresolved and let the user check
the incident.

The update was delivered if an item has a `body` that contains the sent text
under an attribution line naming the agent, and a `createdTime` no earlier than
two minutes before the time recorded in Step 4. Clocks can differ, so allow that
margin. Formatting in `body` may differ from the sent text. Incident adds
activity items asynchronously, so if nothing matches, wait about a minute and
check once more.

If a match appears, treat the update as delivered: tell the user and continue to
the model save. If none appears, tell the user that no matching status update
appeared in the incident's activity. Incident can store an update without its
activity item, so ask the user to check the incident's updates in Grafana before
deciding whether to resend. Keep the evidence draft, and do not save it until
the user has decided. If the user chooses not to resend, that is their explicit
skip: continue to the model save. If they resend, wait for its receipt first.

To resend, rerun the Step 4 send command with the original `body.txt`, pointer,
agent identity, context and Incident id. There is no citation flag or automatic
Slack details reply.

## Model writes

A `stopped` receipt names the failed write in `stopped_at` and its cause in
`error`. Exit 4 means this invocation confirmed earlier writes. With no
confirmed writes, the exit code describes the failure, such as 3 for
authentication or 5 for cancellation. The receipt may also list existing
entities and skipped links; those are not new writes. Report the confirmed
writes and the failed step. Do not resend the Incident message.

Fix the cause, then rerun the same `upsert-model` command on the same draft.
It rereads the model, reuses what already landed, and creates only what is
missing, including a write whose response was lost. Keep the draft unchanged
apart from edits that fix the cause, so the rerun matches what landed. The same
applies when the command was interrupted without a receipt.

On a conflict, or a server rejection (`opmem rejected the request`, exit 1),
review the reported reason and reconsider the intended write. Do not impersonate a human or rerun to bypass rejection. Never infer a
minted id from a local ref or reuse an id from another container. If the cause
cannot be fixed, stop and report the saved and unsaved parts.
