# RFC structure

Adapted from the [Rust RFC template](https://github.com/rust-lang/rfcs/blob/master/0000-template.md). Preserve its progression from motivation and examples to technical detail and tradeoffs. Scale depth to the proposal; use meaningful headings and omit empty boilerplate. Repository conventions take precedence over filename and metadata defaults.

Suggested outline:

```markdown
# RFC NNN: <Title>

## Summary

One paragraph describing the proposal and its effect.

## Motivation

The concrete problem, affected users, and useful scenarios. Explain why it matters.

## Scope

Goals and meaningful exclusions. Identify dependencies or ownership where useful.

## User experience

The guide-level explanation: teach the design through a concrete example.
Use "How it works" for proposals without a user-facing workflow.
Introduce behavior, concepts and lifecycle before implementation detail.

## Technical design

The reference-level explanation: component boundaries, contracts, resource model,
important flows, permissions, identity, failure behavior and concurrency as relevant.
Place diagrams and examples beside the concepts they explain.

## Drawbacks

Costs, constraints and operational consequences of the proposed design.

## Rationale and alternatives

Meaningful choices and why this design fits. For each consequential choice, state
the alternative and its cost. Avoid recounting discarded drafts.

## Prior art

Name the patterns this design reuses or departs from and what each teaches.
Write "None" if there are none.

## Unresolved questions

Outstanding design decisions and technical validation, clearly distinguished.
State what would resolve each. Do not turn an assumption into an accepted decision.

## Future possibilities

Useful extensions outside current scope, without implying a commitment.

## Terminology

Domain vocabulary and concise definitions. Include only terms that improve
understanding; implementation instructions belong in Technical design.

## References (optional)

Prefer a few descriptive links near the claims they support. For repository RFCs,
keep references within the repository unless the user requests external references.
Incorporate relevant reasoning from external research into the RFC; keep its source
inventory in working context or required private notes. Omit this section when inline
links suffice.
```

The RFC is the single deliverable. Put relevant research conclusions, unresolved questions and terminology here rather than requiring companion context files. Reference existing authoritative documentation instead of copying entire manuals.
