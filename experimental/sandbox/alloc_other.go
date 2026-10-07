//go:build !linux

package sandbox

import "github.com/tetratelabs/wazero/experimental"

// memoryAllocator returns nil, wazero's default, off Linux (see alloc_linux.go).
func memoryAllocator() experimental.MemoryAllocator { return nil }
