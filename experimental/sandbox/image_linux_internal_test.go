//go:build linux

package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero/experimental"
)

// testModule is a module with a memory section of minPages and a data
// section, if dataSection isn't nil.
func testModule(minPages uint64, dataSection []byte) []byte {
	b := []byte("\x00asm\x01\x00\x00\x00")
	if minPages > 0 {
		mem := appendULEB([]byte{1, 0}, minPages)
		b = appendULEB(append(b, 5), uint64(len(mem)))
		b = append(b, mem...)
	}
	if dataSection != nil {
		b = appendULEB(append(b, 11), uint64(len(dataSection)))
		b = append(b, dataSection...)
	}
	return b
}

// active encodes an active data segment for memory 0, with the given flags (0
// or 2) at an i32.const offset.
func active(flags byte, offset int32, data string) []byte {
	b := []byte{flags}
	if flags == 2 {
		b = append(b, 0) // memory 0
	}
	b = appendSLEB(append(b, 0x41), int64(offset))
	b = appendULEB(append(b, 0x0b), uint64(len(data)))
	return append(b, data...)
}

func dataSection(segments ...[]byte) []byte {
	b := appendULEB(nil, uint64(len(segments)))
	for _, s := range segments {
		b = append(b, s...)
	}
	return b
}

type put struct {
	offset uint32
	data   string
}

// segments counts the data segments left in a module.
func segments(t *testing.T, wasm []byte) uint64 {
	t.Helper()
	r := wasmReader{b: wasm, i: 8}
	for r.err == nil && r.i < len(r.b) {
		id := r.byte()
		body := r.bytes(r.uleb())
		if id == 11 {
			return (&wasmReader{b: body}).uleb()
		}
	}
	if r.err != nil {
		t.Fatal(r.err)
	}
	return 0
}

func TestStripModule(t *testing.T) {
	passive := append(appendULEB([]byte{1}, 2), "pp"...)
	// A data count section (12) of 1 goes between memory (5) and data (11).
	withDataCount := testModule(1, nil)
	withDataCount = append(withDataCount, 12, 1, 1)
	withDataCount = appendULEB(append(withDataCount, 11), uint64(len(dataSection(active(0, 16, "abc")))))
	withDataCount = append(withDataCount, dataSection(active(0, 16, "abc"))...)
	for _, tc := range []struct {
		name         string
		wasm         []byte
		wantPuts     []put
		wantSegments uint64 // left in the stripped module
		wantErr      string
	}{
		{
			name:     "active segments are dropped",
			wasm:     testModule(2, dataSection(active(0, 16, "abc"), active(0, 70000, "xyz"), active(2, 100, "q"))),
			wantPuts: []put{{16, "abc"}, {70000, "xyz"}, {100, "q"}},
		},
		{
			// memory.init names the passive segment by its index.
			name: "a passive segment keeps every index",
			wasm: testModule(2, dataSection(
				active(0, 16, "abc"), passive, active(0, 70000, "xyz"), active(2, 100, "q"), active(0, 5, ""),
			)),
			wantPuts:     []put{{16, "abc"}, {70000, "xyz"}, {100, "q"}},
			wantSegments: 5,
		},
		{name: "a data count keeps every index", wasm: withDataCount, wantPuts: []put{{16, "abc"}}, wantSegments: 1},
		{name: "no data", wasm: testModule(1, nil)},
		{name: "no memory", wasm: testModule(0, dataSection(active(0, 0, "a"))), wantErr: "no memory"},
		{name: "outside the initial memory", wasm: testModule(1, dataSection(active(0, 65535, "ab"))), wantErr: "outside"},
		{
			name: "offset from a global",
			// global.get 0 instead of i32.const.
			wasm:    testModule(1, dataSection([]byte{0, 0x23, 0, 0x0b, 1, 'a'})),
			wantErr: "opcode 0x23",
		},
		{name: "truncated", wasm: testModule(1, dataSection(active(0, 0, "abc")))[:20], wantErr: "truncated"},
		{name: "not wasm", wasm: []byte("hello, world"), wantErr: "not a wasm module"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var puts []put
			out, size, err := stripModule(tc.wasm, func(offset uint32, data []byte) error {
				puts = append(puts, put{offset, string(data)})
				return nil
			})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(puts, tc.wantPuts) {
				t.Fatalf("puts %v, want %v", puts, tc.wantPuts)
			}
			if n := segments(t, out); n != tc.wantSegments {
				t.Fatalf("%d segments left, want %d", n, tc.wantSegments)
			}
			// Stripping the stripped module finds nothing left to move, and
			// changes nothing.
			again, size2, err := stripModule(out, func(offset uint32, data []byte) error {
				t.Errorf("stripped module still has %q at %d", data, offset)
				return nil
			})
			if err != nil || size2 != size || !bytes.Equal(again, out) {
				t.Fatalf("stripping twice: err %v, size %d then %d, same %v", err, size, size2, bytes.Equal(again, out))
			}
		})
	}
}

