# Design

## Context

See `proposal.md` (Why) for motivation and `specs/alerting/routing-trees/spec.md` for the required behavior. Current state that shapes the approach:

- **Alert provider.** `internal/providers/alert/provider.go` is a custom provider. It returns no typed registrations and builds plain cobra groups that call the provisioning REST client (`provisioning_client.go`). Nothing in the package uses the native notifications API.
- **Discovery exclusion.** `internal/resources/discovery/registry.go:26-34` lists `notifications.alerting.grafana.app` in `ignoredResourceGroups` with a TODO. `FilterDiscoveryResults` (`registry.go:260-300`) drops ignored groups twice: from the `APIGroup` list (`:266-269`) and from the resource lists (`:280-282`). Preferred versions come from the `APIGroup` entries (`registry_index.go:229-242`).
- **Native access today.** Dashboards access the native API through hand-rolled code. `descriptor.Resolve` builds a selector string and calls `discovery.NewDefaultRegistry` (`internal/providers/dashboards/descriptor/descriptor.go:23-60`). `mutationDeps.resolve` combines it with `dynamic.NewDefaultNamespacedClient` and a test override (`internal/providers/dashboards/crud.go:41-76`). `readManifest` reads `os.Stdin` directly (`crud.go:565-583`).
- **Constitution.** `CONSTITUTION.md:44` names the dashboards provider as "the one documented exception" allowed to call the dynamic client directly.
- **Router.** The `gcx resources` router sends a GVK to an adapter whenever one is registered, and to the dynamic client otherwise (`internal/resources/adapter/router.go:91-101`, `internal/resources/remote/remote.go:27-37`).
- **Server contract** (Grafana source).
  - Group `notifications.alerting.grafana.app`, kind `RoutingTree`, resource `routingtrees`, namespaced.
  - `v1beta1` is preferred from 13.0. `v0alpha1` is still served on 13.x and is the only version on 12.x. The specs are identical at HEAD.
  - The default tree is `user-defined`. `default` is accepted as an input alias on newer servers.
  - Update requires an exact `resourceVersion`; an empty one conflicts.
  - DELETE resets the default tree and removes a named tree.
  - Named trees return 405 on 12.3 and earlier, and 501 on 12.4–13.0 when `alertingMultiplePolicies` is off. They are GA in 13.1 and always on from 13.2.

## Goals / Non-Goals

**Goals:**
- One native-resource access path that dedicated commands share. Dashboards can adopt it later without changing it.
- Keep the access path free of CLI dependencies, so another agent-facing surface (for example an MCP server) can reuse it. That reuse may be as a library or by running gcx commands in process as generated tools.

**Non-Goals:**
- Migrating dashboards to the binding (follow-up PR).
- Any legacy or provisioning fallback, or a gated adapter registration for fallback (later slice).
- Taking native resources into `adapter.NewProvider` or a unified native/REST declaration model.
- Fixing `gcx resources` selector parsing (separate prerequisite change).
- Typed Go structs for `RoutingTree`. Manifests stay unstructured end to end.

## Decisions

### D1. Shared native binding in `internal/providers/native`

```go
package native

// Client is the dynamic-client subset the router already uses for native resources;
// *dynamic.NamespacedClient satisfies it.
type Client = adapter.DynamicClient

type Config struct {
    Group    string // "notifications.alerting.grafana.app"
    Resource string // "routingtrees"
}

type LoadOptions struct {
    APIVersion string // "group/version" or "version"; empty = server preferred
}

type Access struct {
    Client     Client
    Descriptor resources.Descriptor
    Config     config.NamespacedRESTConfig // same snapshot, for helper queries and deep links
}

func Bind(loader providers.GrafanaConfigLoader, cfg Config, opts ...BindOption) Binding // no I/O
func (b Binding) Load(ctx context.Context, o LoadOptions) (Access, error)
func Fixed(a Access) Binding // test seam: Load returns a without I/O

func WithRegistry(f func(ctx context.Context, cfg config.NamespacedRESTConfig) (*discovery.Registry, error)) BindOption
```

