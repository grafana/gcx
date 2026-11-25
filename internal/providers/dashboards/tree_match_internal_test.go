package dashboards

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlobMatcher(t *testing.T) {
	tests := []struct {
		pattern    string
		ignoreCase bool
		title      string
		want       bool
	}{
		{pattern: "*", title: "anything", want: true},
		{pattern: "Ceph*", title: "Ceph - Cluster", want: true},
		{pattern: "Ceph*", title: "My Ceph", want: false},
		{pattern: "*Overview", title: "Static Pages / Overview", want: true},
		{pattern: "Usage ?", title: "Usage 1", want: true},
		{pattern: "Usage ?", title: "Usage 10", want: false},
		{pattern: "[CO]*", title: "OSD Overview", want: true},
		{pattern: "[!CO]*", title: "OSD Overview", want: false},
		{pattern: "[^CO]*", title: "Host Details", want: true},
		{pattern: "[]]", title: "]", want: true},
		{pattern: "a|b", title: "b", want: true},
		{pattern: "a|b", title: "ab", want: false},
		{pattern: "*(RBD)", title: "Ceph CSI (RBD)", want: true},
		{pattern: `\*`, title: "*", want: true},
		{pattern: `\*`, title: "x", want: false},
		{pattern: "ceph*", title: "Ceph", want: false},
		{pattern: "ceph*", ignoreCase: true, title: "Ceph", want: true},
		{pattern: "Stratum 1 · *", title: "Stratum 1 · timepi", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.pattern+" "+tc.title, func(t *testing.T) {
			m, err := newGlobMatcher(tc.pattern, tc.ignoreCase)
			require.NoError(t, err)
			assert.Equal(t, tc.want, m.matches(tc.title))
		})
	}
}

func TestGlobMatcherEmptyAndInvalid(t *testing.T) {
	var m *globMatcher
	assert.False(t, m.matches("x"), "a nil matcher matches nothing")

	empty, err := newGlobMatcher("", false)
	require.NoError(t, err)
	assert.True(t, empty.matches(""))
	assert.False(t, empty.matches("x"))

	_, err = newGlobMatcher("[abc", false)
	require.ErrorContains(t, err, "unterminated character class")
}

func TestNaturalCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"usage 2", "usage 10", -1},
		{"usage 10", "usage 2", 1},
		{"usage 02", "usage 2", 0},
		{"v1.9", "v1.10", -1},
		{"abc", "abd", -1},
		{"abc", "abc", 0},
		{"ab", "abc", -1},
		{"10", "9", 1},
	}
	for _, tc := range tests {
		t.Run(tc.a+" vs "+tc.b, func(t *testing.T) {
			assert.Equal(t, tc.want, naturalCompare(tc.a, tc.b))
		})
	}
}
