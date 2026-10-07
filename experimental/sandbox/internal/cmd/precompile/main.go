// Command precompile compiles a gcx.wasm into a wazero compilation cache, so
// that images can ship the compiled code and embedders skip the ~40 s cold
// compile at startup.
//
// Usage: go run ./internal/cmd/precompile <gcx.wasm> <cache dir>
//
// Pass the same directory as Config.CacheDir at runtime. Compiled code is
// specific to this module's wazero version and to the CPU architecture and OS
// it is compiled on, so run this natively on each target platform. It then
// runs `gcx version` once, so a module the sandbox cannot run fails here.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/grafana/gcx/experimental/sandbox"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "precompile:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: %s <gcx.wasm> <cache dir>", os.Args[0])
	}
	wasm, err := os.ReadFile(os.Args[1])
	if err != nil {
		return err
	}
	ctx := context.Background()

	start := time.Now()
	rt, err := sandbox.New(ctx, wasm, sandbox.Config{CacheDir: os.Args[2]})
	if err != nil {
		return fmt.Errorf("compiling: %w", err)
	}
	defer rt.Close(ctx)
	fmt.Fprintf(os.Stdout, "compiled in %v\n", time.Since(start).Round(time.Millisecond))

	var stdout, stderr bytes.Buffer
	res, err := rt.Run(ctx, sandbox.Invocation{Args: []string{"version"}, Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return fmt.Errorf("running gcx version: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("gcx version exited with status %d: %s", res.ExitCode, stderr.String())
	}
	_, err = stdout.WriteTo(os.Stdout)
	return err
}