`Load` runs in this order:
1. Load a fresh config snapshot.
2. Build a registry: `discovery.NewDefaultRegistry` unless `WithRegistry` overrides it.
3. Resolve the descriptor.
4. Build the client with `dynamic.NewDefaultNamespacedClient`.

Commands call `Load` only after validation and any confirmation. Nothing is cached between `Load` calls.

**Why this package:**
- It can't live in `adapter`: `discovery` imports `adapter`, and `adapter` can't import `providers`.
- Putting it in `internal/providers/resource.go` would pull discovery and client-go dynamic into the `providers` package, which every provider imports.

**Alternatives considered:**
- *`adapter.Resource[RoutingTree]`.* An adapter registration takes over the GVK in the router and pins one version. It also round-trips manifests through a Go struct, which drops unknown fields, and duplicates the server schema. The RFC and the dashboards design both rule this out.
- *Native declarations accepted by `adapter.NewProvider`.* `Declaration.registration` is unexported and must return exactly one `Registration`. A native declaration would return none and carry no behavior in this change. Revisit only if a fallback registration or tool generation needs a per-provider resource list.
- *Keep hand-rolling per provider.* This is what the constitution rule replaces.

### D2. Descriptor resolution and version precedence

`Load` builds a `resources.Selector` directly, from `PartialGVK{Group: cfg.Group, Version: v, Resource: cfg.Resource}`, and calls `MakeFilters` with `PreferredVersionOnly`. It never formats a selector string, which avoids the string-parsing bugs in `descriptor.Resolve` for `version`-only inputs and multi-dot groups.

| Command | Version source | Rejected before any request |
|---|---|---|
| `list`, `get`, `delete` | `--api-version`, else the server's preferred version | `--api-version` group ≠ `cfg.Group` |
| `create`, `update` | Manifest `apiVersion` | Manifest group/kind mismatch; `--api-version` set and ≠ manifest `apiVersion` |

When the manifest decides, the command passes the manifest's version into `LoadOptions.APIVersion`.

### D3. Reuse constraints for agent-facing surfaces

These constraints let the binding and the commands be reused, as a library or as generated tools (one tool per command), without changing this slice:
- **No CLI imports.** `native` imports no cobra, `cmdio`, terminal, or prompt packages.
- **Injectable loader.** `Bind` takes the `GrafanaConfigLoader` interface. Commands get the loader from the provider's command factory, never construct one inside a leaf.
- **Injectable discovery.** `WithRegistry` lets a long-running multi-tenant process supply its own cache instead of the CLI's on-disk cache.
- **Commands depend only on their inputs.** A command's output depends only on args, flags, injected stdin and loader. It writes only through `cmd.OutOrStdout()`/`cmd.ErrOrStderr()` and keeps no package-level mutable state.
- **Standard flags only.** `-f/--filename` (`-` = `cmd.InOrStdin()`), `-o`, `--api-version`, `--force`.

Dashboards' `readManifest` reads `os.Stdin` directly, which breaks the injected-stdin rule. A shared `native.ReadManifest(filename string, stdin io.Reader)` (JSON or YAML into unstructured, with dashboards' 32 MiB stdin limit) replaces it for routing trees. Dashboards adopts it in its migration PR.

### D4. Per-resource discovery allowlist

`ignoredResourceGroups` stays for whole-group exclusions. Remove `notifications.alerting.grafana.app` from it and add a separate allowlist:

```go
// partiallyExposedGroups lists groups whose resources are hidden unless named here.
var partiallyExposedGroups = map[string][]string{
    "notifications.alerting.grafana.app": {"routingtrees"},
}
```

`FilterDiscoveryResults` keeps the group's `APIGroup` entry, which carries the preferred version. Within that group's resource lists it drops every resource not in the allowlist. New resources Grafana adds to the group stay hidden until a slice opts them in.

**Alternative considered:** a denylist of `receivers`, `templategroups`, `timeintervals` and `inhibitionrules`. Rejected because any new resource would appear in `pull`/`push` before anyone checked its secret handling.

### D5. Command group in the alert provider

