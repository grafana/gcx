package graph

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestTruncateLabel(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "short", in: "up", want: "up"},
		{name: "exactly 30 ASCII", in: strings.Repeat("a", 30), want: strings.Repeat("a", 30)},
		{name: "long ASCII", in: strings.Repeat("a", 31), want: strings.Repeat("a", 27) + "..."},
		{name: "30 multi-byte runes", in: strings.Repeat("я", 30), want: strings.Repeat("я", 30)},
		{name: "long multi-byte", in: strings.Repeat("я", 31), want: strings.Repeat("я", 27) + "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateLabel(tt.in)
			assert.Equal(t, tt.want, got)
			assert.True(t, utf8.ValidString(got))
		})
	}
}

func TestLegends_LongMultiByteNames(t *testing.T) {
	series := []Series{{Name: strings.Repeat("база", 10), Points: []Point{{Value: 1}}}}

	for name, got := range map[string]string{
		"line": renderLegend(series),
		"bar":  renderBarLegend(series),
	} {
		t.Run(name, func(t *testing.T) {
			assert.True(t, utf8.ValidString(got))
			assert.Contains(t, got, strings.Repeat("база", 6)+"баз...")
		})
	}
}
