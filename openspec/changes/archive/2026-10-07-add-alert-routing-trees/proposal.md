# Proposal

## Why

gcx cannot manage named notification routing trees. `gcx alert notification-policies` only reads and replaces the default tree through the provisioning API, and `gcx resources` hides the whole `notifications.alerting.grafana.app` group from discovery. Grafana has served named trees natively since 13.1 (always on from 13.2), so users who rely on them have no CLI path. This change is the first slice of [RFC 001](../../../../docs/rfcs/001-alerting-provider-refactor.md): native routing-tree CRUD plus a reusable native-resource binding that later slices build on.

## What Changes

- New command group `gcx alert routing-trees` with `list`, `get`, `create`, `update`, and `delete`, operating on native `RoutingTree` manifests through the Kubernetes-compatible API. The API version comes from server discovery (`v1beta1` on 13.0+, `v0alpha1` on 12.x).
- `gcx resources` can see `routingtrees`. The discovery exclusion of `notifications.alerting.grafana.app` becomes a per-resource allowlist; receivers, template groups, time intervals, and inhibition rules stay hidden.
- New shared native-resource binding (`internal/providers/native`) that dedicated commands use to resolve a descriptor and a dynamic client after validation. It takes an injectable config loader and discovery source and has no direct CLI imports, so other agent-facing surfaces can reuse it.
- `CONSTITUTION.md`: the dashboards-only exception for direct dynamic-client use is replaced by a rule that providers using native resources go through the shared binding.
- New developer glossary under `docs/glossary/` (map plus `alerting.md`), linked from `openspec/config.yaml` `context:`. RFC and doc-maintenance terminology rules change from "keep terms in the RFC" to "proposed terms stay in the RFC; shipped terms move to the glossary".
- No legacy fallback. On targets where named trees are unavailable, the server's response is surfaced unchanged.
- `gcx alert notification-policies` is unchanged; its deprecation is a later slice.

## Capabilities

### New Capabilities
- `alerting/routing-trees`: managing notification routing trees through gcx, both via the dedicated `gcx alert routing-trees` commands and via the generic `gcx resources` commands.

### Modified Capabilities
<!-- None: no specs exist yet. -->

## Impact

- **Code:** `internal/providers/alert/` (new command group and provider wiring), new `internal/providers/native/`, `internal/resources/discovery/registry.go` (filter granularity).
- **Docs:** `CONSTITUTION.md`, `docs/glossary/` (new), `openspec/config.yaml`, `docs/rfcs/README.md`, `docs/reference/doc-maintenance.md`, `docs/architecture/project-structure.md`, `CLAUDE.md` (package map and documentation map), generated CLI reference.
- **Server compatibility:** Grafana 12+. Named-tree operations require 13.1+ or 12.4–13.0 with `alertingMultiplePolicies` enabled; default-tree operations work on all 12+ releases.
- **Prerequisite:** a separate change fixes `gcx resources` selector parsing for `resource.group` with multi-dot groups (e.g. `routingtrees.notifications.alerting.grafana.app`). This change's verification uses that form.
- **Follow-ups (not in this change):** dashboards migrate to the native binding; legacy fallback (needs gated registration); other notification resources; rule definitions; `notification-policies` deprecation.
