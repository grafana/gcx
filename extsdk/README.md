# gcx extension SDK

`github.com/grafana/gcx/extsdk` is the Go SDK for [gcx extensions](../docs/sources/guides/extensions.md). An extension is an executable that gcx starts with [go-plugin](https://github.com/hashicorp/go-plugin) when a user runs `gcx ext <name> [args...]`.

```go
package main

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/extsdk"
)

func main() {
	extsdk.Serve(func(ctx context.Context, gcx *extsdk.GCX, args []string) error {
		var health struct {
			Version string `json:"version"`
		}
		if err := gcx.DecodeJSON(ctx, &health, "api", "/api/health"); err != nil {
			return err
		}
		fmt.Println("Grafana", health.Version)
		return nil
	})
}
```

- `GCX.Execute` runs any gcx command in a new gcx process and returns its stdout, stderr and exit code. A non-zero exit is not an error: gcx can report a partial failure with a complete result, so check `Result.ExitCode` or `Result.Err()`
- `GCX.DecodeJSON` adds `--output json`, fails on a non-zero exit and decodes stdout
- Commands use the context, configuration file and agent mode the user ran `gcx ext` with, so the extension doesn't need to handle credentials. Extensions aren't sandboxed, though: they run with the user's permissions
- Write the extension's own output to `os.Stdout` and `os.Stderr`; gcx forwards both. Standard input is the user's terminal
- `ctx` is cancelled when the user interrupts gcx
- Return an `*extsdk.ExitError` to exit with a specific code

gcx JSON output is the contract for `Execute` and `DecodeJSON`; use `minGCXVersion` in the manifest when an extension depends on newer output.

## Protocol

Extensions in other languages implement the gRPC services in [`proto/extension.proto`](proto/extension.proto) with go-plugin's handshake: magic cookie `GCX_EXTENSION=gcx-extension`, protocol version `1` and plugin name `extension`. gcx calls `Extension.Run` once and serves `Host` on the go-plugin broker ID in the request for as long as `Run` lasts. Send the extension's stdout and stderr through `Host.Output` and close that stream before `Run` returns: go-plugin's own stdio forwarding can drop output written at the end of a run.

Run `./generate.sh` after changing the proto file. It regenerates the SDK's code and gcx's copy in `internal/extensions/extensionpb`; gcx does not import this module.
