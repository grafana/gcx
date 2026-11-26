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
	"os"
	"runtime/debug"
	"strings"
	_ "unsafe" // for go:linkname

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/grafana/gcx/internal/agent"
)

// hostArgsEnv reads this instance's arguments and environment from WASI,
// which the runtime read only at startup. build.sh adds it to the runtime
// from experimental/sandbox/overlay.
//
//go:linkname hostArgsEnv runtime.gcxHostArgsEnv
func hostArgsEnv() (args, env []string)

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
	args, env := hostArgsEnv()
	os.Args = args
	if len(os.Args) == 0 { // main expects a program name
		os.Args = []string{"gcx"}
	}
	os.Clearenv()
	for _, kv := range env {
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
