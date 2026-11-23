# gcx extension SDK

`github.com/grafana/gcx/extsdk` is the Go SDK for [gcx extensions](../docs/sources/guides/extensions.md). An extension is an executable that gcx starts when a user runs `gcx ext <name> [args...]`. It communicates with `gcx` using [go-plugin](https://github.com/hashicorp/go-plugin) .


Here is an example extension, which fetches a Grafana instance's version from gcx:

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

The SDK gives an extension two ways to run gcx commands:

- `GCX.Execute` runs any gcx command in a new gcx process and returns its stdout, stderr and exit code. 
- `GCX.DecodeJSON` adds `--output json`, decodes stdout and returns an `*ExitError` for any non-zero exit, so it drops partial results. Use `Execute` when you want to catch partial results.

Some more notes about extensions:

- Commands use the same context from when the user ran `gcx ext`, so the extension doesn't need to handle credentials.
- Authors should write the extension's own output to `os.Stdout` and `os.Stderr`. `gcx` forwards both.
- Authors should return an `*extsdk.ExitError` to exit with a specific code.
- The error from `GCX.Execute` is only non-nil when the command couldn't run. A command that has a non-zero exit still returns its output, because gcx exits 4 on a partial failure but prints the results that succeeded. Therefore, check `Result.ExitCode`, or call `Result.Err()` to check whether the command succeeded.
- `Host.Execute` streams a command's output in chunks as it is written and sends its exit code as the last event, so output size isn't limited by gRPC's message size.
- A request's stdin can be up to 64 MiB.


#### Other languages

We only provide an interface for Go, but extensions can in theory be written in other languages. Check the protocol definitions in [`proto/extension.proto`](proto/extension.proto).

#### Protocol changes

After making changes to the protocol definition in `proto/extension.proto`, run `mise run generate:extsdk` from the repository root and commit the generated files. It regenerates both the SDK's code in `internal/extensionpb` and gcx's own copy in `../internal/extensions/extensionpb`.
