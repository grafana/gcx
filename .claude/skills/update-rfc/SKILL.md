---
name: update-rfc
description: Reconcile an existing engineering RFC with implemented changes and verification evidence, including PoCs and verified or archived OpenSpec changes. Run when explicitly instructed to update an RFC after verified implementation. After OpenSpec archival, suggest this follow-up without starting it automatically.
---

# Update RFC

Update the existing RFC so engineers can understand the current design, what has been implemented, and what remains unverified. Preserve its identity and agreed intent while incorporating concrete implementation evidence. Work in one document, with terminology near the end, followed by a references section when needed; keep its Rust-style structure or established repository format.

This is a public repository. Use public evidence or independently developed material suitable for public review. Keep restricted research outside the checkout; removing links or attribution does not make internal findings publishable. See [the RFC index](../../../docs/rfcs/README.md) for location and conventions.

## Invocation

Run on an explicit instruction to update the RFC, including a combined request such as “verify, archive, and update the RFC.” After archival, suggest “Would you like to update the RFC?” if that work has not already been requested. Archival alone is not authorization to run this skill. This guidance does not install hooks or modify other archival workflows.

## Locate the RFC and evidence

Read repository guidance, the target RFC, and the supplied implementation references. Follow existing links and repository conventions to locate the relevant RFC when none is named. Ask only if multiple plausible documents imply materially different scope. If no matching RFC exists, report that and leave files unchanged; suggest `create-rfc` when useful. Preserve unrelated edits and update the existing numbered file rather than creating a replacement RFC.

Establish the implementation boundary: change or commit range, relevant code and configuration, verification results, and environment. A local diff is evidence of local work, not of merge or deployment. Prefer focused reads of affected interfaces and behaviors to a repository-wide audit.

When OpenSpec is present, inspect the relevant proposal, design, delta specs, tasks and available verification findings. For archived changes, locate the actual archive and inspect any corresponding canonical specifications. Confirm the mapping to the code rather than inferring completion from an archive directory or checked task list. OpenSpec is optional; code, diffs, tests and other design records support the same workflow. This skill does not verify or archive an OpenSpec change on the user's behalf unless separately requested.

Build a working comparison between affected RFC claims and evidence: unchanged, implemented as designed, implemented differently, partially implemented, deferred, or still unknown. Keep this comparison in working context or required private notes, not a new companion design document.

## Reconcile intent with implementation

This skill normally runs after implementation has already been verified. Use existing proof: OpenSpec specifications and verification findings, other documentation, PRs, code, and recorded test or runtime results. Inspect what that evidence establishes and its scope; do not initiate new implementation verification or rerun tests. If verification is missing, ambiguous, or contradictory, pause the affected update and ask the human to clarify or provide the existing proof. Never infer a passed check from code alone.

Apply these distinctions:

- **Implemented:** behavior exists in the inspected code or configuration.
- **Verified:** named checks exercised that behavior under stated conditions.
- **Deployed:** evidence establishes availability in the named environment.
- **PoC:** observations establish feasibility within the prototype's conditions; remaining production requirements retain their status.

These are independent attributes, not automatic stages. A merged PR, archived change or successful smoke test does not prove all acceptance criteria or production readiness. Preserve findings about missing authorization, concurrency, lifecycle, migration or operational checks when relevant.

Update routine details directly when the evidence is clear: field names, route shapes, component placement, configuration and resolved technical questions. If implementation contradicts an agreed product or security requirement, describe the discrepancy and ask whether it is an intended design change or a defect. Do not silently redefine the requirement to match code. Continue unaffected updates while that decision is pending. An approved decision in supplied context need not be approved again.

Update only portions addressed by the implemented change and supported by existing evidence. Incorporate answers to resolved investigations and open questions into the relevant design sections, then remove those question entries; do not retain struck-through questions or a resolution log. For partially answered questions, retain only the unresolved portion. Preserve untouched design and intended but unimplemented scope.

## Edit a current engineering snapshot

Update affected prose, API and resource examples, Mermaid diagrams, invariants, validation, unresolved questions, and terminology together. Check downstream references to renamed concepts and changed flows. Use component diagrams for architecture, sequence diagrams for flows, and state diagrams for lifecycle behavior; retain useful boundaries and avoid irrelevant platform internals.

Integrate conclusions where they belong. Keep a concise implementation-status or validation section when it helps distinguish delivered behavior from planned scope. Follow the RFC's established reference policy. For repository RFCs without one, keep reader-facing references within the repository unless the user requests external references. External sources may establish evidence; incorporate the relevant conclusions and retain their provenance in working context or required private notes. Cite repository files, recorded checks or pinned source revisions with descriptive links near material claims. Keep raw hashes and source inventories out of the prose. Explain limitations at the relevant claim instead of repeating caveats throughout.

The RFC should read as a design an engineer can use today without the original conversation or research documents. Preserve its progression from proposal and user-visible behavior to technical detail. Use direct language and concrete examples, and give each rule or rationale one clear home. Remove superseded assumptions and resolved placeholders. Preserve rationale that still explains a meaningful tradeoff. Leave execution chronology, debugging detours and interview history out of the narrative; repository history can retain earlier versions. Keep terminology in the RFC, not a separate glossary or context document.

For repositories that treat accepted RFCs as immutable, follow their amendment convention. Do not rewrite a frozen RFC merely because an implementation diverged; identify the permitted update mechanism and ask if that creates a consequential scope choice.

## Review and hand off

Review the diff for lost requirements, changed exact interfaces, unsupported completion claims and inconsistencies among examples, diagrams and prose. Confirm that updates preserve the reading order and reference policy, and consolidate repetition without removing material limitations. Check changed links and example syntax where practical; validate Mermaid when tooling is available. These are document checks, not a new verification pass over the implementation. Pause and ask the human when the existing implementation proof is insufficient.

Finish with the RFC location, the substantive updates, evidence checked and any remaining discrepancies needing a decision. If evidence supports no change, report that instead of manufacturing edits. Routine evidence-backed updates do not require restarting a full design interview.

Keep changes local unless publication is authorized. Commit/push, shared-document writes, PR creation, implementation repairs, deployment and messages to others are separate actions; reuse authorization already given for the same action and scope.
