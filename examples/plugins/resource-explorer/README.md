# Resource explorer — typed go-plugin PoC

This is the RPC counterpart to [the subprocess + generic SDK PoC](https://github.com/grafana/gcx/pull/1563).
The explorer UI comes from [PR #1544](https://github.com/grafana/gcx/pull/1544).
It runs in a separate Go plugin process and calls three typed host capabilities:
resource discovery, schema lookup and resource listing. No callback launches gcx
or constructs CLI arguments, and the plugin imports no gcx internal packages.

## Build and run

From the worktree root:

```sh
sh examples/plugins/resource-explorer/setup-local.sh
export GCX_AGENT_MODE=false
bin/gcx plugins run --demo ./bin/resource-explorer
```

For live data:

```sh
bin/gcx plugins run --context <context> ./bin/resource-explorer
bin/gcx plugins run --context <context> ./bin/resource-explorer -- dashboards
```

`--config /absolute/path/config.yaml` is also supported on `plugins run`; the host
uses the normal configuration, context and authentication loaders. Host flags go
before `--`. Plugin flags and selectors go after it, with plugin flags first:

```sh
bin/gcx plugins run --demo ./bin/resource-explorer -- --no-tui dashboards
bin/gcx plugins run --demo ./bin/resource-explorer -- --help
```

`--no-tui` prints discovered types, schemas, and the first type's objects. It is
not an export of every discovered type. `--demo` serves fixtures in the host over
the same RPC path without contacting a backend. The fixtures include two types
in one API group and objects without `kind` and with large integer metadata.

Keys: enter lists objects; esc goes back or clears a filter; `/` filters; tab
moves focus to the preview; `o` opens an available Grafana link; `q` quits.
Re-run setup after changes. No installation manifest or global plugin store is
used: this experiment executes an explicitly selected local binary.

## Boundary and responsibilities

```text
gcx plugins run                     resource-explorer process
  load selected config/auth           implement SDK App.Run
  start plugin + verify protocol  --> build and own terminal UI
  serve typed host callbacks      <-- ListTypes / GetSchemas / ListResources
  discovery + schemas + puller     --> typed responses + dynamic resource JSON
  cancel calls and clean up           return application result
```

The host creates a discovery registry, schema fetcher and resource puller for
this run, and reuses them for callbacks. The puller handles native resources and
registered provider adapters, pagination and operation summaries. Ordinary gcx
invocations retain their independent lifecycle. Nothing here introduces shared
state across invocations or proves a performance improvement.

`source.go` is the plugin's typed adapter. The [SDK](../../../experimental/pluginsdk/README.md)
contains the protobuf contract, generated clients and lifecycle wiring. The host
implementation is in `internal/pluginhost`. Descriptors keep full group/version/
plural identity, and metadata is typed; resource bodies and schemas remain JSON
because their shapes are discovered at runtime. Partial results remain visible
with warnings rather than becoming apparently complete empty lists.

Third-party plugins run with the user's permissions and inherited credentials.
Grafana and gcx maintainers do not review or support their contents and are not
responsible for consequences of running them. Use at your own risk. The host
prints this disclosure before execution. Typed callbacks and mTLS do not sandbox
plugin code or prevent direct access to files and the network.

## Tradeoffs exposed by this PoC

- Authors call typed methods without knowing CLI flags or decoding the CLI's
  nested discovery response. They still decode dynamic resource bodies.
- gcx owns a second public contract: protobuf messages, host methods, protocol
  compatibility and SDK releases. New CLI commands are not automatically callable.
- Callbacks reuse clients within this run and support independent cancellation.
  There are no host-driven notifications or application lifecycle callbacks beyond
  Run/cancellation; the explorer does not need them.
- RPC requires startup negotiation, broker setup, generated code and process
  cleanup. It also complicates terminal I/O: this example opens `/dev/tty`, and
  the host restores terminal modes on cancellation or plugin failure.
- Application exit status is explicit in `RunResponse`; a crash is a transport
  failure. Host cancellation exits 5. This differs from raw child exit forwarding
  in the subprocess PoC.

## Scope and validation

Only read-only resource capabilities are implemented. No generic executor,
profile API, remote installation, update management, manifest, telemetry design,
capability negotiation or stable SDK promise is included. Live configuration is
loaded before launching the plugin; use `--demo` for offline help and exploration.
Headless responses are buffered and have a 64 MiB RPC message limit. Interactive
Windows support is unimplemented; `--no-tui` is available but Windows remains
unvalidated. `wasip1` does not register `plugins`.

```sh
# From this example directory:
go test -race ./...
go vet ./...
mise exec -- golangci-lint run -c ../../../.golangci.yaml ./...
```

The host uses the repository checks; the SDK is a separate module with its own
real-process tests for callbacks, partial responses, permission errors, crashes,
protocol incompatibility and cancellation. Host HTTP tests cover authentication,
namespace/type mapping, pagination, missing-kind objects and failure reporting.
The copied UI tests and typed adapter tests run in the example module.
