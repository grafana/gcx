# Tasks

## 1. Read client and configuration model

- [x] 1.1 Implement read-only collection/detail, enrollment and calibration-progress client operations using the existing Assistant transport and typed errors; verify request/error mapping with httptest.Server and keep API mapping in Go client code.
- [x] 1.2 Add exhaustive cursor paging, archive partition selection, repeated-cursor protection, cancellation and bounded supplementary-read concurrency. Verify source mappings against the private evidence note before coding and test each pagination/failure boundary.
- [x] 1.3 Define the complete strict Watcher schema, example and domain type from the design table, including all secret union alternatives and the configured-secret preserve projection. Add table-driven validation and conversion tests; do not resolve secret sources.
- [x] 1.4 Retain a private, field-by-field inventory of response settings excluded from spec for step 2. Verify that every current response field is modeled or explicitly excluded; public artifacts contain categories only; verify the inventory against current response types.

## 2. Typed resource and identity

- [x] 2.1 Implement Watcher TypedCRUD list/get, title-derived resource names, same-stack ID annotation, and lazy factory. Leave mutation callbacks unset; add descriptor/schema/example as the Assistant provider's second registration; verify discovery and schema/example registration tests and update the package map.
- [x] 2.2 Share name/ID resolution across dedicated and generic reads; exhaust both archive partitions for name collision detection and retain ID precedence. Cover same-title and same-slug collisions, archived candidates, later pages and direct-ID reads.
- [x] 2.3 Test configuration reads through httptest.Server: separate enrollment observations, invalid/missing responses, unavailable versus denied settings, secret stripping and manifest JSON/YAML parity.
- [x] 2.4 Ensure unavailable collection capability survives generic error handling instead of being silently skipped. Test generic push/delete for actionable unsupported errors and zero mutation requests, including create and update push paths.

- [x] 2.5 Preserve native preferred versions while falling back to registered provider descriptors for missing kinds on unversioned lookup and preferred enumeration. Keep explicit versions exact; cover same-group mixed versions, native preference, duplicate prevention and real generic Watcher commands with regression tests.

## 3. Commands and status

- [x] 3.1 Add experimental watchers/list/get/status with options/setup/Validate, argument validators, output codecs, readable help and examples. Bind archived-only list selection, explain default caller-visible collection coverage in help and verify command construction tests.
- [x] 3.2 Emit full list items and coverage metadata, initialized empty arrays, identical get envelopes across both paths, and finite agent results. Fetch equivalent configuration in all output formats and verify cross-path JSON/YAML parity tests.
- [x] 3.3 Implement status mapping for lifecycle, archive observation, assessment, available run times, usage/sample context, audit, current version, calibration and checks. Handle sub-read failures explicitly without fabricating observations; preserve unsupported check information; verify status mapping and sub-read failure tests and document observed-field semantics.
- [x] 3.4 Add shared output-class and command-annotation entries, Cloud-only metadata, token costs and useful agent hints. Test experimental metadata and command-level validation, flag binding, output destinations and representative machine/human results.

## 4. Safe pull and documentation

- [x] 4.1 Add the narrow shared pull preflight before identity-keyed collection insertion, keeping candidate formatting and the cross-archive identity-only collision index in the Watcher package. Do not read supplementary configuration for candidates outside the export partition. Report every colliding candidate, retain unique resources and honor existing error policies without adding a new framework; verify the end-to-end collision tests in 4.2.
- [x] 4.2 Drive real generic pull against httptest.Server to test collisions across pages and archive partitions, unchanged existing collision files, unique-file success, zero results, explicit-ID pulls, secret-free artifacts and accurate partial-failure receipts/status; verify all cases pass without real backend mutations.
- [x] 4.3 Add Watcher reference docs and package-map/architecture updates. Check relevant skill routing and generated help/reference output. Verify links and the doc-maintenance structural checks.
- [x] 4.4 Add CODEOWNERS coverage with both gcx and Assistant teams for Watcher reference docs, the existing Assistant MCP reference doc, and Watcher OpenSpec spec/change/archive paths. Keep shared registries and pull glue gcx-owned; verify each new path matches the intended CODEOWNERS entry.

## 5. Verification and review

- [x] 5.1 Run table-driven client, schema, adapter and command tests plus root wiring/agent conformance checks. Complete triggered contribute self-review checks and the four-level compliance review; address findings.
- [x] 5.2 Run `GCX_AGENT_MODE=false GOMAXPROCS=4 mise run --jobs 1 all` and `GCX_AGENT_MODE=false mise run reference`; inspect generated changes and structural doc-maintenance checks. Complete the mandatory pre-commit checks before committing.
- [x] 5.3 Ask the user to refresh the throwaway context with `gcx login --context <test-context>` before live e2e. Check supplementary-read availability early. Using the built bin/gcx, exercise list, archived list, get JSON/YAML, generic-get parity, status and generic pull. Record verified coverage and unavailable probes privately.
- [x] 5.4 If no Watchers exist, prepare the concrete test fixture and obtain approval before creating it. Clean up only an approved fixture; read-only implementation never creates one implicitly.
- [x] 5.5 Review the complete outgoing diff and all prose/artifacts/metadata for private API names, endpoints, headers, gating names, exact limits and source-derived details. Keep wire identifiers confined to Go client code. Inspect reachable commits and confirm no private evidence note enters git.

- [x] 5.6 Address attached review findings: preserve per-item read results and accurate receipts, keep plain typed client errors with boundary rendering, drop unused server fields, disclose selector-free discovery impact, and revert unrelated AGENTS wording. Run regressions and required gates before committing the corrections.

## 6. Publication after implementation review

- [x] 6.1 Commit the reviewed implementation after required checks, fetch current origin/main, and rebase only this change's commits using the recorded .context/base-sha. Keep the existing branch name and drop planning-base commits; verify origin/main ancestry and that no docs-base commits enter the outgoing range.
- [x] 6.2 Re-run required gates after rebase and inspect complete outgoing commits/diff/metadata. Prepare the concrete draft PR title/body with Closes #1493 and William Dumont review request; follow repository conventions and omit internal tracker references; verify the reviewed PR metadata matches the prepared draft.
- [x] 6.3 Obtain the user's concrete publication approval as required by the workspace working agreement, then push/open the draft PR within that approved scope and verify published branch/PR state. Do not merge or archive before merge.
