//go:build !linux

package sandbox

import "github.com/tetratelabs/wazero/experimental"

// newRunMemory returns no allocator, wazero's default, off Linux (see
// alloc_linux.go).
func newRunMemory() (experimental.MemoryAllocator, func()) {
	return nil, func() {}
}

// newMemoryImage returns wasm unchanged and wazero's default allocator off
// Linux (see image_linux.go).
func newMemoryImage(wasm []byte) ([]byte, func() (experimental.MemoryAllocator, func()), func()) {
	return wasm, newRunMemory, func() {}
}
