package irm

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		maxLen int
		want   string
	}{
		{name: "short ASCII is unchanged", in: "disk full", maxLen: 10, want: "disk full"},
		{name: "exactly maxLen is unchanged", in: "0123456789", maxLen: 10, want: "0123456789"},
		{name: "long ASCII is cut", in: "0123456789abc", maxLen: 10, want: "0123456..."},
		{name: "multi-byte text within maxLen is unchanged", in: "сбой диска", maxLen: 10, want: "сбой диска"},
		{name: "multi-byte text is cut on a rune boundary", in: "база данных недоступна", maxLen: 10, want: "база да..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.in, tt.maxLen)
			assert.Equal(t, tt.want, got)
			assert.True(t, utf8.ValidString(got), "result must be valid UTF-8")
		})
	}
}