func TestImageAllocate(t *testing.T) {
	img, _, err := buildImage(testModule(2, dataSection(active(0, 16, "abc"), active(0, 70000, "xyz"))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = img.f.Close() })
	want := make([]byte, 2*65536)
	copy(want[16:], "abc")
	copy(want[70000:], "xyz")
	for _, tc := range []struct {
		name     string
		maxBytes uint64
		mapped   bool
	}{
		{name: "mapped", maxBytes: 1 << 20, mapped: true},
		// No mapping can be that large, so this exercises the heap fallback.
		{name: "heap fallback", maxBytes: 1 << 62},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mem := img.allocate(64<<10, tc.maxBytes)
			if _, ok := mem.(*mappedMemory); ok != tc.mapped {
				t.Fatalf("got %T, want mapped %v", mem, tc.mapped)
			}
			buf := mem.Reallocate(img.size)
			if !bytes.Equal(buf, want) {
				t.Fatal("memory doesn't hold the image")
			}
			// Writes stay private to this memory, and growing keeps them.
			buf[16] = 'A'
			grown := mem.Reallocate(4 * 65536)
			if grown[16] != 'A' || !bytes.Equal(grown[17:2*65536], want[17:]) || !bytes.Equal(grown[2*65536:], make([]byte, 2*65536)) {
				t.Fatal("growing lost or dirtied the contents")
			}
			other := img.allocate(64<<10, tc.maxBytes)
			if other.Reallocate(img.size)[16] != 'a' {
				t.Fatal("a write to one memory reached the image")
			}
			other.Free()
			mem.Free()
		})
	}
}

// imageGuest is testdata/stdin.wasm with 1 MiB of initial memory and a
// 256 KiB data segment at 64 KiB.
func imageGuest(t *testing.T) ([]byte, []byte) {
	t.Helper()
	const offset = 64 << 10
	segment := bytes.Repeat([]byte("gcx memory image "), 256<<10/17+1)[:256<<10]
	wasm := append([]byte(nil), stdinGuest[:8]...)
	r := wasmReader{b: stdinGuest, i: 8}
	for r.i < len(r.b) {
		id := r.byte()
		body := r.bytes(r.uleb())
		if id == 5 {
			body = appendULEB([]byte{1, 0}, 16)
		}
		wasm = appendULEB(append(wasm, id), uint64(len(body)))
		wasm = append(wasm, body...)
	}
	if r.err != nil {
		t.Fatal(r.err)
	}
	data := dataSection(active(0, offset, string(segment)))
	wasm = appendULEB(append(wasm, 11), uint64(len(data)))
	return append(wasm, data...), segment
}

// Runs map the shared image rather than copying it, and each sees the data
// segments. Echoing stdin shows the guest's own writes land too.
func TestRunMapsMemoryImage(t *testing.T) {
	wasm, segment := imageGuest(t)
	r, err := New(t.Context(), wasm, Config{MemoryLimitBytes: 4 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })

	var mu sync.Mutex
	var mems []experimental.LinearMemory
	var problems []string
	inner := r.newRunMemory
	r.newRunMemory = func() (experimental.MemoryAllocator, func()) {
		alloc, free := inner()
		return experimental.MemoryAllocatorFunc(func(capacity, maxBytes uint64) experimental.LinearMemory {
			mem := alloc.Allocate(capacity, maxBytes)
			mu.Lock()
			mems = append(mems, mem)
			mu.Unlock()
			return mem
		}), free
	}

	const runs = 2
	var reading sync.WaitGroup
	reading.Add(runs)
	release := make(chan struct{})
	var wg sync.WaitGroup
	for range runs {
		wg.Go(func() {
			var once sync.Once
			stdin := readerFunc(func(p []byte) (int, error) {
				once.Do(reading.Done)
				<-release
				return copy(p, "x"), nil
			})
			var out bytes.Buffer
			if _, err := r.Run(t.Context(), Invocation{Stdin: stdin, Stdout: &out}); err != nil || out.String() != "x" {
				mu.Lock()
				problems = append(problems, "run failed")
				mu.Unlock()
			}
		})
	}
	reading.Wait()
	// The guests are blocked reading stdin, so their memory is still mapped.
	mu.Lock()
	for i, mem := range mems {
		mapped, ok := mem.(*mappedMemory)
		switch {
		case !ok:
			t.Errorf("memory %d is a %T, want *mappedMemory", i, mem)
		case !bytes.Equal(mapped.buf[64<<10:][:len(segment)], segment):
			t.Errorf("memory %d doesn't hold the data segment", i)
		}
	}
	mu.Unlock()
	maps, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(maps), "memfd:gcx-memory-image"); n < runs {
		t.Errorf("%d mappings of the image with %d runs in flight", n, runs)
	}
	close(release)
	wg.Wait()
	for _, p := range problems {
		t.Error(p)
	}
}

