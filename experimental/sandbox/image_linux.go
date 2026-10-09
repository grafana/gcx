//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/tetratelabs/wazero/experimental"
	"golang.org/x/sys/unix"
)

// newMemoryImage moves the module's data segments into a memory image that
// every instance maps copy-on-write. It returns the module to compile, the
// allocator for each Run, and a function that releases the image.
//
// gcx's data segments (its read-only data, pclntab and type information) are
// about 62 MiB, and wazero copies them into every instance, although a run
// writes only about 0.1 MiB of them. So New empties the segments in the module
// it compiles, and writes them once into a sealed memfd instead. Each
// instance maps the memfd MAP_PRIVATE over the start of its reservation:
// pages the guest only reads stay shared page cache, counted once for all
// instances, and nothing is copied when an instance starts.
//
// If the module has no data, isn't one this understands, or the kernel
// refuses the memfd, it returns the module unchanged with newRunMemory, and a
// nil release.
func newMemoryImage(wasm []byte) ([]byte, func() (experimental.MemoryAllocator, func()), func()) {
	img, module, err := buildImage(wasm)
	if err != nil {
		return wasm, newRunMemory, nil
	}
	return module, img.newRunMemory, func() { _ = img.f.Close() }
}

var errNoData = errors.New("no data segments")

// buildImage writes wasm's data segments into a sealed memfd, and returns it
// with the module to compile instead of wasm.
//
// The image ends at the page after the last segment's data. The rest of the
// initial memory (Go's bss and the first heap pages) starts as zeros, and
// guests write to it in every run: as part of a MAP_PRIVATE memfd mapping,
// each of those writes would first allocate the memfd page and then copy it,
// leaving a page of zeros in the image until Close. The anonymous reservation
// beyond the image covers it instead.
func buildImage(wasm []byte) (*memoryImage, []byte, error) {
	fd, err := unix.MemfdCreate("gcx-memory-image", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, nil, err
	}
	f := os.NewFile(uintptr(fd), "gcx-memory-image")
	var end int64
	module, _, err := stripModule(wasm, func(offset uint32, data []byte) error {
		end = max(end, int64(offset)+int64(len(data)))
		_, err := f.WriteAt(data, int64(offset))
		return err
	})
	page := int64(os.Getpagesize())
	size := (end + page - 1) / page * page
	if err == nil && size == 0 {
		err = errNoData
	}
	if err == nil {
		// Sizing it after writing the segments only rounds it up to a page:
		// mapping past the end of the file would fault.
		err = f.Truncate(size)
	}
	if err == nil {
		// Nothing may change the image under the instances mapping it.
		_, err = unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS,
			unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_WRITE|unix.F_SEAL_SEAL)
	}
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return &memoryImage{f: f, size: uint64(size)}, module, nil //nolint:gosec // size is a positive number of pages.
}

// memoryImage is the start of a module's initial memory, as its data
// segments describe it.
type memoryImage struct {
	f    *os.File
	size uint64 // a whole number of pages, up to the end of the last segment
}

func (img *memoryImage) newRunMemory() (experimental.MemoryAllocator, func()) {
	return newRunMemoryWith(img.allocate)
}

// allocate is allocateMapped with the image mapped over the start of the
// memory.
func (img *memoryImage) allocate(capacity, maxBytes uint64) experimental.LinearMemory {
	if buf, ok := reserve(maxBytes); ok {
		if img.size > uint64(len(buf)) { // can't happen: New rejects a cap below the minimum
			_ = syscall.Munmap(buf)
		} else if img.mapOver(buf) {
			return &mappedMemory{buf: buf} // unmapping buf in Free unmaps the image too
		}
	}
	buf := make([]byte, img.size, max(capacity, img.size))
	if err := img.read(buf); err != nil {
		// Reading a memfd only fails if the kernel is out of memory, and a
		// guest without its data would run on garbage.
		panic(fmt.Sprintf("sandbox: reading the memory image: %v", err))
	}
	return &heapMemory{buf: buf}
}

// mapOver maps the image over the start of buf, a reservation from reserve.
// When it can't, it gives back what's left of buf, and buf must not be used.
func (img *memoryImage) mapOver(buf []byte) bool {
	_, err := unix.MmapPtr(int(img.f.Fd()), 0, unsafe.Pointer(&buf[0]), uintptr(img.size), // nosemgrep: go.lang.security.audit.unsafe.use-of-unsafe-block -- MmapPtr takes the address to map at, the start of buf, as an unsafe.Pointer
		unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_FIXED)
	if err == nil {
		return true
	}
	// A failed MAP_FIXED may already have unmapped the start of buf, and
	// another mapping may since have taken that range, so neither write to
	// it nor unmap it: that could clobber someone else's memory. Unmap only
	// the rest, which the call never touched. At worst the start stays
	// reserved but untouched until the process exits.
	if rest := buf[img.size:]; len(rest) > 0 {
		_ = unix.MunmapPtr(unsafe.Pointer(&rest[0]), uintptr(len(rest))) // nosemgrep: go.lang.security.audit.unsafe.use-of-unsafe-block -- MunmapPtr takes the address to unmap from as an unsafe.Pointer
	}
	return false
}

func (img *memoryImage) read(buf []byte) error {
	_, err := img.f.ReadAt(buf, 0)
	return err
}

