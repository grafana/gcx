# Generic extension SDK

An optional, standalone Go module for RFC-style subprocess extensions. The gcx
host does not import it. It has no third-party dependencies, domain types, typed
resource helpers or RPC connection.

```go
client, err := extensionsdk.FromEnvironment()
// Handle err before using client.
result, err := client.Execute(ctx, extensionsdk.CommandRequest{
    Args: []string{"resources", "get", "dashboards", "--output", "json", "--limit", "0"},
})
// Handle launch/cancellation errors separately from result.Err().
// Stdout, Stderr and ExitCode remain available even on partial failure.
```

`FromEnvironment` reads `GCX_EXT_GCX_BIN`, `GCX_CONTEXT` and `GCX_CONFIG`. The
executable must be absolute; no PATH fallback silently chooses another gcx.
Execution inherits cwd and environment, and forwards the selected config and
context. It does not export resolved credentials or bypass mutation approval.

`Execute` runs an argument vector without a shell, accepts optional stdin and
buffers stdout/stderr until exit. Nonzero command exits return a `Result`;
`Result.Err()` exposes an `ExitError` with the exit code and prefers structured
gcx diagnostics when available. Launch failures and cancellation return errors.
Each call has its own context: cancellation stops that child, not other calls.
It does not guarantee termination of arbitrary grandchildren.

`DecodeJSON(ctx, executor, destination, args...)` appends `--output json`, checks
the result, and decodes into the caller's own response model. It treats nonzero
exits as errors. Use `Execute` when partial stdout matters, as resource-explorer
does for exit 4. Domain-specific arguments and response models live in extensions.

This API is for finite, noninteractive commands. Streaming, child terminals,
host lifecycle notifications and sandboxing are not provided. Every operation
starts gcx and loads configuration/authentication independently. Signalled child
processes may have exit code -1 from Go's process API.

The module is unpublished; the example uses a local Go module replacement.
Publishing would require versioning and CLI compatibility tests.

Validation: `go test -race ./...` and `go vet ./...`.
