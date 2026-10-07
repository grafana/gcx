# Requests for Comments

RFCs propose engineering changes for review: the problem, user experience, technical design, tradeoffs, and open questions. A proposal is not evidence that a feature has been implemented or deployed. Existing architecture decisions remain documented in [ADRs](../adrs/).

## Index

| RFC | Status |
| --- | --- |
| [001: Unify alerting configuration and resource workflows](001-alerting-provider-refactor.md) | Proposed |

## Authoring and maintenance

Use `NNN-title.md`, choosing the next unused number starting at `001`, title the document `RFC NNN: <Title>`, and add it to this index. Record status in the index rather than in the RFC. Keep later revisions in the same file. Include terminology in the RFC when needed rather than creating a separate glossary.

The repository provides these contributor skills:

- [create-rfc](../../.claude/skills/create-rfc/SKILL.md): research and develop a proposal through review, using the [Rust-based template](../../.claude/skills/create-rfc/references/rfc-template.md).
- [review-rfc](../../.claude/skills/review-rfc/SKILL.md): review an RFC before it is shared or accepted, checking its claims, scope, rules, consistency and structure.
- [update-rfc](../../.claude/skills/update-rfc/SKILL.md): reconcile an existing RFC with implementation evidence when explicitly requested, distinguishing implementation, verification, and deployment.

These workflows do not authorize implementation or publication. RFCs and their examples must be suitable for this public repository; restricted research belongs outside the checkout.
