# Tasks

Implementation and the narrowly opted-in collection-identity extension were
authorized through the apply workflow. Unnamed TimeInterval and TemplateGroup
manifests must retain distinct logical identities before reaching the pusher;
malformed inputs must remain available for individual failure reporting.

## 1. Native identity policy and portable push

- [x] 1.1 Add the opted-in pure TimeInterval/TemplateGroup name policy under `internal/resources/` per design D3–D4, including logical identity, historical kind normalization, read-only classification, and deep-copy create preparation without encoded-name computation; verify table-driven tests cover both API versions, empty/malformed fields, preserved spec/provenance, removed source identity fields, unchanged original input, and non-opted-in resources.
- [x] 1.2 Integrate native logical matching with `findByNaturalKey`/`naturalKeyCache` without adapter registrations, rejecting native ambiguity while preserving existing adapter semantics; verify pusher-level tests cover unnamed manifests, GET-not-found followed by a logical match, equal titles with different kinds, and historical-to-new encoding on repeated pushes. Preserve distinct unnamed logical identities in the resource collection and actual directory reader, including equal-title different-kind templates and malformed inputs; verify directory-to-pusher aggregation and unchanged named/non-opted-in collection keys.
- [x] 1.3 Apply create preparation only on the actual create path, retain destination name/version on update, treat mismatched name hints as logical-identity lookup, and skip read-only native push before lookup or mutation (superseding the original imported-input failure policy); verify a recording transport checks unnamed create, current destination version, edited-title new identity on populated/empty targets and repeated pushes, historical/current name collisions without wrong-object updates, no create after forbidden/transport errors, and ordinary caller-selected names unchanged.
- [x] 1.4 Document the opted-in name policy and matching sequence in `docs/architecture/resource-model.md`, `data-flows.md`, and `patterns.md` where applicable; verify doc-maintenance structural checks and Markdown links match the final implementation, with package-map updates only if structure changes.

## 2. Discovery and safe preview

- [x] 2.1 Expose timeintervals/templategroups alongside routingtrees in `partiallyExposedGroups`; verify filtered-discovery and registry tests resolve both served versions and the preferred version while receivers, inhibitionrules, and newly introduced unreviewed kinds remain hidden.
- [x] 2.2 Keep both kinds outside the static server-dry-run allowlist; add guard/pusher/deleter regression tests that would fail if a POST/PUT/PATCH/DELETE escaped for either version, and verify skipped/unverified output and the existing explicit assertion override without modifying its behavior.
- [x] 2.3 Update affected discovery descriptions and the existing routing-tree generic-access requirement consistently with the delta; verify no document still claims that only routingtrees is exposed by the completed change.

## 3. Time interval commands and existing provisioning export

- [x] 3.1 Mount the time-intervals group through native.Bind and implement list/get exact title resolution and tables; verify native.Fixed/Func and HTTP tests cover complete pagination, empty items, case-sensitive matching, missing/ambiguous titles, both title/name output, preserved manifests, version selection, and error propagation.
- [x] 3.2 Implement create/update with native.ReadManifest, shared create preparation, strict create conflict semantics, caller update version, and current-title subject resolution independent of the desired title; verify real-command tests cover file/stdin, existing/missing resources, stale versions, missing RV, explicit rename with the old API name and supplied version, rename conflict without another mutation, changed argument plus retained old name rejection, wrong group/kind, explicitly empty flags, and version disagreement before I/O. Verify a rename receipt shows old/new titles and the returned API name without rewriting the input.
- [x] 3.3 Implement delete using standard confirmation and structured receipts; verify forced deletion, declined prompt, closed stdin, agent mode without force, title lookup failure, and preserved server errors, with no request when confirmation is unavailable.
- [x] 3.4 Keep provisioning export on the existing mute-timings command and direct native time-interval help there; the initially implemented duplicate leaf was removed during PR review. Preserve legacy raw bytes, selectors, format validation and classification.
- [x] 3.5 Add all six output-class and agent annotations, routing metadata/examples, and generated CLI reference; verify command consistency/conformance tests, relevant skill-command drift, reference drift, and rendered help/commands accurately describe native manifests versus provisioning exports.

## 4. Template group commands

- [x] 4.1 Mount the template-groups group through native.Bind and implement complete list/get, title/name/kind/read-only tables, and `--kind grafana|mimir` on get/delete; verify native and HTTP command tests cover imported groups, absent historical kind, equal-title collisions, explicit disambiguation, invalid/empty kind, preserved structured output, and empty lists.
- [x] 4.2 Implement create/update with the shared create policy, caller RV, manifest-kind selection, and current-title resolution independent of the desired title; verify stale/current versions, stdin, explicit title rename with unchanged kind and a single update to the old API name, rename conflict, wrong group/kind, attempted kind conversion, imported create/update, retained-name mismatches, missing targets, and unchanged other groups. Assert no mutation on local rejection and old/new titles plus the returned API name in rename receipts.
- [x] 4.3 Implement confirmed delete for editable groups, reject imported deletion even with force, and add structured receipts; verify editable/imported equal-title behavior, declined/unavailable confirmation, and server error passthrough.
- [x] 4.4 Add the five output-class/agent annotation entries, help/examples, and generated CLI reference; verify no template-group export exists, original templates behavior is unchanged, consistency/conformance/reference-drift checks pass, and read-only/imported guidance renders accurately.

