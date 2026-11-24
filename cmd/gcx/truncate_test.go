package main

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 3))
	assert.Equal(t, "ab", truncate("abc", 2))
	assert.Empty(t, truncate("abc", 0))

	// "é" is two bytes; a byte cap that lands inside it must drop it whole.
	got := truncate("aéb", 2)
	assert.Equal(t, "a", got)
	assert.True(t, utf8.ValidString(got))

	got = truncate("éé", 3)
	assert.Equal(t, "é", got)
	assert.True(t, utf8.ValidString(got))
}
