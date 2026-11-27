package logs

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{name: "short is unchanged", in: "level=info", limit: 10, want: "level=info"},
		{name: "exactly limit is unchanged", in: "0123456789", limit: 10, want: "0123456789"},
		{name: "long ASCII is cut", in: "0123456789abc", limit: 10, want: "0123456..."},
		{name: "multi-byte within limit is unchanged", in: "сбой диска", limit: 10, want: "сбой диска"},
		{name: "multi-byte is cut on a rune boundary", in: "база данных недоступна", limit: 10, want: "база да..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.in, tt.limit)
			assert.Equal(t, tt.want, got)
			assert.True(t, utf8.ValidString(got), "result must be valid UTF-8")
		})
	}
}
