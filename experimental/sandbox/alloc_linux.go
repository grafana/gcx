//go:build linux

package sandbox

import (
	"math"
	"sync"
	"syscall"

	"github.com/tetratelabs/wazero/experimental"
)

// newRunMemory returns the allocator for one Run's instance and a function
// that frees everything it allocated.
//
// It backs linear memory with its own mapping, reserved at the memory limit
// up front and touched only as the guest grows into it. wazero's default
// grows a []byte by copying and leaves the old buffers to the Go GC, so
// concurrent runs peak at about 2.5x the memory and a process holds its
// high-water mark long after the runs end. A mapping never copies, and its
// pages go back to the OS as soon as it is freed.
//
// Run calls free once the guest has stopped, rather than relying on wazero:
// wazero skips releasing an instance's memory when the guest exits or traps
// just after its context is cancelled, which with a mapping would leak it
// for the life of the process. Freeing twice is harmless.
func newRunMemory() (experimental.MemoryAllocator, func()) {
	var mu sync.Mutex
	var mems []experimental.LinearMemory
	alloc := func(capacity, maxBytes uint64) experimental.LinearMemory {
		mem := allocateMapped(capacity, maxBytes)
		mu.Lock()
		mems = append(mems, mem)
		mu.Unlock()
		return mem
	}
	free := func() {
		mu.Lock()
		defer mu.Unlock()
		for _, mem := range mems {
			mem.Free()
		}
	}
	return experimental.MemoryAllocatorFunc(alloc), free
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

// heapMemory grows like wazero's default linear memory. Growing can move it,
// which wazero allows for everything but shared memory (threads), which gcx
// doesn't use.
type heapMemory struct{ buf []byte }

func (m *heapMemory) Reallocate(size uint64) []byte {
	if size > uint64(len(m.buf)) {
		m.buf = append(m.buf, make([]byte, size-uint64(len(m.buf)))...)
	} else {
		// wazero never shrinks memory, but if it did, growing again must
		// expose zeroes rather than the old contents.
		clear(m.buf[size:])
		m.buf = m.buf[:size]
	}
	return m.buf
}

func (m *heapMemory) Free() { m.buf = nil }