// stripModule returns wasm with every active data segment emptied, and the
// size of its initial memory. It calls put with each segment's offset and
// contents, in order, so that put can build the memory wazero would have.
//
// When nothing can refer to the segments by index (no passive segments and
// no data count section, which memory.init and data.drop need), it drops
// them instead: Go's linker emits up to 100,000, and wazero keeps a record of
// each in the compiled module and in every instance.
//
// It handles what Go's wasm linker emits: one memory, and segments at
// constant offsets. Anything else is an error.
func stripModule(wasm []byte, put func(offset uint32, data []byte) error) ([]byte, uint64, error) {
	if len(wasm) < 8 || string(wasm[:4]) != "\x00asm" {
		return nil, 0, errors.New("not a wasm module")
	}
	type section struct {
		id   byte
		body []byte
	}
	var sections []section
	var minBytes uint64
	var dataCount bool
	r := wasmReader{b: wasm, i: 8}
	for r.err == nil && r.i < len(r.b) {
		id := r.byte()
		body := r.bytes(r.uleb())
		if r.err != nil {
			break
		}
		switch id {
		case 5: // memory
			mr := wasmReader{b: body}
			if n := mr.uleb(); n != 1 {
				return nil, 0, fmt.Errorf("%d memories", n)
			}
			if flags := mr.uleb(); flags&^1 != 0 {
				return nil, 0, fmt.Errorf("memory flags %#x", flags)
			}
			pages := mr.uleb()
			if mr.err != nil {
				return nil, 0, mr.err
			}
			if pages > 65536 {
				return nil, 0, errors.New("memory larger than 4 GiB")
			}
			minBytes = pages * 65536
		case 12: // data count
			dataCount = true
		case 11: // data
			if minBytes == 0 {
				return nil, 0, errors.New("no memory for the data segments")
			}
			stripped, err := stripData(body, minBytes, dataCount, put)
			if err != nil {
				return nil, 0, err
			}
			body = stripped
		}
		sections = append(sections, section{id, body})
	}
	if r.err != nil {
		return nil, 0, r.err
	}
	if minBytes == 0 {
		return nil, 0, errors.New("no memory to initialize")
	}

	size := 8
	for _, s := range sections {
		size += 1 + ulebLen(uint64(len(s.body))) + len(s.body)
	}
	out := make([]byte, 0, size)
	out = append(out, wasm[:8]...)
	for _, s := range sections {
		out = append(out, s.id)
		out = appendULEB(out, uint64(len(s.body)))
		out = append(out, s.body...)
	}
	return out, minBytes, nil
}

// stripData rewrites a data section's body with its active segments emptied,
// or with no segments when keepIndices is false and none is passive. The
// memory and data count sections come before it.
func stripData(body []byte, minBytes uint64, keepIndices bool, put func(uint32, []byte) error) ([]byte, error) {
	r := wasmReader{b: body}
	n := r.uleb()
	out := appendULEB(nil, n)
	for range n {
		start := r.i
		flags := r.uleb()
		if flags == 1 { // passive: memory.init copies it later, so keep it
			r.bytes(r.uleb())
			out = append(out, body[start:r.i]...)
			keepIndices = true
			continue
		}
		if flags == 2 && r.uleb() != 0 {
			return nil, errors.New("data segment for another memory")
		} else if flags > 2 {
			return nil, fmt.Errorf("data segment flags %d", flags)
		}
		exprStart := r.i
		if op := r.byte(); op != 0x41 { // i32.const
			return nil, fmt.Errorf("data segment offset opcode %#x", op)
		}
		offset := r.i32()
		if end := r.byte(); end != 0x0b {
			return nil, errors.New("data segment offset isn't a constant")
		}
		exprEnd := r.i
		data := r.bytes(r.uleb())
		if r.err != nil {
			return nil, r.err
		}
		if uint64(offset)+uint64(len(data)) > minBytes {
			return nil, errors.New("data segment outside the initial memory")
		}
		if len(data) > 0 {
			if err := put(offset, data); err != nil {
				return nil, err
			}
		}
		// The same segment, at the same offset, with no contents.
		out = append(out, 0x00)
		out = append(out, body[exprStart:exprEnd]...)
		out = append(out, 0x00)
	}
	if r.err == nil && r.i != len(body) {
		return nil, errors.New("trailing bytes in the data section")
	}
	if !keepIndices {
		out = appendULEB(nil, 0)
	}
	return out, r.err
}

// wasmReader decodes the parts of the wasm binary format stripModule needs.
// Reading past the end sets err and returns zeros.
type wasmReader struct {
	b   []byte
	i   int
	err error
}

var errTruncated = errors.New("truncated module")

func (r *wasmReader) byte() byte {
	if r.err != nil || r.i >= len(r.b) {
		r.err = errTruncated
		return 0
	}
	r.i++
	return r.b[r.i-1]
}

func (r *wasmReader) bytes(n uint64) []byte {
	rest := r.b[r.i:]
	if r.err != nil || n > uint64(len(rest)) {
		r.err = errTruncated
		return nil
	}
	b := rest[:n]
	r.i += len(b)
	return b
}

func (r *wasmReader) uleb() uint64 {
	var v uint64
	for shift := uint(0); shift < 64; shift += 7 {
		b := r.byte()
		v |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return v
		}
	}
	r.err = errors.New("LEB128 too long")
	return 0
}

// i32 decodes an i32.const immediate, a signed LEB128, as the unsigned
// offset wasm reads it as.
func (r *wasmReader) i32() uint32 {
	var v uint32
	for shift := uint(0); shift < 35; shift += 7 {
		b := r.byte()
		v |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			if shift+7 < 32 && b&0x40 != 0 {
				v |= ^uint32(0) << (shift + 7) // sign-extend
			}
			return v
		}
	}
	r.err = errors.New("LEB128 too long")
	return 0
}

func appendULEB(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func ulebLen(v uint64) int {
	n := 1
	for v >= 0x80 {
		v >>= 7
		n++
	}
	return n
}
