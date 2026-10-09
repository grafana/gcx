//go:build !linux

package sandbox

import "github.com/tetratelabs/wazero/experimental"

// newRunMemory returns no allocator, wazero's default, off Linux (see
// alloc_linux.go).
func newRunMemory() (experimental.MemoryAllocator, func()) {
	return nil, func() {}
}
