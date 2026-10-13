# Tasks

## 1. Shared native binding

- [x] 1.1 Create `internal/providers/native` with `Config`, `LoadOptions`, `Access`, `Client` (alias of `adapter.DynamicClient`), `Bind`, `Binding.Load`, `Fixed`, and `WithRegistry` per design D1–D2; verify `go list -f '{{join .Imports "\n"}}' ./internal/providers/native` lists no cobra, `cmdio`, or terminal packages as direct imports, and `TestNoCLIImports` enforces it (transitive dependencies still include them through `internal/config` and `internal/resources/adapter`; that cleanup is a separate follow-up)
- [x] 1.2 Implement descriptor resolution from a constructed `PartialGVK` selector with `PreferredVersionOnly`, accepting `group/version` or `version` and rejecting a mismatched group; verify table-driven tests against `discovery.NewCachedRegistry` with a fake client cover preferred version, explicit version, version-only input, group mismatch, and resource not served
- [x] 1.3 Add `native.ReadManifest(filename string, stdin io.Reader)` (JSON/YAML into unstructured, 32 MiB stdin limit, `-` reads the given reader); verify tests cover file, stdin via injected reader, empty filename, and invalid content
- [x] 1.4 Document the native binding as a pattern in `docs/architecture/patterns.md`, and add `internal/providers/native/` to the package map in `docs/architecture/project-structure.md`; verify both docs reference `native.Bind` and the package path

## 2. Discovery allowlist

- [x] 2.1 Replace the `notifications.alerting.grafana.app` entry in `ignoredResourceGroups` with a `partiallyExposedGroups` allowlist (`routingtrees` only) and apply it in `FilterDiscoveryResults` while keeping the group's `APIGroup` entry; verify table-driven tests show the group entry kept, `routingtrees` kept, and `receivers`, `templategroups`, `timeintervals`, `inhibitionrules` dropped
- [x] 2.2 Verify through a registry-level test that `LookupPartialGVK` resolves `routingtrees` to the group's preferred version and that the group's other resources are not resolvable

## 3. `gcx alert routing-trees` commands

- [x] 3.1 Add the `routing-trees` group (alias `routing-tree`) to the alert provider, binding `native.Bind` once in the command factory, and reword the `TypedRegistrations` comment in `internal/providers/alert/provider.go`; verify `bin/gcx alert routing-trees --help` lists the five verbs
- [x] 3.2 Implement `list` with table columns `NAME`, `RECEIVER`, `ROUTES` and `-o wide` adding `PROVENANCE`, plus JSON/YAML manifest output; verify tests through `native.Fixed` cover table, wide, JSON output, missing spec fields rendering empty, and `--api-version` passing through to `Load`
- [x] 3.3 Implement `get <name>` defaulting to YAML and emitting the server manifest unchanged; verify tests cover found, not found (server error surfaced, non-zero exit), and names passed through without aliasing
- [x] 3.4 Implement `create -f`: version from manifest `apiVersion`, rejecting group/kind mismatch and a conflicting `--api-version` before `Load`; verify tests cover success, stdin input via `cmd.SetIn`, 409 passthrough, and each pre-request validation error exiting with usage status
- [x] 3.5 Implement `update <name> -f`: reject empty `metadata.resourceVersion` and `<name>` ≠ `metadata.name` before `Load`, plus the same version rules as `create`; verify tests cover success, 409 stale-version passthrough, and each pre-request validation error
- [x] 3.6 Implement `delete <name>` with `--force`/`providers.ConfirmDestructive` before `Load`, and a `cmdio` receipt reporting `reset` for `user-defined`/`default` and `deleted` otherwise; verify tests cover forced delete, reset wording, declined prompt (cancelled status, no request), and agent mode without `--force` failing with an actionable error
- [x] 3.7 Add an `httptest` native-API test that runs one path per verb end to end, including 403 (auth-failure status) and 501 (named trees unsupported, single request, no other calls); verify `go test ./internal/providers/alert/...` passes
- [x] 3.8 Regenerate CLI reference with `GCX_AGENT_MODE=false mise run reference` and add `routing-trees` usage to `claude-plugin/skills/gcx-observability/references/phases-4-7-alerting.md`; verify `mise run reference-drift` is clean and `go test ./cmd/gcx/root/ -run TestSkillsGcxInvocationsMatchCommandTree` passes

