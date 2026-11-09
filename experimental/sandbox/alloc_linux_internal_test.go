//go:build linux

package sandbox

import (
	"bytes"
	"testing"
)

func TestAllocateMapped(t *testing.T) {
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
			mem := allocateMapped(64<<10, tc.maxBytes)
			if _, ok := mem.(*mappedMemory); ok != tc.mapped {
				t.Fatalf("got %T, want mapped %v", mem, tc.mapped)
			}
			buf := mem.Reallocate(64 << 10)
			if len(buf) != 64<<10 {
				t.Fatalf("len %d, want %d", len(buf), 64<<10)
			}
			buf[len(buf)-1] = 1
			// Growing keeps the contents.
			grown := mem.Reallocate(128 << 10)
			if len(grown) != 128<<10 || grown[64<<10-1] != 1 || !bytes.Equal(grown[64<<10:], make([]byte, 64<<10)) {
				t.Fatal("growing lost or dirtied the contents")
			}
			if tc.mapped && mem.Reallocate(tc.maxBytes+1) != nil {
				t.Fatal("grew past the reservation")
			}
			mem.Free()
		})
	}
}