func appendSLEB(b []byte, v int64) []byte {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && c&0x40 == 0) || (v == -1 && c&0x40 != 0) {
			return append(b, c)
		}
		b = append(b, c|0x80)
	}
}

// loadGCX reads the real gcx module, as run_test.go does.
func loadGCX(t *testing.T) []byte {
	t.Helper()
	if testing.Short() {
		t.Skip("end-to-end test; skipped with -short")
	}
	path := os.Getenv("GCX_SANDBOX_WASM")
	if path == "" {
		path = "gcx.wasm"
	}
	wasm, err := os.ReadFile(path)
	if err != nil {
		if os.Getenv("GCX_SANDBOX_WASM") != "" {
			t.Fatal(err)
		}
		t.Skipf("gcx module not built (%v); run ./build.sh", err)
	}
	return wasm
}

// vmaUsage is what /proc/self/smaps reports for the mappings inside a range.
type vmaUsage struct {
	privateKiB, rssKiB uint64
	image              bool // one of them is the memory image
}

func usage(t *testing.T, start, end uint64) vmaUsage {
	t.Helper()
	smaps, err := os.ReadFile("/proc/self/smaps")
	if err != nil {
		t.Fatal(err)
	}
	var u vmaUsage
	inside := false
	for line := range strings.Lines(string(smaps)) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if lo, hi, ok := strings.Cut(fields[0], "-"); ok && len(fields) >= 5 {
			from, err1 := strconv.ParseUint(lo, 16, 64)
			to, err2 := strconv.ParseUint(hi, 16, 64)
			inside = err1 == nil && err2 == nil && from >= start && to <= end
			if inside && strings.Contains(line, "memfd:gcx-memory-image") {
				u.image = true
			}
			continue
		}
		if !inside || len(fields) != 3 || fields[2] != "kB" {
			continue
		}
		n, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "Private_Clean:", "Private_Dirty:":
			u.privateKiB += n
		case "Rss:":
			u.rssKiB += n
		}
	}
	return u
}

// measuredMemory calls onFree just before freeing mem, while it's mapped.
type measuredMemory struct {
	experimental.LinearMemory

	onFree func()
}

func (m *measuredMemory) Free() {
	m.onFree()
	m.LinearMemory.Free()
}

// gcx runs on the memory image, so a run's private memory stays well below
// the ~62 MiB a copy of gcx's data segments costs. This fails if New quietly
// falls back to copying them, e.g. because a newer Go linker emits a data
// section stripModule doesn't handle.
func TestGCXRunsOnMemoryImage(t *testing.T) {
	const budgetMiB = 40
	wasm := loadGCX(t)
	stripped, _, err := stripModule(wasm, func(uint32, []byte) error { return nil })
	if err != nil {
		t.Fatalf("stripModule can't handle gcx: %v", err)
	}
	if n := segments(t, stripped); n != 0 {
		t.Errorf("%d data segments left in gcx, want none", n)
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(t.Context(), wasm, Config{CacheDir: filepath.Join(cache, "gcx-sandbox", "compiled")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })

	var measured bool
	var u vmaUsage
	inner := r.newRunMemory
	r.newRunMemory = func() (experimental.MemoryAllocator, func()) {
		alloc, free := inner()
		return experimental.MemoryAllocatorFunc(func(capacity, maxBytes uint64) experimental.LinearMemory {
			mem := alloc.Allocate(capacity, maxBytes)
			mapped, ok := mem.(*mappedMemory)
			if !ok {
				t.Errorf("memory is a %T, want *mappedMemory", mem)
				return mem
			}
			start, err := strconv.ParseUint(strings.TrimPrefix(fmt.Sprintf("%p", &mapped.buf[0]), "0x"), 16, 64)
			if err != nil {
				t.Fatal(err)
			}
			end := start + uint64(len(mapped.buf))
			return &measuredMemory{LinearMemory: mem, onFree: func() {
				measured = true
				u = usage(t, start, end)
			}}
		}), free
	}

	var out bytes.Buffer
	res, err := r.Run(t.Context(), Invocation{Args: []string{"version"}, Stdout: &out, Stderr: &out})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("gcx version: exit %d, err %v, output:\n%s", res.ExitCode, err, out.String())
	}
	if !measured {
		t.Fatal("the run's memory was never freed through the measuring allocator")
	}
	t.Logf("gcx version: %d MiB private, %d MiB resident", u.privateKiB>>10, u.rssKiB>>10)
	if !u.image {
		t.Error("the run's memory doesn't map the memory image")
	}
	if u.privateKiB>>10 >= budgetMiB {
		t.Errorf("the run used %d MiB of private memory, want under %d", u.privateKiB>>10, budgetMiB)
	}
}
