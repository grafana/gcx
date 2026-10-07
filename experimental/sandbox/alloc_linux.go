//go:build linux

package sandbox

import (
	"math"
	"syscall"

	"github.com/tetratelabs/wazero/experimental"
)

// memoryAllocator backs each instance's linear memory with its own mapping,
// reserved at the memory limit up front and touched only as the guest grows
// into it. wazero's default grows a []byte by copying and leaves the old
// buffers to the Go GC, so concurrent runs peak at about 2.5x the memory and
// a process holds its high-water mark long after the runs end. A mapping
// never copies, and Free returns its pages to the OS as the instance closes.
func memoryAllocator() experimental.MemoryAllocator {
	return experimental.MemoryAllocatorFunc(allocateMapped)
}

func allocateMapped(capacity, maxBytes uint64) experimental.LinearMemory {
	// wazero can't handle a nil LinearMemory, so when the reservation is
	// refused (e.g. by strict overcommit), fall back to the heap.
	if maxBytes <= math.MaxInt {
		buf, err := syscall.Mmap(-1, 0, int(maxBytes), syscall.PROT_READ|syscall.PROT_WRITE,
			syscall.MAP_PRIVATE|syscall.MAP_ANON|syscall.MAP_NORESERVE)
		if err == nil {
			return &mappedMemory{buf: buf}
		}
	}
	return &heapMemory{buf: make([]byte, 0, capacity)}
}

type mappedMemory struct{ buf []byte }

func (m *mappedMemory) Reallocate(size uint64) []byte {
	if size > uint64(len(m.buf)) {
		return nil
	}
	return m.buf[:size]
}

func (m *mappedMemory) Free() {
	if m.buf != nil {
		_ = syscall.Munmap(m.buf)
		m.buf = nil
	}
}

// heapMemory grows like wazero's default linear memory.
type heapMemory struct{ buf []byte }

func (m *heapMemory) Reallocate(size uint64) []byte {
	if size > uint64(len(m.buf)) {
		m.buf = append(m.buf, make([]byte, size-uint64(len(m.buf)))...)
	}
	return m.buf[:size]
}

func (m *heapMemory) Free() { m.buf = nil }
