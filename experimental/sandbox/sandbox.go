// Package sandbox runs gcx commands in an in-process sandbox, so programs
// can embed gcx (for example as an MCP tool) without importing it as a
// library or running it as a subprocess.
//
// EXPERIMENTAL: this package is v0 and its API may change without notice.
//
// gcx is compiled to WebAssembly (GOOS=wasip1, see build.sh) and run with
// wazero, a pure-Go runtime. Compile the module once with New, then call Run
// once per command. Each Run gets a fresh module instance that sees only what its
// Invocation grants: its args and env, an empty read-only root with a
// writable $HOME, and HTTP to the destinations in its egress policy. The
// guest has no sockets; the host makes every request and can attach
// credentials the guest never sees.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// Config configures a Runtime.
type Config struct {
	// CacheDir, if set, persists compiled code between processes, which
	// avoids the ~40s cold compile of gcx.
	CacheDir string
	// MemoryLimitBytes caps each instance's linear memory, rounded down to
	// 64 KiB pages. Zero means wazero's default (4 GiB).
	MemoryLimitBytes uint64
	// Transport performs the guest's HTTP requests after the egress policy
	// has allowed them. Nil means http.DefaultTransport.
	Transport http.RoundTripper
}

// Runtime holds compiled gcx and runs commands with it. It is safe for
// concurrent use.
type Runtime struct {
	rt        wazero.Runtime
	compiled  wazero.CompiledModule
	transport http.RoundTripper
	cache     wazero.CompilationCache // nil without Config.CacheDir
	root      string                  // empty directory mounted read-only at /
}

// New compiles the gcx wasip1 module (see build.sh).
func New(ctx context.Context, wasm []byte, cfg Config) (*Runtime, error) {
	rcfg := wazero.NewRuntimeConfig().WithCloseOnContextDone(true).
		// gcx has no DWARF, and nothing here uses function listeners, so
		// wazero's source map would only cost memory: since wazero#2527
		// (unreleased as of v1.12.0) it is recorded for every module, about
		// 150 MiB of heap for gcx.
		WithDebugInfoEnabled(false)
	if cfg.MemoryLimitBytes > 0 {
		rcfg = rcfg.WithMemoryLimitPages(memoryLimitPages(cfg.MemoryLimitBytes))
	}
	var cache wazero.CompilationCache
	if cfg.CacheDir != "" {
		var err error
		if cache, err = wazero.NewCompilationCacheWithDir(cfg.CacheDir); err != nil {
			return nil, err
		}
		rcfg = rcfg.WithCompilationCache(cache)
	}
	r := &Runtime{rt: wazero.NewRuntimeWithConfig(ctx, rcfg), transport: cfg.Transport, cache: cache}
	if r.transport == nil {
		r.transport = http.DefaultTransport
	}

	if err := r.init(ctx, wasm); err != nil {
		_ = r.Close(ctx)
		return nil, err
	}
	return r, nil
}

func (r *Runtime) init(ctx context.Context, wasm []byte) error {
	root, err := os.MkdirTemp("", "gcx-sandbox-root")
	if err != nil {
		return err
	}
	r.root = root
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r.rt); err != nil {
		return err
	}
	if err := instantiateHTTP(ctx, r.rt); err != nil {
		return err
	}
	compiled, err := r.rt.CompileModule(ctx, wasm)
	if err != nil {
		return err
	}
	r.compiled = compiled
	return nil
}

// memoryLimitPages converts a byte limit to 64 KiB wasm pages, rounding down
// and capping at wasm32's 4 GiB (65536 pages).
func memoryLimitPages(limit uint64) uint32 {
	const pageSize, maxPages = 65536, 65536
	pages := limit / pageSize
	if pages > maxPages {
		return maxPages
	}
	return uint32(pages)
}