- **Provider wiring.** The alert provider stays custom. `provider.go` adds `routingTreesCommands(loader)` next to the existing groups, with alias `routing-tree`. The command factory calls `native.Bind` once and passes the `Binding` to every leaf. The `TypedRegistrations` comment (`provider.go:66-69`, "must not mimic adapter CRUD") is reworded: alert commands may manage native resources through the binding but register no adapters.
- **Command structure.** Leaves follow dashboards' mutation commands: options struct with `setup`/`Validate`, then `Load`, then the client call, then a `cmdio` result.
- **`update`.** Validates `metadata.resourceVersion` is non-empty and `<name>` equals `metadata.name` before `Load`. The dynamic client does not enforce either.
- **`delete`.** Confirms through `providers.ConfirmDestructive` unless `--force`, and only then calls `Load`. The result receipt says `reset` when the name is `user-defined` or `default`, and `deleted` otherwise. This is wording only; the request is the same.
- **`list` table columns.** `NAME`, `RECEIVER` (`spec.defaults.receiver`), `ROUTES` (top-level count). `-o wide` adds `PROVENANCE` (`grafana.com/provenance` annotation). Missing fields render empty rather than failing, which tolerates 12.x `v0alpha1` differences.
- **`get`.** Defaults to YAML output, matching dashboards. JSON and YAML always emit the server manifest unchanged.

### D6. Constitution rule

Replace the exception at `CONSTITUTION.md:44` with this rule:

> **Native resources go through the shared native binding.** Provider commands that manage a Kubernetes-compatible resource discovered from the server use `internal/providers/native`. They never register an adapter for a discovered GVK, and never construct discovery registries or dynamic clients directly.

Dashboards temporarily violates the new rule until its migration PR. The rule text names that PR as pending, so the gap is explicit rather than a silent second exception.

### D7. Glossary

- `docs/glossary/README.md` is a short map: one line per context file, plus how the contexts relate.
- `docs/glossary/alerting.md` defines routing tree, default tree, named tree, notification policy, receiver and integration. It uses bold terms, one- or two-sentence definitions and `_Avoid_` lines, with no implementation details.
- `openspec/config.yaml` `context:` gets three lines telling agents to read the map, then only the context files a change touches, and to add new terms there.
- `docs/rfcs/README.md` and `docs/reference/doc-maintenance.md`: proposed terms stay in the RFC, and shipped terms move to the glossary.
- RFC 001's Terminology section links to `alerting.md` for the terms this slice ships.

### D8. Verification

- **Unit tests** (table-driven):
  - `native` resolution, against `discovery.NewCachedRegistry` with a fake client: preferred version, explicit version, group mismatch, and the resource missing from discovery.
  - Allowlist filtering in `FilterDiscoveryResults`: the group entry is kept, `routingtrees` is kept, the group's other resources are dropped.
  - Command behavior, through `native.Fixed` with a fake `Client`: every validation path in the spec, the delete receipt wording, and stdin input through `cmd.SetIn`.
  - A `httptest` server standing in for the native API, for one end-to-end path per verb, including 409 and 501 passthrough.
- **End-to-end check, run by an agent against the local `docker-compose.yml` stack (Grafana 13.2).** The steps are listed in `tasks.md`, so an agent can run them with no human input and report each expected result.

## Risks / Trade-offs

- **12.x `v0alpha1` schema may differ from HEAD.** Only HEAD was compared. → Manifests pass through unstructured, and table columns tolerate missing fields. Document 12.x verification as not yet done in the RFC's version matrix.
- **The discovery cache can hold a stale preferred version for up to 10 minutes after a Grafana upgrade.** → Existing behavior for all native resources; `--api-version` overrides it.
- **Dashboards violates the new constitution rule until its migration lands.** → The rule text names the pending migration, and the follow-up PR is next in the RFC rollout.
- **Delete receipt wording relies on gcx knowing the default tree's names.** → It's wording only. If Grafana adds another alias the request still works; only the receipt would say `deleted`.
- **The prerequisite parser fix may not land first.** → Task 5.0 blocks the end-to-end check until it is merged; the rest of the change does not depend on it.

## Migration Plan

The change is additive. Nothing existing changes behavior: `notification-policies` and the other alert commands are untouched, and the discovery change only exposes `routingtrees`. Rollback is a revert.
