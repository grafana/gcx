# Assistant Watcher version addressing

**Status**: accepted — explicit human-approved constitutional waiver, 2026-10-15

## Context

A retained Watcher definition is identified by both its Watcher and its positive definition version. RFC 002 and issue #1497 propose an experimental `versions` group for inspection, comparison and restore. The command-naming guide defers new composite-identity shapes to an explicit constitutional decision; existing dashboard commands do not authorize a new exception.

## Decision

Record the human-approved exception for these experimental commands only:

```shell
gcx assistant watchers versions get WATCHER VERSION
gcx assistant watchers versions diff WATCHER FROM_VERSION TO_VERSION
gcx assistant watchers versions restore WATCHER VERSION --expected-current-version CURRENT_VERSION
```

`gcx assistant watchers list-versions WATCHER` follows the existing parent-scoped enumeration rule. Versions remain provider-only domain operations without an adapter registration. Configuration commands and generic resource push continue to use their shared TypedCRUD path.

The exception preserves the reviewed RFC syntax. It does not amend the general Constitution, establish a rule for other composite identities, or exempt unrelated commands from naming review.

## Consequences

Version reads disclose server-retained coverage and do not infer unavailable predecessors. Diff compares only the returned definitions. Restore requires a separately paused Watcher and the caller's explicit current-version guard; it creates a new version without activating monitoring. Its result distinguishes accepted version evidence from later state observations and uncertain request outcomes. The version definition has a narrower scope than the configuration manifest; current actions and settings outside that scope are retained.
