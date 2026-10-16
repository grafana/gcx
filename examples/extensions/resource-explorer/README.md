# Resource explorer — RFC subprocess PoC with a generic SDK

A read-only terminal explorer for resource types, schemas and objects. The UI
comes from the resource-browser proposal in [PR #1544](https://github.com/grafana/gcx/pull/1544).
It is now an ordinary executable that reaches Grafana through generic SDK calls
to the invoking gcx binary. It imports no gcx internals and uses no RPC.

## Build and run

From the worktree root:

```sh
export GCX_AGENT_MODE=false GCX_TELEMETRY=disabled
go build -buildvcs=false -o bin/gcx ./cmd/gcx
sh examples/extensions/resource-explorer/setup-local.sh
export GCX_EXTENSIONS_DIR="$PWD/.resource-explorer-local/extensions"
bin/gcx ext resource-explorer --demo
```

The setup script builds the extension, commits its manifest into a small local
git source, and installs the built binary into an isolated extension store.
It does not commit the PoC branch or change the normal extension store.
Re-run it after editing the extension; quit running extensions first.

For real data, select an existing context:

```sh
bin/gcx --context <context> ext resource-explorer
bin/gcx --context <context> ext resource-explorer dashboards
```

Use `GCX_CONFIG=/absolute/path/config.yaml` for an explicit configuration file.
gcx flags go before `ext`; arguments after `resource-explorer` are forwarded to
the extension. Extension flags go before positional selectors.

Keys: enter lists objects; esc goes back or clears the filter; `/` filters;
tab moves focus to the preview; `o` opens an available Grafana link; `q` quits.

For an offline JSON smoke check without a terminal:

```sh
bin/gcx ext resource-explorer --demo --no-tui dashboards
```

`--no-tui` reports discovered types, schemas, and objects of the first type.
It is not an export of every selected type. Live output contains full resource
bodies and may be large. No backend is contacted in `--demo` mode.

## SDK boundary

`source.go` calls `Executor.Execute` for `resources list-types` and
`resources get <plural.version.group> --output json --limit 0`. The SDK owns
process execution, cancellation and result/error handling. This extension owns
resource discovery, response decoding, partial-result handling and the UI.

Resource types and schemas are keyed by the full selector, so multiple resources
in one API group and identical plurals in different groups remain distinct.
Objects without `kind` remain browsable, and integer metadata is preserved.
Partial results (exit 4) remain visible alongside a warning.

The demo uses a fixture subprocess through the same SDK. It includes two types
in one API group and objects without `kind`. The original UI tests are retained;
adapter tests cover the subprocess boundary, selectors and partial results.

## PoC scope

This demonstrates the execution/SDK approach, not all of RFC 003:

- `gcx ext install <local-git-checkout> --yes`, `gcx ext list` and raw dispatch
  are implemented. The manifest uses `schemaVersion: 1` and local `path` platform
  rows. Only declared platforms are accepted; wildcards support local builds.
- The committed manifest is read at a fixed HEAD commit. The local binary may
  be an untracked build output and is copied into the installation. No build or
  install hooks run. Paths are constrained to the source checkout.
- Installation uses an exclusive store lock and atomically replaces the index;
  execution holds a shared lock until the child exits. This deliberately blocks
  installs while any extension in this PoC store runs. Previous payload directories
  are retained; garbage collection is not implemented.
- Release versions are checked against inclusive min/max bounds at install and
  run. Local development builds without a release version skip that comparison.
- Remote repositories/artifacts, scripts, updates, uninstall, installed-name help,
  extension-name telemetry and the full RFC failure/concurrency validation are
  not implemented. Reserved management names cannot be installed.
- Unix cancellation forwards an interrupt, with a five-second cleanup timeout;
  child exit status is preserved. Windows currently kills the child on cancellation
  and remains unvalidated. `wasip1` does not register `ext`.
- Extensions are not sandboxed. They run with the user's permissions and inherited
  credentials. Grafana/gcx maintainers do not review or support third-party contents
  or accept responsibility for consequences of running them. The host discloses
  this at installation and execution. The SDK does not change the trust boundary.

## Validate

```sh
go test -race ./...
go vet ./...
mise exec -- golangci-lint run --disable gomoddirectives -c ../../../.golangci.yaml ./...
```

Run the commands above from this example's directory. The SDK is a separate module
at `experimental/extensionsdk` and has its own tests. The worktree's normal
`mise run lint` checks host code; lint the example separately because it is a
standalone module. The example lint command permits its intentional local SDK
module replacement; other lint checks remain enabled.