## 5. Documentation and quality gates

- [x] 5.1 Reconcile the source-backed RFC 001 matrix and final slice contract with the implementation; verify exact public tag links and keep source registration, runtime E2E, omitted-kind defaulting, and untested 12.x patch coverage distinct. Check omitted-kind writes on a newer target before claiming defaulting; document any unsupported cross-version payload rather than claiming it was exercised.
- [x] 5.2 Run the final compliance sweep and doc-maintenance gate from CLAUDE.md; verify CONSTITUTION/DESIGN compliance, VISION fit, actual architecture changes documented, and no copied `.claude/skills/openspec-*` setup files staged.
- [x] 5.3 Format touched Go files, run targeted tests, then `GOMAXPROCS=4 mise run --jobs 1 gate`; verify it passes and resolve failures before the full gate.
- [x] 5.4 Before PR publication, sync with origin/main under the repository checklist and run `GCX_AGENT_MODE=false GOMAXPROCS=4 mise run --jobs 1 all`; verify full lint/tests/build/docs and generated references pass. Run the mandatory pre-commit checklist before every commit and inspect status for unintended changes.

## 6. Integration verification and review evidence

Use the freshly built `bin/gcx`, an isolated scratch config, an isolated discovery
cache, and a unique task prefix for titles and template definitions. The private
working note maps the local and throwaway Cloud test contexts to the user's
authorized environments. Do not mutate the tracked integration-test config or
retain credentials in logs. Record every actual command, exit code, key result,
and cleanup outcome in private evidence, then use sanitized context/path
placeholders in the PR description as #1472 did.

- [x] 6.1 Start/verify the repository compose Grafana and the authorized throwaway Cloud context; verify `/api/health`, discovery of timeintervals/templategroups, selected versions, and no pre-existing task-prefixed objects. Confirm no mutation testing has targeted another context.
- [x] 6.2 On **each** context, run the following time-interval sequence and verify every listed outcome: `list -o json`; `create -f interval.yaml` (new title, native name empty); repeat create (conflict); `get <title> -o yaml > interval-current.yaml`; edit a time range; `update <title> -f interval-current.yaml` (success); repeat with the stale file (conflict); `get <title> -o yaml | update <title> -f -` (success); generic `resources get timeintervals.notifications.alerting.grafana.app/<returned-name> -o yaml` (same manifest); `export --format hcl` and `export <title> --format hcl` (provisioning HCL); `delete <title> --force` then get (deleted/not found). Record default/preferred and explicit v0alpha1 reads where served.
- [x] 6.3 On **each** context, run the corresponding template sequence with a unique valid `spec.content` definition: list; unnamed create; repeat create conflict; get/save; edit content; update success; stale-file conflict; `get | update -f -`; generic `resources get templategroups.notifications.alerting.grafana.app/<returned-name>` equality; forced delete; not-found get. Verify title/name output, `--kind grafana`, no export leaf, and both API versions where served. Preserve another task-owned object as an unchanged control during updates.
- [x] 6.4 Check rename and read-only behavior on each context for both kinds: fetch a fresh task-owned object, edit its spec title, update using its old title, verify the new title/API name and old-title absence, and record the unchanged local file and stale/destination-conflict behavior. Separately push an edited title with the old metadata name retained and verify the desired identity is created/updated while the original remains unchanged, including repeated push and an empty target. Verify imported list/get/ambiguous-title/read-only mutation behavior using disposable imports when feasible, otherwise retain recording-transport coverage and report the unavailable live-import setup explicitly. Check legacy mute-timings/templates read commands still work.
- [x] 6.5 Verify default preview for both kinds on create, existing update, and delete: run generic dry-run, confirm skipped/unverified output, compare before/after manifests, and confirm the mutation recording tests recorded zero writes. Do not assert server dry-run support to bypass the guard.
- [x] 6.6 Create both unique objects on local only; `resources pull timeintervals.notifications.alerting.grafana.app/<interval-name> templategroups.notifications.alerting.grafana.app/<group-name> -p <scratch-dir>`; confirm the titles are absent on the throwaway target; `resources push -p <scratch-dir>` there (two successful creates); repeat push (same identities, no duplicates), edit source manifests and push (target updates using its versions), then compare target get/spec with local source. Separately test a saved dedicated-get manifest pushed directly with source metadata. Verify logical matching across the historical/current template encodings in the recording-server tests even if no live 12.x fixture is available.
- [x] 6.7 Clean up every task-owned object on both contexts, including rename-path/new-identity objects; verify get is not found and lists contain no task prefix. Re-run cleanup even after a failed step; report any remaining object explicitly.
- [x] 6.8 Prepare the single PR description with the concrete behavior, `#1509` reference, `Part of #1473` wording, all actual E2E command/results, failed/skipped checks, and cleanup; inspect the full outgoing diff, metadata, generated files, and reachable commits for public appropriateness before publication, then follow the existing authorization boundary. Never use a closing keyword for #1473.

