# Typed go-plugin SDK experiment

This standalone, unpublished Go module owns the versioned contract between gcx
and local plugin executables. It uses HashiCorp go-plugin v1.8.0 with gRPC and
automatic mutual TLS. It exposes typed resource operations, not arbitrary CLI
execution. The host and example use a local module replacement while evaluating
this API; publishing it would require a separate module release.

A plugin implements one method and starts the SDK server:

```go
func main() { pluginsdk.Serve(app{}) }

type app struct{}

func (app) Run(ctx context.Context, req *pluginsdk.RunRequest,
    host pluginsdk.ResourceHostClient) (*pluginsdk.RunResponse, error) {
    types, err := host.ListTypes(ctx, &pluginsdk.ListTypesRequest{
        Selectors: []string{"dashboards"},
    })
    if err != nil { return nil, err }
    if len(types.GetTypes()) == 0 { return &pluginsdk.RunResponse{}, nil }
    result, err := host.ListResources(ctx, &pluginsdk.ListResourcesRequest{
        Type: types.GetTypes()[0],
    })
    if err != nil { return nil, err }
    // Check result.Failures, Warnings and Truncated before claiming completeness.
    _ = result
    return &pluginsdk.RunResponse{}, nil
}
```

`plugin.proto` is the contract; generated `.pb.go` files are checked in. Plugin
authors don't need protoc to build an existing SDK version. `App`, `Serve` and
`Run` in `runtime.go` hide go-plugin startup, broker wiring and cleanup.

| Method | Request | Response |
|---|---|---|
| `ListTypes` | Resource selector strings | Typed group/version/kind/plural/singular descriptors |
| `GetSchemas` | Discovered descriptors | Schemas associated with exact descriptors, plus warnings |
| `ListResources` | One discovered descriptor | Typed metadata, resource JSON, failures, warnings and truncation |

Resource bodies and OpenAPI schemas have runtime-defined shapes. They remain JSON
bytes inside typed responses, preserving unknown fields, absent `kind` values and
integer precision. This is not a statically generated Go model for every Grafana
resource kind. There is no `Execute(args)` escape hatch and no typed profile API.

The host registers its capability server on a broker connection for one `Run`.
The plugin calls back through the supplied typed client. The host owns clients
and credentials; neither are serialised into requests. Each callback propagates
its context cancellation through gRPC. A cancelled host run cancels RPC work and
cleans up the plugin. Plugin startup and callback-server startup each have a ten-second timeout.

`RunResponse` carries the application exit code, finite stdout and diagnostics.
Transport failures are separate errors. Noninteractive stdout is buffered until
Run completes; this is not a streaming output API. The example uses a separate
controlling-terminal handle for its TUI because stdout belongs to go-plugin.
Individual messages are limited to 64 MiB; larger responses fail explicitly.
Pagination/streaming across that limit is not implemented.

Protocol version 1 is checked at startup. Breaking method or wire changes need a
new protocol version. Additive protobuf fields can be compatible, but new methods
still need host-version/capability negotiation before plugin authors can rely on
them. That negotiation, an SDK support window and schema compatibility CI are
future work. Adding typed commands requires proto changes, generated bindings,
host handlers and tests; it does not automatically expose the full CLI surface.

The plugin is an ordinary process with the user's permissions and inherited
environment. mTLS protects the RPC connection; it does not sandbox the plugin or
prevent it from reading credentials or making its own network requests. The
magic cookie identifies the protocol, not the trustworthiness of plugin code.

## Development

```sh
go test -race ./...
go vet ./...
mise exec -- golangci-lint run -c ../../.golangci.yaml ./...
```

To regenerate after changing the proto, use protoc 36.2 and these Go generators:

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1
sh generate.sh
```
