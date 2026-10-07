---
name: review-rfc
description: Review an engineering RFC before it is shared or accepted. Checks its claims against code and documentation, its scope against the originating request, repository rules, internal consistency and template structure, then reports author decisions, fixes and side-channel questions. Trigger on "review this RFC", "check the RFC against <issue>", "is this RFC ready to share". Not for drafting or revising an RFC (create-rfc), reconciling one with implementation (update-rfc), or reviewing a code PR (review-pr).
---

# Review RFC

Review the RFC as it stands and produce a report. The author owns the decisions; the RFC changes only when they ask, and then the writing rules in [create-rfc](../create-rfc/SKILL.md) apply. Posting the report to a PR or issue is a separate action that needs its own authorization.

## Gather

Read the RFC, the [RFC index](../../../docs/rfcs/README.md) and the [template](../create-rfc/references/rfc-template.md). Find the originating request (issue, notes, earlier decisions) and the code, documentation, ADRs and other RFCs the RFC's claims touch. If the RFC has uncommitted or recent edits, compare it with the previous revision: content removed in the edit is part of the review.

Done when every source is read, or named in the report as unavailable. Without the originating request the scope check cannot run; ask for it or report the gap.

## Checks

Run every check. Claims, scope and repository rules are independent: bounded research agents may run them in parallel, and every citation they return is verified before it enters the report.

1. **Claims.** Every statement about existing behavior is a claim: gcx code and framework capabilities, product APIs, documentation, and precedents the RFC says it follows. Verify each against its primary source and cite `file:line` or the document section. Look hardest at framework features the design assumes, at whether a cited precedent really behaves as described, and at whether each API operation the design depends on exists and does what the design needs. Done when every claim is verified, contradicted or marked unverifiable.
2. **Scope drift.** Map each requirement in the originating request to the RFC: covered, explicitly excluded, or deferred to named follow-up work. Anything else is a finding, and so is scope the RFC adds without saying why. Done when every requirement has a mapping.
3. **Repository rules.** Walk the compliance hierarchy in `AGENTS.md` in order and name each rule the design touches, with a verdict. Report implementation-level rules, such as test gates, exit codes and output registration, only when they change the design. Done when every touched rule has a verdict.
4. **Consistency.** Read each example, diagram, table and walkthrough against the rules it illustrates. Every step a rule requires appears in the walkthrough; every field and flag in an example is defined by the rules, and every rule is reflected in the examples; renamed concepts are renamed everywhere; links and anchors resolve. Done when every example and diagram has been read against its rule.
5. **Settled versus open.** For each choice the RFC calls settled, check that its enabling contract is established. A settled choice resting on an open question is a finding: it needs a stated fallback or belongs among the open questions. Each open question says what would resolve it. A settled decision reopens only on new evidence, so name the evidence. Done when every settled choice and every open question has been checked.
6. **Structure.** Compare the sections with the template, in order. Each section does its job: Drawbacks names costs, Rationale and alternatives names the alternative to each consequential choice and its cost, and Prior art names the patterns reused or departed from and what each teaches. Each rule has one home, and status lives in the RFC index. Run this check last; it is the cheapest.

## Side-channel questions

Some findings rest on sources the RFC cannot cite, or on detail that belongs outside it. Keep that detail out of the RFC text and out of suggested wording, and list the questions separately so the author can raise them through another channel. When a fix depends on such detail, propose wording that states only the property the design needs.

## Report

Rank findings by what they cost the design, most costly first, and write each one so the author can act on it without rereading the sources:

1. **Decisions for the author:** choices the review cannot make, such as scope questions and settled choices that new evidence undermines. Give a recommendation and its main tradeoff.
2. **Fixes:** contradicted claims, rule violations and consistency defects. Give the RFC location, the evidence and proposed wording.
3. **Side-channel questions:** the separate list described above.
4. **Structure and editorial:** a few lines at most.

Close with what the review could not check and why.
