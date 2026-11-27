//go:build wasip1

package runtime

import _ "unsafe" // for go:linkname

// This file is not part of gcx's packages: experimental/sandbox/build.sh adds
// it to the runtime with go build -overlay when it builds gcx-preinit.wasm,
// and cmd/gcx/preinit_wasip1.go calls its functions. They use runtime
// internals, so a Go upgrade can break the build; the sandbox's
// TestPreinitMatchesCommandModule and TestPreinitReseedsRandomness check
// they still work.

// gcxHostArgsEnv reads the arguments and environment from WASI again, as the
// runtime did at startup, and returns them. A module pre-initialized by
// wizer started from the snapshot's, not the instance's.
//
//go:linkname gcxHostArgsEnv
func gcxHostArgsEnv() (args, env []string) {
	goenvs()
	return argslice, envs
}

// gcxReseedRandom replaces the runtime's random state with fresh entropy
// from the host. A module pre-initialized by wizer starts every instance from
// the same runtime state, so without it every instance would draw the same
// map hash seeds, math/rand values and hash/maphash seeds.
//
//go:linkname gcxReseedRandom
func gcxReseedRandom() {
	var seed [32]byte
	if readRandom(seed[:]) != len(seed) {
		throw("gcxReseedRandom: random_get failed")
	}
	lock(&globalRand.lock)
	globalRand.state.Init(seed)
	unlock(&globalRand.lock)
	// wasip1 is single-threaded, so no other M is running.
	for mp := allm; mp != nil; mp = mp.alllink {
		mrandinit(mp)
	}
}
