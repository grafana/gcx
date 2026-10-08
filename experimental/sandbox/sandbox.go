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
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/experimental"
	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
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
	//
	// On Linux, guest memory is mapped outside the Go heap (see the README),
	// so the Go GC and GOMEMLIMIT don't see it: leave room for it below the
	// container's limit, and watch the container's memory or PSS rather than
	// Go heap metrics. RSS counts the memory image the runs share once per
	// run. With vm.overcommit_memory=2, each run commits its full cap while
	// it runs.
	MemoryLimitBytes uint64
	// Transport performs the guest's HTTP requests after the egress policy
	// has allowed them. Nil means http.DefaultTransport.
	Transport http.RoundTripper
}

// ErrClosed is returned by Run once Close has been called, and by runs that
// Close stopped. A run that finishes anyway returns its result.
var ErrClosed = errors.New("sandbox: runtime closed")

// Runtime holds compiled gcx and runs commands with it. It is safe for
// concurrent use.
type Runtime struct {
	rt        wazero.Runtime
	compiled  wazero.CompiledModule
	transport http.RoundTripper
	cache     wazero.CompilationCache // nil without Config.CacheDir
	root      string                  // empty directory mounted read-only at /

	// newRunMemory is the allocator for each Run's instance, and how to free
	// what it allocated (see alloc_linux.go). Tests wrap it.
	newRunMemory func() (experimental.MemoryAllocator, func())
	// releaseImage releases the memory image newRunMemory maps into each
	// instance (see image_linux.go). Close calls it after closing rt.
	releaseImage func()

	// Close stops the runs in flight and waits for them before closing rt,
	// because closing rt frees every instance's memory, and a guest still
	// running in an unmapped instance crashes the process.
	mu      sync.Mutex
	closed  bool
	runs    sync.WaitGroup
	cancels map[*context.CancelCauseFunc]struct{} // of the runs in flight
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
	r := &Runtime{
		rt:           wazero.NewRuntimeWithConfig(ctx, rcfg),
		transport:    cfg.Transport,
		cache:        cache,
		newRunMemory: newRunMemory,
		releaseImage: func() {},
		cancels:      map[*context.CancelCauseFunc]struct{}{},
	}
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
	// On Linux, the module's data segments become one memory image that
	// every instance shares (see image_linux.go).
	module, runMemory, release := newMemoryImage(wasm)
	r.newRunMemory, r.releaseImage = runMemory, release
	compiled, err := r.rt.CompileModule(ctx, module)
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

// Close releases the runtime and compiled code. It stops any runs in flight,
// which then return ErrClosed, and waits for them first. A guest stops at its
// next safe point, or when the host call it is in returns: a run in gcx's
// retry backoff waits out the sleep, and one blocked reading Stdin or writing
// Stdout or Stderr waits for that to return.
//
// If ctx is done first, Close returns its error and leaves the runtime open,
// because closing it would free memory that a running guest still uses.
// Later runs are still refused.
func (r *Runtime) Close(ctx context.Context) error {
	r.mu.Lock()
	r.closed = true
	for cancel := range r.cancels {
		(*cancel)(ErrClosed)
	}
	r.mu.Unlock()
	stopped := make(chan struct{})
	go func() {
		r.runs.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-ctx.Done():
		return ctx.Err()
	}

	if r.root != "" {
		_ = os.RemoveAll(r.root)
	}
	err := r.rt.Close(ctx)
	r.releaseImage()
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
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil) // abandons any requests still in flight on the host
	if !r.start(&cancel) {
		return Result{}, ErrClosed
	}
	defer r.finish(&cancel)

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
	// wazero hands these slices of guest memory, which is unmapped when the
	// instance closes (see alloc_linux.go), so a reader or writer that kept
	// one after returning could crash the process or see a later run's
	// memory at the same address. Copy, so they only ever see their own
	// buffers.
	if inv.Stdin != nil {
		cfg = cfg.WithStdin(copyStdin(inv.Stdin))
	}
	if inv.Stdout != nil {
		cfg = cfg.WithStdout(copyOutput(inv.Stdout))
	}
	if inv.Stderr != nil {
		cfg = cfg.WithStderr(copyOutput(inv.Stderr))
	}

	ctx = withSession(ctx, newSession(inv.Egress, inv.Authorize, r.transport))
	alloc, freeMemory := r.newRunMemory()
	ctx = experimental.WithMemoryAllocator(ctx, alloc)

	mod, err := r.rt.InstantiateModule(ctx, r.compiled, cfg)
	if mod != nil {
		_ = mod.Close(ctx)
	}
	// The guest has stopped, so nothing uses its memory any more.
	freeMemory()
	if err != nil && errors.Is(context.Cause(ctx), ErrClosed) { // stopped by Close
		return Result{}, ErrClosed
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

// start registers a run, so Close can stop it and wait for it. It reports
// false once Close has been called.
func (r *Runtime) start(cancel *context.CancelCauseFunc) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.runs.Add(1)
	r.cancels[cancel] = struct{}{}
	return true
}

func (r *Runtime) finish(cancel *context.CancelCauseFunc) {
	r.mu.Lock()
	delete(r.cancels, cancel)
	r.mu.Unlock()
	r.runs.Done()
}

// copyStdin wraps r in a copyReader. An *os.File is passed through: its Read
// is a syscall that keeps nothing, and wazero only polls and stats the real
// file when it sees an *os.File. A wrapped reader keeps its Poll, which
// wazero calls for the guest's poll_oneoff on stdin, and which would
// otherwise always report ready.
func copyStdin(r io.Reader) io.Reader {
	switch p := r.(type) {
	case *os.File:
		return p
	case experimentalsys.Pollable:
		return pollableCopyReader{copyReader{r}, p}
	}
	return copyReader{r}
}

// copyOutput wraps w in a copyWriter, unless it is an *os.File, for the same
// reasons as copyStdin.
func copyOutput(w io.Writer) io.Writer {
	if f, ok := w.(*os.File); ok {
		return f
	}
	return copyWriter{w}
}

type pollableCopyReader struct {
	copyReader
	experimentalsys.Pollable
}

// copyReader reads into its own buffer, so the reader never sees guest memory.
type copyReader struct{ r io.Reader }

func (c copyReader) Read(p []byte) (int, error) {
	buf := make([]byte, min(len(p), 32<<10))
	n, err := c.r.Read(buf)
	copy(p, buf[:n])
	return n, err
}

// copyWriter writes through its own buffer, so the writer never sees guest
// memory. It copies at most 32 KiB at a time, so large writes don't make
// equally large copies.
type copyWriter struct{ w io.Writer }

func (c copyWriter) Write(p []byte) (int, error) {
	buf := make([]byte, min(len(p), 32<<10))
	written := 0
	for written < len(p) {
		n := copy(buf, p[written:])
		m, err := c.w.Write(buf[:n])
		written += m
		if err != nil {
			return written, err
		}
		if m < n {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}
