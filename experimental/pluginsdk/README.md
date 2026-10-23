# Generic go-plugin SDK experiment

This standalone, unpublished module implements proposal 2.a: HashiCorp go-plugin
with gRPC, automatic mutual TLS and one generic command callback. It is the RPC
counterpart to the subprocess SDK in PR #1563, based on the typed RPC PoC in
PR #1568. The host uses a local module replacement.

```text
gcx host → plugin → Execute RPC → host → new gcx subprocess → Grafana
```

A plugin implements `App` and calls `Serve`:

```go
func main() { pluginsdk.Serve(app{}) }

type app struct{}
func (app) Run(ctx context.Context, req *pluginsdk.RunRequest,
    commands pluginsdk.Executor) (*pluginsdk.RunResponse, error) {
    result, err := commands.Execute(ctx, pluginsdk.CommandRequest{
        Args: []string{"resources", "get", "dashboards", "--output", "json"},
    })
    if err != nil { return nil, err }
    return &pluginsdk.RunResponse{
        Stdout: result.Stdout, Stderr: string(result.Stderr),
        ExitCode: int32(result.ExitCode),
    }, nil
}
```

`Execute` accepts an argument vector and optional stdin bytes. The host starts
its own gcx binary directly, without a shell, inheriting the environment and
forwarding the selected context/config file. Each call loads configuration,
resolves authentication and creates clients independently. No Grafana clients
or credentials are passed over RPC. This does not remove subprocess overhead.

Command failures return stdout, stderr and exit status, including partial output.
Startup and transport failures are errors. Calls accept independent contexts;
cancellation reaches the child gcx process. Grandchild process-tree management
is outside this PoC. `DecodeJSON` adds JSON output selection, checks command status
and decodes into an extension-owned destination. Command arguments and response
models belong to the extension. Agent mode is inherited, so CLI output shapes
remain subject to the same mode-dependent behaviour as the subprocess SDK.

The extension's output is separate from command output. `RunResponse` carries
finite stdout, stderr and application exit status; the host prints these after
Run completes. Ordinary plugin stderr is forwarded to host stderr. `SyncStdout`
is unset, so ordinary stdout after SDK startup is discarded. Both sides use
null go-plugin loggers. Interactive plugins must manage their terminal UI
separately from stdout. The host restores terminal state on exit.

`plugin.proto` is the wire contract; generated bindings are checked in. Startup
checks protocol version 1 with a distinct generic-command handshake cookie;
typed-PoC binaries are incompatible. Startup and callback-server startup each
have a ten-second timeout. Messages are limited to 64 MiB. Streaming, capability
negotiation, typed operations and remote installation are outside this PoC.
Plugins run with the user's permissions and inherited environment. mTLS protects
the connection; it does not sandbox third-party code. Generic execution can
invoke any gcx command, including mutations; normal command safety rules apply.

## Development

```sh
go test -race ./...
go vet ./...
mise exec -- golangci-lint run -c ../../.golangci.yaml ./...
```

Regenerate bindings with protoc 34.1, protoc-gen-go v1.36.11 and
protoc-gen-go-grpc v1.6.1 on PATH, then run `sh generate.sh`.