## 7. Review fixes

The original completed tasks above retain their implementation and verification
history. The review fixes below supersede imported-input failures with shared
read-only skips and require a fresh validation pass. Prior runtime evidence does
not establish the retry wrapper or the new skip/refusal behavior.

- [x] 7.1 Remove redundant template update kind checks; resolve the Grafana-managed group and preserve kind-change rejection through its API-name validation.
- [x] 7.2 Correct list field-selection hints and get/list costs; clarify that provisioning export covers Grafana-managed intervals only and regenerate references.
- [x] 7.3 Share read-only classification across both kinds, count pull/push/delete skips by reason, reject edit before the editor opens, and fail writable local matches against read-only destinations; verify request-free rejection/skip paths.
- [x] 7.4 Tolerate unknown template kinds in server inventories and logical matching while preserving strict local manifests, raw-kind table output, known-kind filtering, and unfiltered ambiguity candidates.
- [x] 7.5 Reject unnamed writable native delete files before any client request for that file, with dedicated-command guidance; preserve named-file deletion and read-only skip precedence.
- [x] 7.6 Add optional source paths to push/delete failure entries and diagnostics, retaining mutation batch schema version 1 and omission when unavailable.
- [x] 7.7 Add native dynamic-client Conflict-only retry inside the dry-run guard for Create/Update/Delete; verify contention, cancellation, exhaustion, unchanged reads, non-Conflict errors, and unaffected adapters without an alerting serial phase.
- [x] 7.8 Reconcile README, architecture, and OpenSpec with final review semantics; run strict OpenSpec validation and the doc-maintenance structural checks.
- [x] 7.9 Run renewed quality gates and live validation on the authorized throwaway environment, including default-concurrency push/delete, read-only skips and edit refusal, failure source paths, hints, and cleanup. Keep imported-destination writes, file-provenance runtime, and Grafana 12.x coverage gaps explicit. Update the private PR draft only from verified results and follow existing publication authorization.

## 8. Review 2 fixes

- [x] 8.1 Use plain read-only guidance for built-in, external-import, and file-provisioned resources; give dedicated read-only manifests exit code 1 and remove unreachable checks. Verify source-specific errors and validation before client loading. Clarify that template update accepts Grafana-managed manifests and selects the Grafana-managed group by argument title; an imported-only title returns not found.
- [x] 8.2 Share the guarded retry factory between pusher and deleter and exercise that factory in tests. Add real-command regressions for read-only pull exclusion and push/delete reason propagation in text, JSON, and agent mode; verify removing the wrappers, pull filter, or result reason assignments fails the relevant tests. Correct the pull diagram order and exact validation-error example.
- [x] 8.3 Add optional top-level `skipped_reasons` to mutation batches and artifact receipts, keeping schema version 1 and total skipped count; render human reasons from the result and remove duplicate codec registration. Document the contract, run targeted/race/full checks, and verify JSON/agent reason distinctions, read-only messages/exits, unchanged built-ins, and cleanup live on the authorized throwaway target, with read-only inventory/pull checks on the development target.


## 9. PR review corrections

These corrections supersede the earlier always-skipped pull and broad dynamic-retry policies. The original runtime/export checks above remain historical evidence; native provisioning export was removed before this feature shipped.

- [x] 9.1 Scope Conflict retries to TimeInterval/TemplateGroup in both API versions, pass all other kinds through once, preserve resourceVersion and the guard, and verify the scope matrix and contention regressions.
- [x] 9.2 Remove the duplicate native time-interval export command and its metadata/reference; direct help to the existing mute-timings export without changing that command.
- [x] 9.3 Include read-only manifests in pull by default with optional --skip-read-only filtering; preserve write-side skips, verify both modes in real commands and live audit/round-trip checks.
- [x] 9.4 Align both families' ls/rm aliases and get table/wide codecs with real command regressions.
- [x] 9.5 Make create preparation a pure copy/strip operation, keep validation at the write boundary, and remove redundant matching validation while preserving local failure reporting and remote tolerance.
- [x] 9.6 Use source paths for deterministic malformed-file collection keys and retain source-less inputs independently; verify repeated-file deduplication and distinct files, update docs, and complete required gates.
