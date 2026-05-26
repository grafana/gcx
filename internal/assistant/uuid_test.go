package assistant //nolint:testpackage // testing unexported newUUID function

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewUUID_Format(t *testing.T) {
	id := newUUID()
	assert.Len(t, id, 36)
	// Check dashes at correct positions.
	assert.Equal(t, byte('-'), id[8])
	assert.Equal(t, byte('-'), id[13])
	assert.Equal(t, byte('-'), id[18])
	assert.Equal(t, byte('-'), id[23])
	// Version nibble (position 14) should be '4'.
	assert.Equal(t, byte('4'), id[14])
}

func TestNewUUID_Unique(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for range 100 {
		id := newUUID()
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate UUID generated: %s", id)
		}
		seen[id] = struct{}{}
	}
}
