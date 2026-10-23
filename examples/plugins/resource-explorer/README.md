# Resource explorer — generic go-plugin PoC

This implements proposal 2.a from the extensions design: go-plugin with a generic
SDK. The UI is reused from the typed RPC PoC (#1568); data fetching uses the same
command arguments and extension-owned response models as the subprocess SDK
PoC (#1563).

```text
resource explorer → Execute RPC → parent gcx → child gcx → Grafana
```

## Build and run

From the worktree root:

```sh
sh examples/plugins/resource-explorer/setup-local.sh
export GCX_AGENT_MODE=false
bin/gcx plugins run --demo ./bin/resource-explorer
bin/gcx plugins run --demo ./bin/resource-explorer -- --no-tui
bin/gcx plugins run --demo ./bin/resource-explorer -- --no-tui folders
bin/gcx --context <context> plugins run ./bin/resource-explorer
bin/gcx --context <context> plugins run ./bin/resource-explorer -- --no-tui dashboards
```

The offline demo also crosses RPC and starts a subprocess on every callback.
Instead of gcx, it executes the explorer binary's `--fixture` mode, which returns
CLI-shaped JSON for dashboards and folders in one API group, including objects
without `kind`. No Grafana access is needed.

Enter opens objects; Escape returns; `/` filters; Tab switches preview;
`o` opens the selected object; `q` quits. The TUI uses `/dev/tty`, and requires
interactive stdin/stdout with agent mode disabled. Windows supports `--no-tui`
only. Finite JSON output and application diagnostics return through RunResponse;
ordinary stderr is forwarded, while ordinary stdout after SDK startup is discarded.

## Contract and tradeoffs

`source.go` builds `resources list-types` and `resources get` argument vectors,
then decodes their CLI JSON. The SDK exposes only generic execution and JSON
helpers. There are no typed resource RPCs or direct backend calls from the plugin.
The explorer requests `--limit 0` and retains integer precision and missing kinds.

The host starts a fresh gcx process for every live request. Authentication,
configuration loading and network-client setup are repeated. The generic RPC
layer adds transport/versioning/lifecycle machinery without removing those costs.
Context, config, credentials and agent-mode environment follow normal gcx rules.
Use `GCX_AGENT_MODE=false` for the same human-mode JSON shapes as the subprocess
explorer. The executor itself can invoke any gcx command, although this explorer
only constructs read operations.

This PoC has no installation/upgrade flow, typed SDK, shared Grafana clients,
streaming output or stable SDK promise. See the [SDK README](../../../experimental/pluginsdk/README.md)
for cancellation, output, message limits and trust boundaries.

## Validation

```sh
go test -race ./internal/pluginhost ./cmd/gcx/plugins
(cd experimental/pluginsdk && go test -race ./...)
(cd examples/plugins/resource-explorer && go test -race ./...)
```

Tests cover generic RPC arguments/stdin/output/status, protocol mismatch,
plugin crashes and cancellation; host tests cover separate subprocesses,
configuration forwarding and cancellation. Explorer tests cover command
construction, resource/schema association, partial failures and absent kinds.

Read-only live validation against the development context discovered 275 resource
types, loaded schemas and listed Alertmanager datasource objects with previews.
Terminal navigation and clean exit were checked with agent mode disabled.
