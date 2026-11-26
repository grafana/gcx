//go:build wasip1

// A guest for TestPreinitReseedsRandomness: a reactor that, like gcx's
// pre-initialized build, reseeds the runtime's random state in gcx_run. It
// prints the hash of a fixed string under a hash/maphash seed made during
// init, which the snapshot keeps, then under one made after the reseed. Seeds
// come from the runtime's random state, as map hash seeds and math/rand do.
// Built with build.sh's overlay and snapshotted by preinit.sh, every instance
// starts from the same runtime state: the first hash is the same in every
// instance, and only the reseed makes the second differ.
package main

import (
	"fmt"
	"hash/maphash"
	"os"
	_ "unsafe" // for go:linkname
)

// seedAtInit is made before the snapshot.
var seedAtInit = maphash.MakeSeed()

//go:linkname reseedRandom runtime.gcxReseedRandom
func reseedRandom()

//go:wasmexport gcx_init
func gcxInit() {}

//go:wasmexport gcx_run
func gcxRun() {
	reseedRandom()
	fmt.Println(maphash.String(seedAtInit, "x"), maphash.String(maphash.MakeSeed(), "x"))
	os.Exit(0)
}

func main() {}
