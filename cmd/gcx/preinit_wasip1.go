//go:build wasip1 && gcxpreinit

package main

// A pre-initialized gcx module: experimental/sandbox/build.sh builds gcx with
// -buildmode=c-shared and the gcxpreinit tag, so it is a WASI reactor, runs
// its _initialize (the Go runtime and every package init) under wizer, and
// snapshots the result. Each command then starts from that snapshot by
// calling gcx_run, skipping init.
//
// Everything init read from the host is the snapshot's, not the command's,
// so gcx_run reloads it: the arguments, the environment, the runtime's GC
// settings and random state, the working directory, and the package state
// derived from the environment. What it leaves as the snapshot had it is
// listed in experimental/sandbox/README.md.

import (
	"bytes"
	"os"
	"runtime/debug"
	"strings"
	"unsafe"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/grafana/gcx/internal/agent"
)

//go:wasmimport wasi_snapshot_preview1 args_sizes_get
func wasiArgsSizesGet(count, size unsafe.Pointer) uint32

//go:wasmimport wasi_snapshot_preview1 args_get
func wasiArgsGet(ptrs, buf unsafe.Pointer) uint32

//go:wasmimport wasi_snapshot_preview1 environ_sizes_get
func wasiEnvironSizesGet(count, size unsafe.Pointer) uint32

//go:wasmimport wasi_snapshot_preview1 environ_get
func wasiEnvironGet(ptrs, buf unsafe.Pointer) uint32

// reseedRandom replaces the Go runtime's random state with fresh host
// entropy: map hash seeds, math/rand and hash/maphash would otherwise repeat
// in every instance started from the snapshot. build.sh adds it to the
// runtime from experimental/sandbox/overlay.
//
//go:linkname reseedRandom runtime.gcxReseedRandom
func reseedRandom()

// gcx_init is the function wizer runs before taking the snapshot. It does
// nothing: wizer has already run _initialize, as for any reactor.
//
//go:wasmexport gcx_init
func gcxInit() {}

// gcx_run runs one gcx command in an instance started from the snapshot. Like
// main, it ends in os.Exit.
//
//go:wasmexport gcx_run
func gcxRun() {
	os.Args = wasiStrings(wasiArgsSizesGet, wasiArgsGet)
	if len(os.Args) == 0 { // main expects a program name
		os.Args = []string{"gcx"}
	}
	os.Clearenv()
	for _, kv := range wasiStrings(wasiEnvironSizesGet, wasiEnvironGet) {
		// The first of duplicate keys wins, as in Go's own environment.
		if k, v, ok := strings.Cut(kv, "="); ok {
			if _, set := os.LookupEnv(k); !set {
				_ = os.Setenv(k, v)
			}
		}
	}
	gcPercent, memoryLimit, err := gcSettings(os.Getenv("GOGC"), os.Getenv("GOMEMLIMIT"))
	if err != nil { // the runtime aborts on a malformed GOMEMLIMIT, with status 2
		_, _ = os.Stderr.WriteString("gcx: " + err.Error() + "\n")
		os.Exit(2)
	}
	if gcPercent != nil {
		debug.SetGCPercent(*gcPercent)
	}
	if memoryLimit != nil {
		debug.SetMemoryLimit(*memoryLimit)
	}
	reseedRandom()
	// wasip1 takes the working directory from $PWD at init.
	if pwd := os.Getenv("PWD"); pwd != "" {
		_ = os.Chdir(pwd)
	}
	agent.Redetect()
	// The same as lipgloss's own initializer, which read the snapshot's
	// environment: an embedder can force color with CLICOLOR_FORCE.
	lipgloss.Writer = colorprofile.NewWriter(os.Stdout, os.Environ())

	main()
}

// wasiStrings reads a WASI string list, such as the arguments: sizes reports
// the count and buffer size, and get fills an array of pointers and the
// strings themselves, each NUL-terminated, in order.
func wasiStrings(sizes, get func(unsafe.Pointer, unsafe.Pointer) uint32) []string {
	var count, size uint32
	if sizes(unsafe.Pointer(&count), unsafe.Pointer(&size)) != 0 || count == 0 {
		return nil
	}
	ptrs := make([]uint32, count) // guest addresses: 32 bits on wasm
	buf := make([]byte, size)
	if get(unsafe.Pointer(&ptrs[0]), unsafe.Pointer(&buf[0])) != 0 {
		return nil
	}
	out := make([]string, 0, count)
	for s := range bytes.SplitSeq(bytes.TrimSuffix(buf, []byte{0}), []byte{0}) {
		out = append(out, string(s))
	}
	return out
}
