//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"math"
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
// If the module isn't one this understands, or the kernel refuses the memfd,
// it returns the module unchanged with newRunMemory.
func newMemoryImage(wasm []byte) ([]byte, func() (experimental.MemoryAllocator, func()), func()) {
	img, module, err := buildImage(wasm)
	if err != nil {
		return wasm, newRunMemory, func() {}
	}
	return module, img.newRunMemory, func() { _ = img.f.Close() }
}

// buildImage writes wasm's data segments into a sealed memfd, and returns it
// with the module to compile instead of wasm.
func buildImage(wasm []byte) (*memoryImage, []byte, error) {
	fd, err := unix.MemfdCreate("gcx-memory-image", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, nil, err
	}
	f := os.NewFile(uintptr(fd), "gcx-memory-image")
	module, size, err := stripModule(wasm, func(offset uint32, data []byte) error {
		_, err := f.WriteAt(data, int64(offset))
		return err
	})
	if err == nil {
		// Sizing it after writing the segments is fine: the writes extend
		// it as needed, and stripModule checked they fit.
		err = f.Truncate(int64(size)) //nolint:gosec // stripModule caps size at 4 GiB.
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
	return &memoryImage{f: f, size: size}, module, nil
}

// memoryImage is a module's initial memory, as its data segments describe it.
type memoryImage struct {
	f    *os.File
	size uint64 // the module's minimum memory, in bytes
}

func (img *memoryImage) newRunMemory() (experimental.MemoryAllocator, func()) {
	return newRunMemoryWith(img.allocate)
}

// allocate is allocateMapped with the image at the start of the memory.
func (img *memoryImage) allocate(capacity, maxBytes uint64) experimental.LinearMemory {
	if img.size <= maxBytes && maxBytes <= math.MaxInt {
		buf, err := syscall.Mmap(-1, 0, int(maxBytes), syscall.PROT_READ|syscall.PROT_WRITE,
			syscall.MAP_PRIVATE|syscall.MAP_ANON|syscall.MAP_NORESERVE)
		if err == nil {
			// Unmapping buf in Free unmaps this too.
			_, err = unix.MmapPtr(int(img.f.Fd()), 0, unsafe.Pointer(&buf[0]), uintptr(img.size),
				unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_FIXED)
			if err == nil || img.read(buf[:img.size]) == nil {
				return &mappedMemory{buf: buf}
			}
			_ = syscall.Munmap(buf)
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

func (img *memoryImage) read(buf []byte) error {
	_, err := img.f.ReadAt(buf, 0)
	return err
}

// stripModule returns wasm with every active data segment emptied, and the
// size of its initial memory. It calls put with each segment's offset and
// contents, in order, so that put can build the memory wazero would have.
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
		case 11: // data
			if minBytes == 0 {
				return nil, 0, errors.New("no memory for the data segments")
			}
			stripped, err := stripData(body, minBytes, put)
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

// stripData rewrites a data section's body with its active segments emptied.
// The memory section comes before it, so minBytes is known.
func stripData(body []byte, minBytes uint64, put func(uint32, []byte) error) ([]byte, error) {
	r := wasmReader{b: body}
	n := r.uleb()
	out := appendULEB(nil, n)
	for range n {
		start := r.i
		flags := r.uleb()
		if flags == 1 { // passive: memory.init copies it later, so keep it
			r.bytes(r.uleb())
			out = append(out, body[start:r.i]...)
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
