# Provider-owned identity and reconciliation hooks

**Status**: accepted

## Context

The typed resource adapter remains the shared bridge between product configuration and Kubernetes-style envelopes. Some resources have a computed resource name that does not uniquely identify their server-side object. Their reconciliation needs annotations and a complete batch before any mutation; a name-keyed local collection can otherwise discard inputs or target the wrong object. Export can also return readable items alongside item-specific failures.

Dedicated commands and generic resource operations must share domain policy without adding product-specific branches to generic pipelines. The existing first-match natural-key helper and conversion of only `spec` cannot establish safe identity for those resources.

## Decision

Keep `TypedCRUD[T]` and the registered `ResourceAdapter` as the common path. Permit narrowly scoped optional provider-neutral contracts on an owned adapter:

- Pull preflight checks all selected objects before identity-keyed insertion; partial multi-get/read reports retain successful objects alongside item-specific failures and skipped reads.
- Batch push preflight receives complete local envelopes, including same-name inputs, and resolves an invocation-scoped plan before writes.
- Per-item envelope dispatch runs before generic existence and natural-key probes, preserving addressing metadata for the owned reconciliation implementation.
- Delete selection returns minimal identity envelopes before configuration fetching and name-keyed insertion, retaining distinct IDs and enforcing file-based addressing.

Generic pipelines discover these capabilities through the adapter router. Hook signatures use shared resource, filter and envelope types. They do not import product packages, inspect product kind names or implement domain identity rules. Kinds without hooks retain existing behavior, including local reader deduplication. Common mutation guards remain available for resources that do not support writes.

The Assistant Watcher adapter is the first consumer. It owns ID/title collision policy, batch target resolution, authoritative configuration comparison, separate enrollment and partial-completion outcomes. Dedicated commands call the same typed mutation methods. The hooks do not provide a new serialization bridge, erase domain types or introduce a general reconciliation framework.

## Consequences

All relevant input identity survives until the owned policy can decide whether it is safe. Generic push does not make preliminary name probes for opted-in adapters; identity-only delete can work when supplementary configuration cannot be exported. Pull continues retaining readable items and accurate failure coverage.

The added dispatch is a sanctioned exception to the prohibition on new adapter dispatch patterns in [patterns.md](../../architecture/patterns.md). Its scope is the optional contracts above and their shared router/pipeline call sites. A new domain should reuse these contracts only when its identity or completeness needs require them; unrelated resources need no migration. Tests must cover both opted-in behavior and unchanged behavior for kinds without hooks.

No cross-process uniqueness or atomic transaction is implied. The owning adapter reports any definition saved before a failed follow-up stage, including its server identity and a recovery path; generic operation summaries and dedicated commands preserve that partial outcome. A provider-neutral saved-mutation error marker keeps the receipt and exit status 4 when batch processing aborts or callers return an error directly. Watcher mutation requests disable transport retries so a failed write is reported after one attempt; ordinary read retries retain their existing behavior.
