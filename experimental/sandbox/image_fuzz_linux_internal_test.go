//go:build linux

package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/experimental"
)

// FuzzMemoryImage checks the memory image against wazero itself: for any
// module of the shape Go's linker emits, an instance of the stripped module
// on the image starts with exactly the memory wazero gives the original,
// buildImage finds no data only in modules whose memory starts as zeros, and
// it refuses exactly the other modules wazero can't instantiate.
//
// go test runs the seeds below. Run go test -fuzz=FuzzMemoryImage to search
// further.
func FuzzMemoryImage(f *testing.F) {
	for _, seed := range [][]byte{
		{0, 0},                                   // no data section
		{0, 0, 0, 0, 0},                          // one empty segment at 0
		{2, 0, 0, 0, 10, 0, 1, 20},               // segments in two pages
		{0, 0, 0, 4, 10, 0, 4, 10},               // the same offset twice: the later wins
		{0, 0, 0, 5, 30, 0, 5, 10},               // overlapping, at the previous offset
		{1, 0, 0, 1, 10},                         // straddling a page boundary
		{0, 0, 0, 2, 10},                         // ending at the initial memory
		{0, 0, 0, 3, 10},                         // one byte past it
		{0, 0, 0, 3, 0},                          // empty, one byte past it
		{0, 1, 0, 0, 10},                         // with a data count section
		{0, 0, 2, 0, 10, 0, 0, 10},               // a passive segment keeps the indices
		{1, 0, 3, 1, 250, 0, 0, 3, 2, 4, 5, 1},   // flags 2, a large segment, passive
		{2, 1, 0, 1, 250, 2, 2, 250, 3, 5, 7, 9}, // everything at once
	} {
		f.Add(seed)
	}
	// Arbitrary layouts too, so that go test covers more than the cases
	// above. Hashing keeps them the same from run to run.
	for i := range 200 {
		sum := sha256.Sum256([]byte{byte(i)})
		f.Add(sum[1:][:2+3*(sum[0]%8)])
	}

	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter().WithMemoryLimitPages(8))
	f.Cleanup(func() { _ = rt.Close(ctx) })

	f.Fuzz(func(t *testing.T, seed []byte) {
		wasm := generateModule(seed)
		want, wantErr := initialMemory(ctx, rt, wasm, nil)

		img, stripped, err := buildImage(wasm)
		if errors.Is(err, errNoData) {
			// No image: newMemoryImage keeps the module as it is, so wazero's
			// own memory is what runs. It must really start as zeros, or the
			// module had data that buildImage missed.
			if wantErr == nil {
				if i := slices.IndexFunc(want, func(b byte) bool { return b != 0 }); i >= 0 {
					t.Fatalf("buildImage found no data, but wazero's initial memory has a non-zero byte at %d", i)
				}
			}
			return
		}
		if err != nil {
			if wantErr == nil {
				t.Fatalf("buildImage: %v, but wazero instantiates the module", err)
			}
			return
		}
		defer img.f.Close()
		if wantErr != nil {
			t.Fatalf("buildImage accepted a module wazero refuses: %v", wantErr)
		}
		alloc, free := img.newRunMemory()
		defer free()
		got, err := initialMemory(ctx, rt, stripped, alloc)
		if err != nil {
			t.Fatalf("the stripped module: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("initial memory differs from wazero's at byte %d", firstDifference(got, want))
		}
	})
}

// generateModule builds a module from seed: its first byte picks the memory
// size and its second whether to add a data count section, and each three
// bytes after that are a data segment's kind, offset and length.
func generateModule(seed []byte) []byte {
	next := func() byte {
		if len(seed) == 0 {
			return 0
		}
		b := seed[0]
		seed = seed[1:]
		return b
	}
	pages := 1 + uint64(next()%3)
	memBytes := int32(pages * 65536)
	withDataCount := next()%2 == 1
	if len(seed) == 0 {
		return testModule(pages, nil)
	}

	var segs [][]byte
	var prev int32
	for i := 0; len(seed) > 0 && i < 8; i++ {
		kind, at, size := next(), next(), next()
		n := int32(size % 40)
		if size >= 250 {
			n = 70000 // more than a page
		}
		var offset int32
		switch at % 6 {
		case 0:
			offset = 0
		case 1:
			offset = 65536 - n/2 // straddling the first page boundary
		case 2:
			offset = memBytes - n // ending at the initial memory
		case 3:
			offset = memBytes - n + 1 // one byte past it
		case 4:
			offset = prev // at the previous segment's offset
		case 5:
			offset = prev + int32(at) // overlapping the previous segment
		}
		offset = max(offset, 0)
		prev = offset
		// Each segment has its own contents, so the order they land in shows.
		data := string(bytes.Repeat([]byte{byte(i + 1)}, int(n)))
		switch kind % 4 {
		case 0, 1:
			segs = append(segs, active(0, offset, data))
		case 2:
			segs = append(segs, append(appendULEB([]byte{1}, uint64(n)), data...)) // passive
		case 3:
			segs = append(segs, active(2, offset, data))
		}
	}

	wasm := testModule(pages, nil)
	if withDataCount {
		count := appendULEB(nil, uint64(len(segs)))
		wasm = append(appendULEB(append(wasm, 12), uint64(len(count))), count...)
	}
	data := dataSection(segs...)
	wasm = appendULEB(append(wasm, 11), uint64(len(data)))
	return append(wasm, data...)
}

// initialMemory instantiates wasm in rt, on alloc unless it's nil, and
// returns a copy of its memory.
func initialMemory(ctx context.Context, rt wazero.Runtime, wasm []byte, alloc experimental.MemoryAllocator) ([]byte, error) {
	compiled, err := rt.CompileModule(ctx, wasm)
	if err != nil {
		return nil, err
	}
	defer compiled.Close(ctx)
	if alloc != nil {
		ctx = experimental.WithMemoryAllocator(ctx, alloc)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		return nil, err
	}
	defer mod.Close(ctx)
	mem, _ := mod.Memory().Read(0, mod.Memory().Size())
	return bytes.Clone(mem), nil
}

func firstDifference(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}
