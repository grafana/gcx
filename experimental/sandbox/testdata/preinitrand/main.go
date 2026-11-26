//go:build wasip1

// A guest for TestPreinitReseedsRandomness: a reactor that, like gcx's
// pre-initialized build, reseeds the runtime's random state in gcx_run. It
// prints a math/rand/v2 value drawn during init, which the snapshot keeps,
// then one drawn after the reseed. Built with build.sh's overlay and
// snapshotted by preinit.sh, every instance starts from the same runtime
// state: the first value is the same in every instance, and only the reseed
// makes the second differ.
package main

import (
	"fmt"
	"math/rand/v2"
	"os"
	_ "unsafe" // for go:linkname
)

// drawnAtInit is drawn before the snapshot.
var drawnAtInit = rand.Uint64()

//go:linkname reseedRandom runtime.gcxReseedRandom
func reseedRandom()

//go:wasmexport gcx_init
func gcxInit() {}

//go:wasmexport gcx_run
func gcxRun() {
	reseedRandom()
	fmt.Println(drawnAtInit, rand.Uint64())
	os.Exit(0)
}

func main() {}
