# Requests for Comments

RFCs propose engineering changes for review: the problem, user experience, technical design, tradeoffs, and validation still needed. A proposal is not evidence that a feature has been implemented or deployed. Existing architecture decisions remain documented in [ADRs](../adrs/).

## Index

| RFC | Status |
| --- | --- |
| [001: Unify alerting configuration and resource workflows](001-alerting-provider-refactor.md) | Proposed |

## Authoring and maintenance

An RFC sits between a GitHub issue that proposes the work and the per-PR OpenSpec changes under [`openspec/changes/`](../../openspec/) that implement it. Write one when the work spans several PRs.

Use `NNN-title.md`, choosing the next unused number starting at `001`, and add the document to this index. Keep later revisions in the same file. Include terminology in the RFC when needed rather than creating a separate glossary.

The repository provides two contributor skills:

- [create-rfc](../../.claude/skills/create-rfc/SKILL.md): research and develop a proposal through review, using the [Rust-based template](../../.claude/skills/create-rfc/references/rfc-template.md).
- [update-rfc](../../.claude/skills/update-rfc/SKILL.md): reconcile an existing RFC with implementation evidence when explicitly requested, distinguishing implementation, verification, and deployment.

These workflows do not authorize implementation or publication. RFCs and their examples must be suitable for this public repository; restricted research belongs outside the checkout.
