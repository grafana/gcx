# Draft supporting evidence

This is a drafting-only assignment. Use the supplied investigation excerpts,
message, scope, Incident id, GCX context, and agent identity. Do not publish
anything, contact the user, change the message, or delegate further. Run no GCX
command other than the dry run below. Do not re-investigate or retrieve missing
evidence. Return a specific gap if the supplied material cannot support the
message. If the update is skipped, use the requested finding and scope; the
draft message is context, not a published message.

You own content quality: select grounded facts, preserve uncertainty, scope,
counterevidence and established next steps. Check evidence meaning against the
supplied investigation while drafting. The main agent reviews the full draft
against the investigation and final scope before saving.

Select the smallest useful set of entities already established in the thread:

- **observation** — a concrete fact with its known `source_ref` (file and
  symbol, file:line, log query, dashboard, or deploy). Preserve measured
  magnitudes and uncertainty; do not invent a source, observation time, or
  original author from posting metadata. The recording agent is not necessarily
  the original author.
- **hypothesis** — a falsifiable causal claim, not a symptom. Include a
  relevant ruled-out explanation when it would save responders wasted work.
- **impact** — what is broken or degraded and for whom.
- **action** — an established next step, planned or done. Set `prompted_by`
  to the local ref of the observation that prompted it; omit only for a
  standalone action. Promotion does not require an evidence link to a claim,
  consume the observation, or transfer verification.
- **evidence link**: an observation linked to a hypothesis or impact, with
  stance `supports`, `challenges`, or `context`. Claims, including impacts,
  cannot be evidence sources. Preserve counterevidence.

Every extra entity should change what a responder would do or stop doing.
Do not trim decision-relevant content to meet a count. Stay
within the user's scope and exclusions; do not include secrets or customer PII.
Open findings the user is still verifying do not become established claims.
A challenging evidence link does not assess a claim. A completed action does
not establish impact recovery. Sharing a finding authorizes saving supporting
entities and links, not assessment, recovery, or verification.

Write a compact draft to the assigned JSON file, using local refs:

```json
{
  "entities": [
    {
      "ref": "h1",
      "type": "hypothesis",
      "text": "The pool-size change exhausts database connections"
    },
    {
      "ref": "o1",
      "type": "observation",
      "text": "Connections reached the configured maximum after the change",
      "source_ref": "grafana: d/payments-db"
    },
    {
      "ref": "a1",
      "type": "action",
      "text": "Revert the pool-size change",
      "prompted_by": "o1"
    }
  ],
  "evidence_links": [
    {
      "source": "o1",
      "target": "h1",
      "stance": "supports",
      "rationale": "Saturation began with the change"
    }
  ]
}
```

Use `evidence_links: []` when none are needed. Reuse an `existing_id` only when
its identity, type and destination were established in the investigation or in
the supplied pulled model. Represent a reused entity with one local ref and
`existing_id` so new links can target it; omit links already present. The main
agent will refresh the model before saving; do not read it separately or assume
the supplied view is still current.

Before handing off, check the draft against the current model without writing:

```bash
gcx --agent --context <context> irm opmem containers upsert-model <incident-id> \
  -f <draft.json> --dry-run \
  --actor-kind agent --actor-id <agent-id> --actor-name '<agent-name>'
```

A usage error (exit 2) lists the violations, with JSON paths where it can. Correct them in
your draft and repeat only if needed. If it rejects a link that repeats a
withdrawn one, remove the link; restoring a withdrawn interpretation is not part
of sharing. A `planned` receipt means the draft is ready. If the model cannot be
read, report the error with the draft path.

Return the draft path and specific gaps in your report, without repeating the
JSON. Gaps never go in the file. The message is context, not a field to copy
into the model. Do not wait for message approval to finish drafting or checking.
If files cannot be shared, return the JSON once for the caller to store and
check.
