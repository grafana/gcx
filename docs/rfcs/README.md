# Requests for Comments

RFCs record engineering proposals and design decisions: the problem, user experience, technical design, tradeoffs, and open questions. Browse [this directory](.) for the records. A proposal is not evidence that a feature has been implemented or deployed.

## Authoring and maintenance

Follow the [contribution workflow](../../CONTRIBUTING.md#contributing-code) to determine whether an RFC is needed and obtain the applicable maintainer agreement. An RFC records design and scope decisions for implementation PRs. Developers choose their own implementation and planning approach. Start agent-assisted contribution work with [`contribute`](../../.agents/skills/contribute/SKILL.md).

Use `NNN-title.md`, choosing the next unused number, and title the document `RFC NNN: <Title>`. Keep later revisions in the same file. Record status near the title, such as proposed, accepted, implemented, superseded, or retired; link a replacement when applicable. Keep terminology in the RFC. Do not maintain a separate RFC list or copy status into other documents.

The repository provides these contributor skills:

- [create-rfc](../../.agents/skills/create-rfc/SKILL.md): research and develop a proposal through review, using the [Rust-based template](../../.agents/skills/create-rfc/references/rfc-template.md).
- [review-rfc](../../.agents/skills/review-rfc/SKILL.md): review claims, scope, rules, consistency, and structure before sharing or acceptance.
- [update-rfc](../../.agents/skills/update-rfc/SKILL.md): reconcile an existing RFC with implementation evidence when explicitly requested, distinguishing implementation, verification, and deployment.

Use the template for new proposals and adapt its depth to the change. Historical records may retain their original structure and decision context; moving a record does not change its status or ratify its proposals.

These workflows do not authorize implementation or publication. RFCs and their examples must be suitable for this public repository; restricted research belongs outside the checkout.