## 4. Constitution, glossary, and documentation rules

- [x] 4.1 Replace the dashboards exception at `CONSTITUTION.md:44` with the native-binding rule from design D6, naming the pending dashboards migration; verify the old "one documented exception" text is gone
- [x] 4.2 Create `docs/glossary/README.md` (context map) and `docs/glossary/alerting.md` (routing tree, default tree, named tree, notification policy, receiver, integration) in the bold-term / short-definition / `_Avoid_` format, with no implementation details; verify every term used in `specs/alerting/routing-trees/spec.md` is defined
- [x] 4.3 Add a `context:` block to `openspec/config.yaml` pointing to `docs/glossary/README.md` and instructing agents to load only relevant context files; verify `openspec instructions proposal --change add-alert-routing-trees --json` returns that context
- [x] 4.4 Update the terminology rules in `docs/rfcs/README.md` and `docs/reference/doc-maintenance.md` (proposed terms in the RFC, shipped terms in `docs/glossary/`), link RFC 001's Terminology section to `docs/glossary/alerting.md`, and add `docs/glossary/` to the documentation map in `CLAUDE.md`; verify the three docs agree and `mise run docs` builds

## 5. Integration verification

- [x] 5.0 Confirm the selector-parser change (multi-dot `resource.group`, e.g. `routingtrees.notifications.alerting.grafana.app`) is merged into this branch's base; verify `bin/gcx resources get dashboards.dashboard.grafana.app` resolves against the local stack. If it is not merged, stop and rebase after it lands.
- [x] 5.1 Run `mise run gate` and `GCX_AGENT_MODE=false mise run all`; verify both succeed
- [x] 5.2 Agent-driven end-to-end check against the local stack, with no human steps. Record each command's exit code and the key output in the PR description:
  - Start: `docker-compose up -d`, poll `http://localhost:3000/api/health` until `database: ok`, `mise run build`, then use `G="bin/gcx --config testdata/integration-test-config.yaml"`. Run `delete team-a --force` and `delete team-b --force`, ignoring not-found, so a rerun starts clean.
  - `$G alert routing-trees list -o json` includes `user-defined`.
  - Create `team-a` and `team-b` from manifests with `apiVersion: notifications.alerting.grafana.app/v1beta1`, `kind: RoutingTree`, `spec.defaults.receiver: grafana-default-email`, `spec.routes: []`; both succeed, and creating `team-a` again exits non-zero with a conflict.
  - Save `get team-b -o yaml` and `get user-defined -o yaml`. Then `get team-a -o yaml > a.yaml`, set `spec.defaults.group_wait: 45s`, and run `update team-a -f a.yaml`; it succeeds, and the `spec` of `team-b` and `user-defined` is unchanged from the saved copies.
  - `update team-a -f a.yaml` again (stale version) exits non-zero with a conflict. The same manifest with `metadata.resourceVersion` removed exits 2.
  - `$G alert routing-trees get team-a -o yaml | $G alert routing-trees update team-a -f -` succeeds.
  - `$G resources get routingtrees.notifications.alerting.grafana.app/team-a -o yaml` matches `alert routing-trees get team-a -o yaml`.
  - `$G resources list-types` lists `routingtrees` and no `receivers`, `templategroups`, or `timeintervals`.
  - `$G alert routing-trees get team-b --api-version notifications.alerting.grafana.app/v0alpha1 -o yaml` returns a `v0alpha1` manifest.
  - `GCX_AGENT_MODE=true $G alert routing-trees delete team-a` fails naming `--force`. `delete team-a --force` reports `deleted`, and `get team-a` then exits non-zero with not found.
  - `delete user-defined --force` reports `reset`, and `get user-defined` still succeeds.
  - Clean up: `delete team-b --force`.