// Close releases the runtime and compiled code.
func (r *Runtime) Close(ctx context.Context) error {
	if r.root != "" {
		_ = os.RemoveAll(r.root)
	}
	err := r.rt.Close(ctx)
	if r.cache != nil {
		err = errors.Join(err, r.cache.Close(ctx))
	}
	return err
}

// Invocation is one gcx command and everything it may access.
type Invocation struct {
	// Args are gcx's arguments, without the program name.
	Args []string
	// Env is the guest's entire environment, e.g. GRAFANA_SERVER. HOME is
	// always /home. GCX_AGENT_SPILL_BYTES defaults to 0, because the guest
	// has no /tmp to spill large agent-mode results to, and the caller
	// couldn't read the files anyway. Credentials belong in Egress, not here.
	Env map[string]string
	// Stdin, Stdout and Stderr default to empty input and discarded output.
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Home is the host directory mounted read-write at /home. Empty means a
	// fresh temporary directory, removed when Run returns. Runs given the
	// same Home share it: each sees, and can race with, the config and token
	// cache the others write. Give each tenant its own, and don't run one
	// tenant's commands concurrently if those writes must not interleave.
	Home string
	// Egress lists the only destinations the guest may reach.
	Egress []Destination
	// Authorize, if set, is called for every request that Egress allows,
	// before credentials are added and before it is sent. Returning an error
	// refuses the request; the guest sees the error's text. It may read the
	// body, which is restored before sending, but must not otherwise modify
	// the request. Use it for policy beyond the destination, such as which
	// methods and paths a caller may use.
	Authorize func(*http.Request) error
}

// Result is the outcome of a gcx command that ran to completion.
type Result struct {
	ExitCode int
}

// Run executes one gcx command in a fresh instance. Cancelling ctx, or
// reaching its deadline, stops the guest and returns ctx's error.
func (r *Runtime) Run(ctx context.Context, inv Invocation) (Result, error) {
	home := inv.Home
	if home == "" {
		dir, err := os.MkdirTemp("", "gcx-sandbox-home")
		if err != nil {
			return Result{}, err
		}
		defer os.RemoveAll(dir)
		home = dir
	}

	cfg := wazero.NewModuleConfig().
		WithName(""). // allow concurrent instances of the same module
		WithArgs(append([]string{"gcx"}, inv.Args...)...).
		WithFSConfig(wazero.NewFSConfig().
			// An empty root makes paths outside $HOME report ENOENT (as gcx
			// expects for optional config files) instead of EBADF.
			WithReadOnlyDirMount(r.root, "/").
			WithDirMount(home, "/home")).
		WithEnv("HOME", "/home").
		WithSysWalltime().WithSysNanotime().WithSysNanosleep()
	if _, ok := inv.Env["GCX_AGENT_SPILL_BYTES"]; !ok {
		cfg = cfg.WithEnv("GCX_AGENT_SPILL_BYTES", "0")
	}
	for k, v := range inv.Env {
		if k != "HOME" {
			cfg = cfg.WithEnv(k, v)
		}
	}
	if inv.Stdin != nil {
		cfg = cfg.WithStdin(inv.Stdin)
	}
	if inv.Stdout != nil {
		cfg = cfg.WithStdout(inv.Stdout)
	}
	if inv.Stderr != nil {
		cfg = cfg.WithStderr(inv.Stderr)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // abandons any requests still in flight on the host
	ctx = withSession(ctx, newSession(inv.Egress, inv.Authorize, r.transport))
	ctx = experimental.WithMemoryAllocator(ctx, memoryAllocator())

	mod, err := r.rt.InstantiateModule(ctx, r.compiled, cfg)
	if mod != nil {
		_ = mod.Close(ctx)
	}
	if err != nil && ctx.Err() != nil { // stopped by the caller's deadline or cancellation
		return Result{}, ctx.Err()
	}
	var exit *sys.ExitError
	if errors.As(err, &exit) {
		return Result{ExitCode: int(exit.ExitCode())}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("gcx: %w", err)
	}
	return Result{}, nil
}
