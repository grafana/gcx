//go:build wasip1

package runtime

import _ "unsafe" // for go:linkname

// gcxReseedRandom replaces the runtime's random state with fresh entropy
// from the host. A module pre-initialized by wizer starts every instance from
// the same runtime state, so without it every instance would draw the same
// map hash seeds, math/rand values and hash/maphash seeds.
//
// This file is not part of gcx's packages: experimental/sandbox/build.sh adds
// it to the runtime with go build -overlay when it builds gcx-preinit.wasm,
// and cmd/gcx/preinit_wasip1.go calls it. It uses runtime internals, so a Go
// upgrade can break the build; TestPreinitReseedsRandomness checks it still
// works.
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
